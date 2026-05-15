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

func TestCloneForAttemptNoGetBodyBuffersAndReplays(t *testing.T) {
	t.Parallel()

	const payload = "payload"

	request, err := stdhttp.NewRequestWithContext(
		context.Background(),
		stdhttp.MethodPost,
		"http://example.com",
		strings.NewReader(payload),
	)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	request.GetBody = nil
	request.ContentLength = 0

	firstClone, err := cloneForAttempt(context.Background(), request, defaultMaxReplayBodyBytes)
	if err != nil {
		t.Fatalf("first cloneForAttempt returned error: %v", err)
	}
	secondClone, err := cloneForAttempt(context.Background(), request, defaultMaxReplayBodyBytes)
	if err != nil {
		t.Fatalf("second cloneForAttempt returned error: %v", err)
	}
	defer firstClone.Body.Close()
	defer secondClone.Body.Close()

	firstBody, err := io.ReadAll(firstClone.Body)
	if err != nil {
		t.Fatalf("failed to read first clone body: %v", err)
	}
	secondBody, err := io.ReadAll(secondClone.Body)
	if err != nil {
		t.Fatalf("failed to read second clone body: %v", err)
	}

	if string(firstBody) != payload {
		t.Fatalf("unexpected first body: %q", string(firstBody))
	}
	if string(secondBody) != payload {
		t.Fatalf("unexpected second body: %q", string(secondBody))
	}
	if request.GetBody == nil {
		t.Fatal("expected original request GetBody to be set")
	}
	if firstClone.GetBody == nil {
		t.Fatal("expected first clone GetBody to be set")
	}
	if secondClone.GetBody == nil {
		t.Fatal("expected second clone GetBody to be set")
	}
	if request.Body == nil {
		t.Fatal("expected original request Body to be restored")
	}
	if request.ContentLength != int64(len(payload)) {
		t.Fatalf("unexpected content length: got %d, want %d", request.ContentLength, len(payload))
	}
}

func TestMaterializeBodyContentLengthMismatch(t *testing.T) {
	t.Parallel()

	const payload = "payload"

	request, err := stdhttp.NewRequestWithContext(
		context.Background(),
		stdhttp.MethodPost,
		"http://example.com",
		strings.NewReader(payload),
	)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	request.GetBody = nil
	request.ContentLength = 999

	cloned, err := cloneForAttempt(context.Background(), request, defaultMaxReplayBodyBytes)
	if err != nil {
		t.Fatalf("cloneForAttempt returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = cloned.Body.Close()
	})

	if request.ContentLength != int64(len(payload)) {
		t.Fatalf("unexpected original content length: got %d, want %d", request.ContentLength, len(payload))
	}
	if cloned.ContentLength != int64(len(payload)) {
		t.Fatalf("unexpected cloned content length: got %d, want %d", cloned.ContentLength, len(payload))
	}
}

func TestCloneForAttemptNoBodyNoMutation(t *testing.T) {
	t.Parallel()

	request, err := stdhttp.NewRequestWithContext(context.Background(), stdhttp.MethodGet, "http://example.com", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	cloned, cloneErr := cloneForAttempt(context.Background(), request, defaultMaxReplayBodyBytes)
	if cloneErr != nil {
		t.Fatalf("cloneForAttempt returned error: %v", cloneErr)
	}
	if cloned == request {
		t.Fatal("expected cloned request instance")
	}
	if request.Body != nil {
		t.Fatalf("expected original body to stay nil, got %T", request.Body)
	}
	if request.GetBody != nil {
		t.Fatal("expected original GetBody to stay nil")
	}
}

func TestRetryIfWithDefaultRetryPolicyPost503NoGetBodyRetries(t *testing.T) {
	t.Parallel()

	const payload = "payload"

	var attempts atomic.Int32
	seenBodies := make(chan string, 2)
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read request body: %v", err)
		}
		_ = r.Body.Close()
		seenBodies <- string(body)

		if attempts.Add(1) == 1 {
			w.WriteHeader(stdhttp.StatusServiceUnavailable)
			_, _ = w.Write([]byte("retry"))
			return
		}

		w.WriteHeader(stdhttp.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(server.Close)

	request, err := stdhttp.NewRequestWithContext(
		context.Background(),
		stdhttp.MethodPost,
		server.URL,
		strings.NewReader(payload),
	)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	request.GetBody = nil

	executor := routery.Apply(
		NewExecutor(server.Client()),
		routery.RetryIf[*stdhttp.Request, *stdhttp.Response](2, 0, DefaultRetryPolicy),
	)

	response, executeErr := executor.Execute(context.Background(), request)
	if executeErr != nil {
		t.Fatalf("execute returned unexpected error: %v", executeErr)
	}
	t.Cleanup(func() {
		_ = response.Body.Close()
	})

	if attempts.Load() != 2 {
		t.Fatalf("unexpected attempt count: got %d, want 2", attempts.Load())
	}
	for attempt := 1; attempt <= 2; attempt++ {
		got := <-seenBodies
		if got != payload {
			t.Fatalf("unexpected body at attempt %d: %q", attempt, got)
		}
	}
}

func TestNewExecutorMaxReplayBodyBytesExceeded(t *testing.T) {
	t.Parallel()

	const (
		maxReplayBytes = 8
		bodyBytes      = 64
	)

	var attempts atomic.Int32
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		attempts.Add(1)
		w.WriteHeader(stdhttp.StatusOK)
	}))
	t.Cleanup(server.Close)

	request, err := stdhttp.NewRequestWithContext(
		context.Background(),
		stdhttp.MethodPost,
		server.URL,
		strings.NewReader(strings.Repeat("x", bodyBytes)),
	)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	request.GetBody = nil

	_, executeErr := NewExecutor(server.Client(), WithMaxReplayBodyBytes(maxReplayBytes)).Execute(
		context.Background(),
		request,
	)
	if !errors.Is(executeErr, ErrReplayBodyTooLarge) {
		t.Fatalf("expected ErrReplayBodyTooLarge, got %v", executeErr)
	}
	if attempts.Load() != 0 {
		t.Fatalf("unexpected server attempts: got %d, want 0", attempts.Load())
	}
}

func TestNewExecutorMaxReplayBodyBytesExceededIsSticky(t *testing.T) {
	t.Parallel()

	const (
		maxReplayBytes = 8
		bodyBytes      = 64
	)

	var attempts atomic.Int32
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		attempts.Add(1)
		w.WriteHeader(stdhttp.StatusOK)
	}))
	t.Cleanup(server.Close)

	request, err := stdhttp.NewRequestWithContext(
		context.Background(),
		stdhttp.MethodPost,
		server.URL,
		strings.NewReader(strings.Repeat("x", bodyBytes)),
	)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	request.GetBody = nil

	executor := NewExecutor(server.Client(), WithMaxReplayBodyBytes(maxReplayBytes))
	for attempt := 1; attempt <= 2; attempt++ {
		_, executeErr := executor.Execute(context.Background(), request)
		if !errors.Is(executeErr, ErrReplayBodyTooLarge) {
			t.Fatalf("attempt %d: expected ErrReplayBodyTooLarge, got %v", attempt, executeErr)
		}
	}
	if attempts.Load() != 0 {
		t.Fatalf("unexpected server attempts: got %d, want 0", attempts.Load())
	}
}

func TestReadAllLimitedWrapsUnlimitedReadError(t *testing.T) {
	t.Parallel()

	readErr := errors.New("read failed")
	_, err := readAllLimited(failingReader{err: readErr}, 0)
	if !errors.Is(err, readErr) {
		t.Fatalf("expected wrapped read error, got %v", err)
	}
	if !strings.Contains(err.Error(), "read body") {
		t.Fatalf("expected read body context, got %v", err)
	}
}

func TestNewExecutorMaxReplayBodyBytesUnlimited(t *testing.T) {
	t.Parallel()

	const largePayloadSize = 1 << 20

	payload := strings.Repeat("x", largePayloadSize)
	var attempts atomic.Int32
	seenSizes := make(chan int, 2)
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read request body: %v", err)
		}
		_ = r.Body.Close()
		seenSizes <- len(body)

		if attempts.Add(1) == 1 {
			w.WriteHeader(stdhttp.StatusServiceUnavailable)
			_, _ = w.Write([]byte("retry"))
			return
		}

		w.WriteHeader(stdhttp.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(server.Close)

	request, err := stdhttp.NewRequestWithContext(
		context.Background(),
		stdhttp.MethodPost,
		server.URL,
		strings.NewReader(payload),
	)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	request.GetBody = nil

	executor := routery.Apply(
		NewExecutor(server.Client(), WithMaxReplayBodyBytes(0)),
		routery.RetryIf[*stdhttp.Request, *stdhttp.Response](2, 0, DefaultRetryPolicy),
	)

	response, executeErr := executor.Execute(context.Background(), request)
	if executeErr != nil {
		t.Fatalf("execute returned unexpected error: %v", executeErr)
	}
	t.Cleanup(func() {
		_ = response.Body.Close()
	})

	if attempts.Load() != 2 {
		t.Fatalf("unexpected attempt count: got %d, want 2", attempts.Load())
	}
	for attempt := 1; attempt <= 2; attempt++ {
		got := <-seenSizes
		if got != largePayloadSize {
			t.Fatalf("unexpected body size at attempt %d: got %d, want %d", attempt, got, largePayloadSize)
		}
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

type failingReader struct {
	err error
}

func (reader failingReader) Read([]byte) (int, error) {
	return 0, reader.err
}
