package routeryhttp

import (
	"context"
	"errors"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/skosovsky/routery"
)

func TestNewExecutorReturnsConfigErrorForNilClient(t *testing.T) {
	t.Parallel()

	executor := NewExecutor(nil)
	_, err := executor.Execute(context.Background(), httptest.NewRequest(stdhttp.MethodGet, "/", nil))
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestNewExecutorReturnsConfigErrorForNilRequest(t *testing.T) {
	t.Parallel()

	executor := NewExecutor(stdhttp.DefaultClient)
	_, err := executor.Execute(context.Background(), nil)
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestNewExecutorReturnsResponseForSuccessStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(server.Close)

	client := server.Client()
	request, err := stdhttp.NewRequestWithContext(context.Background(), stdhttp.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	response, executeErr := NewExecutor(client).Execute(context.Background(), request)
	if executeErr != nil {
		t.Fatalf("execute returned unexpected error: %v", executeErr)
	}
	t.Cleanup(func() {
		_ = response.Body.Close()
	})
	if response.StatusCode != stdhttp.StatusOK {
		t.Fatalf("unexpected status code: got %d, want %d", response.StatusCode, stdhttp.StatusOK)
	}
}

func TestNewExecutorWrapsNon2xxAsStatusError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusServiceUnavailable)
		_, _ = w.Write([]byte("temporary failure"))
	}))
	t.Cleanup(server.Close)

	client := server.Client()
	request, err := stdhttp.NewRequestWithContext(context.Background(), stdhttp.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	response, executeErr := NewExecutor(client).Execute(context.Background(), request)
	if executeErr == nil {
		t.Fatal("expected status error")
	}
	t.Cleanup(func() {
		_ = response.Body.Close()
	})
	if response == nil {
		t.Fatal("expected non-nil response")
	}

	var statusErr *StatusError
	if !errors.As(executeErr, &statusErr) {
		t.Fatalf("expected StatusError, got %T", executeErr)
	}
	if statusErr.Code != stdhttp.StatusServiceUnavailable {
		t.Fatalf("unexpected status code: got %d, want %d", statusErr.Code, stdhttp.StatusServiceUnavailable)
	}
}

func TestNewExecutorRespectsRequestContextTimeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
			t.Fatal("request was not cancelled in time")
		}
		w.WriteHeader(stdhttp.StatusGatewayTimeout)
	}))
	t.Cleanup(server.Close)

	client := server.Client()
	request, err := stdhttp.NewRequestWithContext(context.Background(), stdhttp.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	executor := routery.Apply(
		NewExecutor(client),
		routery.Timeout[*stdhttp.Request, *stdhttp.Response](16*time.Millisecond),
	)

	_, executeErr := executor.Execute(context.Background(), request)
	if !errors.Is(executeErr, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", executeErr)
	}
}

func TestDefaultRetryPolicyRetryableStatusClosesBody(t *testing.T) {
	t.Parallel()

	closeCounter := &trackingReadCloser{}
	req := httptest.NewRequest(stdhttp.MethodGet, "/", nil)
	statusErr := &StatusError{
		Request: req,
		Response: &stdhttp.Response{
			StatusCode: stdhttp.StatusServiceUnavailable,
			Body:       closeCounter,
		},
		Code: stdhttp.StatusServiceUnavailable,
	}

	retry := DefaultRetryPolicy(context.Background(), req, statusErr)
	if !retry {
		t.Fatal("expected retry for retryable status")
	}
	if closeCounter.closes.Load() != 1 {
		t.Fatalf("expected body to be closed once, got %d", closeCounter.closes.Load())
	}
}

func TestDefaultRetryPolicyNonRetryableStatusDoesNotCloseBody(t *testing.T) {
	t.Parallel()

	closeCounter := &trackingReadCloser{}
	req := httptest.NewRequest(stdhttp.MethodGet, "/", nil)
	statusErr := &StatusError{
		Request: req,
		Response: &stdhttp.Response{
			StatusCode: stdhttp.StatusBadRequest,
			Body:       closeCounter,
		},
		Code: stdhttp.StatusBadRequest,
	}

	retry := DefaultRetryPolicy(context.Background(), req, statusErr)
	if retry {
		t.Fatal("expected no retry for non-retryable status")
	}
	if closeCounter.closes.Load() != 0 {
		t.Fatalf("expected body not to be closed, got %d", closeCounter.closes.Load())
	}
}

func TestDefaultRetryPolicyRequiresReplayableBody(t *testing.T) {
	t.Parallel()

	req := &stdhttp.Request{
		Method: stdhttp.MethodGet,
		Body:   io.NopCloser(strings.NewReader("payload")),
	}
	statusErr := &StatusError{
		Request: req,
		Response: &stdhttp.Response{
			StatusCode: stdhttp.StatusServiceUnavailable,
			Body:       io.NopCloser(strings.NewReader("response")),
		},
		Code: stdhttp.StatusServiceUnavailable,
	}

	retry := DefaultRetryPolicy(context.Background(), req, statusErr)
	if retry {
		t.Fatal("expected no retry for non-replayable body")
	}
}

func TestDefaultRetryPolicyTransportRules(t *testing.T) {
	t.Parallel()

	methods := []struct {
		name      string
		method    string
		wantRetry bool
	}{
		{name: "idempotent", method: stdhttp.MethodGet, wantRetry: true},
		{name: "non-idempotent", method: stdhttp.MethodPost, wantRetry: false},
	}

	for _, tc := range methods {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequest(tc.method, "/", nil)
			got := DefaultRetryPolicy(context.Background(), request, io.ErrUnexpectedEOF)
			if got != tc.wantRetry {
				t.Fatalf("unexpected retry decision: got %v, want %v", got, tc.wantRetry)
			}
		})
	}
}

func TestRetryIfWithDefaultRetryPolicyKeepsFinalBodyOpen(t *testing.T) {
	t.Parallel()

	callCounter := atomic.Int32{}
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		callCounter.Add(1)
		w.WriteHeader(stdhttp.StatusServiceUnavailable)
		_, _ = w.Write([]byte("final-response-body"))
	}))
	t.Cleanup(server.Close)

	client := server.Client()
	request, err := stdhttp.NewRequestWithContext(context.Background(), stdhttp.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	executor := routery.Apply(
		NewExecutor(client),
		routery.RetryIf[*stdhttp.Request, *stdhttp.Response](2, 0, DefaultRetryPolicy),
	)

	response, executeErr := executor.Execute(context.Background(), request)
	if executeErr == nil {
		t.Fatal("expected status error")
	}
	if response == nil {
		t.Fatal("expected final response")
	}

	var statusErr *StatusError
	if !errors.As(executeErr, &statusErr) {
		t.Fatalf("expected StatusError, got %T", executeErr)
	}

	bodyBytes, readErr := io.ReadAll(response.Body)
	if readErr != nil {
		t.Fatalf("expected readable final body, got %v", readErr)
	}
	if string(bodyBytes) != "final-response-body" {
		t.Fatalf("unexpected final body: %q", string(bodyBytes))
	}
	if callCounter.Load() != 2 {
		t.Fatalf("unexpected call count: got %d, want 2", callCounter.Load())
	}
	_ = response.Body.Close()
}

func TestRetryIfClosesIntermediateStatusBodies(t *testing.T) {
	t.Parallel()

	closeCounter := &trackingReadCloser{}
	attempts := 0
	request := httptest.NewRequest(stdhttp.MethodGet, "/", nil)

	base := routery.ExecutorFunc[*stdhttp.Request, *stdhttp.Response](
		func(context.Context, *stdhttp.Request) (*stdhttp.Response, error) {
			attempts++
			if attempts == 1 {
				response := &stdhttp.Response{
					StatusCode: stdhttp.StatusServiceUnavailable,
					Body:       closeCounter,
				}
				return response, &StatusError{
					Request:  request,
					Response: response,
					Code:     stdhttp.StatusServiceUnavailable,
				}
			}

			return &stdhttp.Response{
				StatusCode: stdhttp.StatusOK,
				Body:       io.NopCloser(strings.NewReader("ok")),
			}, nil
		},
	)

	executor := routery.Apply(
		base,
		routery.RetryIf[*stdhttp.Request, *stdhttp.Response](2, 0, DefaultRetryPolicy),
	)

	response, err := executor.Execute(context.Background(), request)
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if response.StatusCode != stdhttp.StatusOK {
		t.Fatalf("unexpected status code: got %d, want %d", response.StatusCode, stdhttp.StatusOK)
	}
	_ = response.Body.Close()

	if closeCounter.closes.Load() != 1 {
		t.Fatalf("expected intermediate body close, got %d", closeCounter.closes.Load())
	}
}

func TestIsRetryableStatus(t *testing.T) {
	t.Parallel()

	if !IsRetryableStatus(stdhttp.StatusTooManyRequests) {
		t.Fatal("expected 429 to be retryable")
	}
	if IsRetryableStatus(stdhttp.StatusBadRequest) {
		t.Fatal("expected 400 to be non-retryable")
	}
}

type trackingReadCloser struct {
	closes atomic.Int32
}

func (body *trackingReadCloser) Read([]byte) (int, error) {
	return 0, io.EOF
}

func (body *trackingReadCloser) Close() error {
	body.closes.Add(1)
	return nil
}
