package routerys3

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/skosovsky/routery"
)

type fakePut struct {
	calls atomic.Int32
	out   *s3.PutObjectOutput
	err   error
}

type contextBody struct {
	ctx    context.Context
	reader *strings.Reader
	closes atomic.Int32
}

func (body *contextBody) Read(data []byte) (int, error) {
	if err := body.ctx.Err(); err != nil {
		return 0, err
	}
	return body.reader.Read(data)
}

func (body *contextBody) Close() error {
	body.closes.Add(1)
	return nil
}

type ownedGet struct {
	body *contextBody
}

func (api *ownedGet) GetObject(ctx context.Context, _ *s3.GetObjectInput,
	_ ...func(*s3.Options),
) (*s3.GetObjectOutput, error) {
	api.body.ctx = ctx
	return &s3.GetObjectOutput{Body: api.body}, nil
}

func TestGetWinnerOwnsStreamUntilClose(t *testing.T) {
	t.Parallel()
	// Arrange.
	body := &contextBody{reader: strings.NewReader("complete object")}
	api := &ownedGet{body: body}
	handler := routery.FirstCompleted(NewGetObjectRouteHandler(api))
	// Act.
	result, err := routery.InvokeRouteHandler(context.Background(), &s3.GetObjectInput{}, handler)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = result.Lifetime.Close() })
	data, readErr := io.ReadAll(result.Payload.Body)
	// Assert.
	if readErr != nil || string(data) != "complete object" || result.Lifetime == nil {
		t.Fatalf("stream data=%q error=%v lifetime=%v", data, readErr, result.Lifetime)
	}
	if body.ctx.Err() != nil || body.closes.Load() != 0 {
		t.Fatal("winner closed before caller finished reading")
	}
	if err := result.Payload.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if err := result.Lifetime.Close(); err != nil {
		t.Fatal(err)
	}
	if body.closes.Load() != 1 || !errors.Is(body.ctx.Err(), context.Canceled) {
		t.Fatalf("closes=%d context=%v", body.closes.Load(), body.ctx.Err())
	}
}

func (f *fakePut) PutObject(
	ctx context.Context,
	params *s3.PutObjectInput,
	optFns ...func(*s3.Options),
) (*s3.PutObjectOutput, error) {
	f.calls.Add(1)
	_ = ctx
	_ = params
	_ = optFns
	return f.out, f.err
}

func TestNewPutObjectRouteHandlerNil(t *testing.T) {
	t.Parallel()
	ex := NewPutObjectRouteHandler(nil)
	_, err := routery.InvokeRouteHandler(context.Background(), &s3.PutObjectInput{}, ex)
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("got %v", err)
	}
}

func TestNewPutObjectRouteHandlerOK(t *testing.T) {
	t.Parallel()
	fp := &fakePut{out: &s3.PutObjectOutput{}}
	ex := NewPutObjectRouteHandler(fp)
	b, k := "b", "k"
	outcome, err := routery.InvokeRouteHandler(context.Background(), &s3.PutObjectInput{Bucket: &b, Key: &k}, ex)
	if err != nil || !outcome.HasPayload || outcome.Payload != fp.out {
		t.Fatalf("out=%v err=%v", outcome.Payload, err)
	}
	if fp.calls.Load() != 1 {
		t.Fatalf("calls=%d", fp.calls.Load())
	}
}

type fakeGet struct {
	calls int
	out   *s3.GetObjectOutput
	err   error
}

func (f *fakeGet) GetObject(
	ctx context.Context,
	params *s3.GetObjectInput,
	optFns ...func(*s3.Options),
) (*s3.GetObjectOutput, error) {
	f.calls++
	_ = ctx
	_ = params
	_ = optFns
	return f.out, f.err
}

func TestNewGetObjectRouteHandlerNil(t *testing.T) {
	t.Parallel()
	ex := NewGetObjectRouteHandler(nil)
	_, err := routery.InvokeRouteHandler(context.Background(), &s3.GetObjectInput{}, ex)
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("got %v", err)
	}
}

func TestNewGetObjectRouteHandlerOK(t *testing.T) {
	t.Parallel()
	fg := &fakeGet{out: &s3.GetObjectOutput{}}
	ex := NewGetObjectRouteHandler(fg)
	b, k := "b", "k"
	outcome, err := routery.InvokeRouteHandler(context.Background(), &s3.GetObjectInput{Bucket: &b, Key: &k}, ex)
	if err != nil || !outcome.HasPayload || outcome.Payload != fg.out {
		t.Fatalf("out=%v err=%v", outcome.Payload, err)
	}
}

func TestPutRouteHandlerConcurrent(t *testing.T) {
	t.Parallel()
	fp := &fakePut{out: &s3.PutObjectOutput{}}
	ex := NewPutObjectRouteHandler(fp)
	const workers = 128
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			_, e := routery.InvokeRouteHandler(context.Background(), &s3.PutObjectInput{}, ex)
			if e != nil {
				t.Error(e)
			}
		})
	}
	wg.Wait()
	if got := int(fp.calls.Load()); got != workers {
		t.Fatalf("want %d calls, got %d", workers, got)
	}
}
