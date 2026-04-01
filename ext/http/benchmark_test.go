package routeryhttp

import (
	"context"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/skosovsky/routery"
)

func BenchmarkNewExecutorSuccess(b *testing.B) {
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	request, err := stdhttp.NewRequestWithContext(context.Background(), stdhttp.MethodGet, server.URL, nil)
	if err != nil {
		b.Fatalf("failed to create request: %v", err)
	}

	executor := NewExecutor(server.Client())
	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		response, executeErr := executor.Execute(context.Background(), request)
		if executeErr != nil {
			b.Fatalf("unexpected execute error: %v", executeErr)
		}
		_ = response.Body.Close()
	}
}

func BenchmarkDefaultRetryPolicyStatus503(b *testing.B) {
	request := httptest.NewRequest(stdhttp.MethodGet, "http://example.com", nil)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		err := &StatusError{
			Request: request,
			Response: &stdhttp.Response{
				StatusCode: stdhttp.StatusServiceUnavailable,
				Status:     "503 Service Unavailable",
				Body:       io.NopCloser(strings.NewReader("retry")),
			},
			Code: stdhttp.StatusServiceUnavailable,
		}
		_ = DefaultRetryPolicy(context.Background(), request, err)
	}
}

func BenchmarkCloneForAttemptWithGetBody(b *testing.B) {
	request := httptest.NewRequest(stdhttp.MethodPost, "http://example.com", strings.NewReader("payload"))
	request.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("payload")), nil
	}

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		cloned, err := cloneForAttempt(context.Background(), request)
		if err != nil {
			b.Fatalf("unexpected clone error: %v", err)
		}
		_ = cloned.Body.Close()
	}
}

func BenchmarkRetryIfHTTP503Then200(b *testing.B) {
	client := &stdhttp.Client{Transport: &alternatingRoundTripper{}}
	request, err := stdhttp.NewRequestWithContext(
		context.Background(),
		stdhttp.MethodGet,
		"http://example.com",
		nil,
	)
	if err != nil {
		b.Fatalf("failed to create request: %v", err)
	}
	executor := routery.Apply(
		NewExecutor(client),
		routery.RetryIf[*stdhttp.Request, *stdhttp.Response](2, 0, DefaultRetryPolicy),
	)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		response, executeErr := executor.Execute(context.Background(), request)
		if executeErr != nil {
			b.Fatalf("unexpected execute error: %v", executeErr)
		}
		_ = response.Body.Close()
	}
}

type alternatingRoundTripper struct {
	calls atomic.Uint64
}

func (transport *alternatingRoundTripper) RoundTrip(*stdhttp.Request) (*stdhttp.Response, error) {
	call := transport.calls.Add(1)
	if call%2 == 1 {
		return &stdhttp.Response{
			StatusCode: stdhttp.StatusServiceUnavailable,
			Status:     "503 Service Unavailable",
			Body:       io.NopCloser(strings.NewReader("retry")),
		}, nil
	}

	return &stdhttp.Response{
		StatusCode: stdhttp.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(strings.NewReader("ok")),
	}, nil
}
