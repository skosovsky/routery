package routerys3

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// BodyFactory creates an independent full upload reader for each call, including fan-out.
// It must support concurrent calls. Any non-nil body transfers ownership, even on error.
type BodyFactory func(context.Context) (io.ReadCloser, error)

// PutRequest borrows immutable non-body input fields and owns each factory body.
// A non-nil Input.Body is invalid. No factory means an empty upload.
type PutRequest struct {
	Input       s3.PutObjectInput
	BodyFactory BodyFactory
}

// PreparePutRequest consumes and closes input.Body and snapshots at most maxBytes+1.
// It does not authorize replay of the remote effect. Other input fields stay borrowed.
// Blocked Read cannot be interrupted by this helper; the source owner must provide that.
func PreparePutRequest(ctx context.Context, input *s3.PutObjectInput, maxBytes int64) (PutRequest, error) {
	if input == nil {
		return PutRequest{}, configError("nil PutObject input")
	}
	if closer, ok := input.Body.(io.Closer); ok {
		return prepareAndClose(ctx, input, maxBytes, closer)
	}
	return preparePut(ctx, input, maxBytes)
}

func prepareAndClose(
	ctx context.Context,
	input *s3.PutObjectInput,
	maxBytes int64,
	closer io.Closer,
) (PutRequest, error) {
	request, err := preparePut(ctx, input, maxBytes)
	err = errors.Join(err, closer.Close())
	if err != nil {
		return PutRequest{}, err
	}
	return request, nil
}

func preparePut(ctx context.Context, input *s3.PutObjectInput, maxBytes int64) (PutRequest, error) {
	if maxBytes < 0 || maxBytes == math.MaxInt64 {
		return PutRequest{}, configError("invalid upload buffer limit")
	}
	if err := ctx.Err(); err != nil {
		return PutRequest{}, err
	}
	request := PutRequest{Input: *input, BodyFactory: nil}
	request.Input.Body = nil
	if input.Body == nil {
		return request, nil
	}
	data, err := io.ReadAll(io.LimitReader(&checkedReader{ctx: ctx, reader: input.Body}, maxBytes+1))
	if err != nil {
		return PutRequest{}, err
	}
	if int64(len(data)) > maxBytes {
		return PutRequest{}, configError("upload exceeds buffer limit")
	}
	if err := ctx.Err(); err != nil {
		return PutRequest{}, err
	}
	request.BodyFactory = func(ctx context.Context) (io.ReadCloser, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return &preparedBody{Reader: bytes.NewReader(data)}, nil
	}
	return request, nil
}

type checkedReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader *checkedReader) Read(data []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(data)
}

type preparedBody struct{ *bytes.Reader }

func (*preparedBody) Close() error { return nil }
