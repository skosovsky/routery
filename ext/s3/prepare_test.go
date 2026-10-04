package routerys3

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/skosovsky/routery"
)

type uploadFixture struct {
	mu      sync.Mutex
	uploads []string
	fail    error
}

func (api *uploadFixture) PutObject(
	_ context.Context,
	input *s3.PutObjectInput,
	_ ...func(*s3.Options),
) (*s3.PutObjectOutput, error) {
	data, err := io.ReadAll(input.Body)
	api.mu.Lock()
	defer api.mu.Unlock()
	api.uploads = append(api.uploads, string(data))
	return nil, errors.Join(err, api.fail)
}
func TestPreparedPutHasIndependentFullBodies(t *testing.T) {
	t.Parallel()
	// Arrange.
	request, err := PreparePutRequest(
		context.Background(),
		&s3.PutObjectInput{Body: strings.NewReader("full payload")},
		100,
	)
	if err != nil {
		t.Fatal(err)
	}
	api := &uploadFixture{fail: io.EOF}
	handler := NewPutObjectRouteHandler(api)
	// Act: consumed first attempt, then repeat; independent parallel calls share template.
	for range 2 {
		_, _ = routery.InvokeRouteHandler(context.Background(), request, handler)
	}
	var workers sync.WaitGroup
	for range 16 {
		workers.Go(func() { _, _ = routery.InvokeRouteHandler(context.Background(), request, handler) })
	}
	workers.Wait()
	// Assert.
	if len(api.uploads) != 18 {
		t.Fatal(api.uploads)
	}
	for _, data := range api.uploads {
		if data != "full payload" {
			t.Fatalf("body=%q", data)
		}
	}
}

type closingSource struct {
	io.Reader

	closes int
	err    error
}

func (source *closingSource) Close() error { source.closes++; return source.err }
func TestPutPreparationFailureOwnership(t *testing.T) {
	t.Parallel()
	for _, limit := range []int64{-1, 2, 100} {
		t.Run(map[int64]string{-1: "invalid-limit", 2: "overflow", 100: "close-error"}[limit], func(t *testing.T) {
			t.Parallel()
			// Arrange.
			source := &closingSource{Reader: strings.NewReader("payload"), err: io.ErrClosedPipe}
			// Act.
			_, err := PreparePutRequest(context.Background(), &s3.PutObjectInput{Body: source}, limit)
			// Assert.
			if err == nil || source.closes != 1 || !errors.Is(err, io.ErrClosedPipe) {
				t.Fatalf("error=%v closes=%d", err, source.closes)
			}
		})
	}
}
func TestPutRejectsUnpreparedAndClosesFactoryFailures(t *testing.T) {
	t.Parallel()
	// Arrange.
	api := &uploadFixture{}
	handler := NewPutObjectRouteHandler(api)
	source := &closingSource{Reader: strings.NewReader("payload"), err: io.ErrClosedPipe}
	// Act.
	_, invalid := routery.InvokeRouteHandler(
		context.Background(),
		PutRequest{Input: s3.PutObjectInput{Body: strings.NewReader("unsafe")}},
		handler,
	)
	_, failed := routery.InvokeRouteHandler(
		context.Background(),
		PutRequest{BodyFactory: func(context.Context) (io.ReadCloser, error) { return source, io.EOF }},
		handler,
	)
	// Assert.
	if !errors.Is(invalid, routery.ErrInvalidConfig) || !errors.Is(failed, io.EOF) ||
		!errors.Is(failed, io.ErrClosedPipe) ||
		source.closes != 1 ||
		len(api.uploads) != 0 {
		t.Fatalf("invalid=%v failed=%v closes=%d calls=%d", invalid, failed, source.closes, len(api.uploads))
	}
}

type partialDownload struct {
	body io.ReadCloser
	err  error
}

func (api partialDownload) GetObject(
	context.Context,
	*s3.GetObjectInput,
	...func(*s3.Options),
) (*s3.GetObjectOutput, error) {
	return &s3.GetObjectOutput{Body: api.body}, api.err
}
func TestPartialDownloadKeepsOwnershipOnError(t *testing.T) {
	t.Parallel()
	// Arrange.
	source := &closingSource{Reader: strings.NewReader("partial")}
	providerErr := io.EOF
	handler := NewGetObjectRouteHandler(partialDownload{body: source, err: providerErr})
	// Act.
	result, err := routery.InvokeRouteHandler(context.Background(), &s3.GetObjectInput{}, handler)
	// Assert.
	if !errors.Is(err, providerErr) || !result.HasPayload || result.Lifetime == nil || source.closes != 0 {
		t.Fatal(result, err, source.closes)
	}
	_ = result.Payload.Body.Close()
	_ = result.Lifetime.Close()
	if source.closes != 1 {
		t.Fatal(source.closes)
	}
}
