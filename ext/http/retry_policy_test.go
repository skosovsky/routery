package routeryhttp

import (
	"context"
	"errors"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/skosovsky/routery"
)

func TestStatusErrorError(t *testing.T) {
	t.Parallel()

	t.Run("nil receiver", func(t *testing.T) {
		t.Parallel()

		var statusErr *StatusError
		if got := statusErr.Error(); got != "routery/ext/http: status error" {
			t.Fatalf("unexpected error string: %q", got)
		}
	})

	t.Run("with response status", func(t *testing.T) {
		t.Parallel()

		statusErr := &StatusError{
			Code:     stdhttp.StatusServiceUnavailable,
			Response: &stdhttp.Response{Status: "503 Service Unavailable"},
		}
		if got := statusErr.Error(); got != "routery/ext/http: unexpected status 503 (503 Service Unavailable)" {
			t.Fatalf("unexpected error string: %q", got)
		}
	})

	t.Run("without response", func(t *testing.T) {
		t.Parallel()

		statusErr := &StatusError{Code: stdhttp.StatusBadGateway}
		if got := statusErr.Error(); got != "routery/ext/http: unexpected status 502" {
			t.Fatalf("unexpected error string: %q", got)
		}
	})
}

func TestDefaultRetryPolicyGuards(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
	}{
		{name: "nil", err: nil},
		{name: "context canceled", err: context.Canceled},
		{name: "context deadline exceeded", err: context.DeadlineExceeded},
		{name: "unknown", err: errors.New("unknown")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if DefaultRetryPolicy(context.Background(), nil, tc.err) {
				t.Fatalf("expected no retry for %s", tc.name)
			}
		})
	}
}

func TestDefaultRetryPolicyStatusMethodMatrix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		method     string
		statusCode int
		replayable bool
		wantRetry  bool
	}{
		{
			name:       "get 503 replayable",
			method:     stdhttp.MethodGet,
			statusCode: stdhttp.StatusServiceUnavailable,
			replayable: true,
			wantRetry:  true,
		},
		{
			name:       "post 503 replayable",
			method:     stdhttp.MethodPost,
			statusCode: stdhttp.StatusServiceUnavailable,
			replayable: true,
			wantRetry:  true,
		},
		{
			name:       "patch 503 replayable",
			method:     stdhttp.MethodPatch,
			statusCode: stdhttp.StatusServiceUnavailable,
			replayable: true,
			wantRetry:  true,
		},
		{
			name:       "post 502 replayable",
			method:     stdhttp.MethodPost,
			statusCode: stdhttp.StatusBadGateway,
			replayable: true,
			wantRetry:  false,
		},
		{
			name:       "delete 503 replayable",
			method:     stdhttp.MethodDelete,
			statusCode: stdhttp.StatusServiceUnavailable,
			replayable: true,
			wantRetry:  false,
		},
		{
			name:       "get 503 non replayable",
			method:     stdhttp.MethodGet,
			statusCode: stdhttp.StatusServiceUnavailable,
			replayable: false,
			wantRetry:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			request := replayableRequest(t, tc.method, tc.replayable)
			closeCounter := &trackingReadCloser{}
			statusErr := &StatusError{
				Request: request,
				Response: &stdhttp.Response{
					StatusCode: tc.statusCode,
					Body:       closeCounter,
				},
				Code: tc.statusCode,
			}

			gotRetry := DefaultRetryPolicy(context.Background(), request, statusErr)
			if gotRetry != tc.wantRetry {
				t.Fatalf("unexpected retry decision: got %v, want %v", gotRetry, tc.wantRetry)
			}

			wantCloses := int32(0)
			if tc.wantRetry {
				wantCloses = 1
			}
			if got := closeCounter.closes.Load(); got != wantCloses {
				t.Fatalf("unexpected close count: got %d, want %d", got, wantCloses)
			}
		})
	}
}

func TestDefaultRetryPolicyTransportMatrix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		method     string
		replayable bool
		innerErr   error
		wantRetry  bool
	}{
		{
			name:       "get replayable io eof",
			method:     stdhttp.MethodGet,
			replayable: true,
			innerErr:   io.EOF,
			wantRetry:  true,
		},
		{
			name:       "get replayable unexpected eof",
			method:     stdhttp.MethodGet,
			replayable: true,
			innerErr:   io.ErrUnexpectedEOF,
			wantRetry:  true,
		},
		{
			name:       "get replayable url wrapped eof",
			method:     stdhttp.MethodGet,
			replayable: true,
			innerErr: &url.Error{
				Op:  "Get",
				URL: "http://example.com",
				Err: io.EOF,
			},
			wantRetry: true,
		},
		{
			name:       "get replayable net error",
			method:     stdhttp.MethodGet,
			replayable: true,
			innerErr:   flakyNetError{},
			wantRetry:  true,
		},
		{
			name:       "post replayable io eof",
			method:     stdhttp.MethodPost,
			replayable: true,
			innerErr:   io.EOF,
			wantRetry:  false,
		},
		{
			name:       "get non replayable io eof",
			method:     stdhttp.MethodGet,
			replayable: false,
			innerErr:   io.EOF,
			wantRetry:  false,
		},
		{
			name:       "get replayable unknown",
			method:     stdhttp.MethodGet,
			replayable: true,
			innerErr:   errors.New("boom"),
			wantRetry:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			request := replayableRequest(t, tc.method, tc.replayable)

			gotRetry := DefaultRetryPolicy(context.Background(), request, tc.innerErr)
			if gotRetry != tc.wantRetry {
				t.Fatalf("unexpected retry decision: got %v, want %v", gotRetry, tc.wantRetry)
			}
		})
	}
}

func TestCloneForAttemptWithoutBody(t *testing.T) {
	t.Parallel()

	request := mustNewRequest(t, stdhttp.MethodGet, nil)
	cloned, err := cloneForAttempt(context.Background(), request, defaultMaxReplayBodyBytes)
	if err != nil {
		t.Fatalf("cloneForAttempt returned error: %v", err)
	}
	if cloned == request {
		t.Fatal("expected cloned request instance")
	}
	if cloned.Body != nil {
		t.Fatalf("expected nil body, got %T", cloned.Body)
	}
}

func TestCloneForAttemptNoBodySentinel(t *testing.T) {
	t.Parallel()

	request := mustNewRequest(t, stdhttp.MethodGet, nil)
	request.Body = stdhttp.NoBody

	cloned, err := cloneForAttempt(context.Background(), request, defaultMaxReplayBodyBytes)
	if err != nil {
		t.Fatalf("cloneForAttempt returned error: %v", err)
	}
	if cloned.Body != stdhttp.NoBody {
		t.Fatal("expected no body sentinel")
	}
}

func TestCloneForAttemptBodyWithoutGetBody(t *testing.T) {
	t.Parallel()

	request := mustNewRequest(t, stdhttp.MethodPost, strings.NewReader("payload"))
	request.GetBody = nil

	cloned, err := cloneForAttempt(context.Background(), request, defaultMaxReplayBodyBytes)
	if err != nil {
		t.Fatalf("cloneForAttempt returned error: %v", err)
	}
	if request.GetBody == nil {
		t.Fatal("expected original request to become replayable")
	}
	if cloned.Body == request.Body {
		t.Fatal("expected cloned body to use a fresh reader")
	}
	body, readErr := io.ReadAll(cloned.Body)
	if readErr != nil {
		t.Fatalf("failed to read cloned body: %v", readErr)
	}
	if string(body) != "payload" {
		t.Fatalf("unexpected cloned body: %q", string(body))
	}
	_ = cloned.Body.Close()
}

func TestCloneForAttemptBodyWithGetBody(t *testing.T) {
	t.Parallel()

	bodyCalls := atomic.Int32{}
	request := replayableRequest(t, stdhttp.MethodPost, true)
	request.GetBody = func() (io.ReadCloser, error) {
		bodyCalls.Add(1)
		return io.NopCloser(strings.NewReader("payload")), nil
	}

	cloned, err := cloneForAttempt(context.Background(), request, defaultMaxReplayBodyBytes)
	if err != nil {
		t.Fatalf("cloneForAttempt returned error: %v", err)
	}
	if cloned.Body == request.Body {
		t.Fatal("expected body from GetBody, got original body")
	}
	if cloned.GetBody == nil {
		t.Fatal("expected cloned GetBody to be set")
	}
	if bodyCalls.Load() != 1 {
		t.Fatalf("unexpected get body calls: got %d, want 1", bodyCalls.Load())
	}
	_ = cloned.Body.Close()
}

func TestCloneForAttemptGetBodyError(t *testing.T) {
	t.Parallel()

	request := replayableRequest(t, stdhttp.MethodPost, true)
	request.GetBody = func() (io.ReadCloser, error) {
		return nil, errors.New("get body failed")
	}

	_, err := cloneForAttempt(context.Background(), request, defaultMaxReplayBodyBytes)
	if err == nil {
		t.Fatal("expected cloneForAttempt error")
	}
	if !strings.Contains(err.Error(), "get body") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRetryIfWithDefaultRetryPolicyThree503Then200(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		currentAttempt := attempts.Add(1)
		if currentAttempt <= 3 {
			w.WriteHeader(stdhttp.StatusServiceUnavailable)
			_, _ = w.Write([]byte(strings.Repeat("x", 1<<20)))
			return
		}

		w.WriteHeader(stdhttp.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))
	t.Cleanup(server.Close)

	request, err := stdhttp.NewRequestWithContext(context.Background(), stdhttp.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	executor := routery.Apply(
		NewExecutor(server.Client()),
		routery.RetryIf[*stdhttp.Request, *stdhttp.Response](4, 0, DefaultRetryPolicy),
	)

	response, executeErr := executor.Execute(context.Background(), request)
	if executeErr != nil {
		t.Fatalf("unexpected execute error: %v", executeErr)
	}
	if response.StatusCode != stdhttp.StatusOK {
		t.Fatalf("unexpected status code: got %d, want %d", response.StatusCode, stdhttp.StatusOK)
	}
	body, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil {
		t.Fatalf("failed to read response body: %v", readErr)
	}
	if string(body) != "success" {
		t.Fatalf("unexpected response body: %q", string(body))
	}
	if gotAttempts := attempts.Load(); gotAttempts != 4 {
		t.Fatalf("unexpected attempt count: got %d, want 4", gotAttempts)
	}
}

func TestRetryIfClosesAllIntermediateStatusBodies(t *testing.T) {
	t.Parallel()

	body1 := &trackingReadCloser{}
	body2 := &trackingReadCloser{}
	body3 := &trackingReadCloser{}

	transport := &scriptedRoundTripper{
		bodies: []*trackingReadCloser{body1, body2, body3},
	}
	client := &stdhttp.Client{Transport: transport}

	request, reqErr := stdhttp.NewRequestWithContext(context.Background(), stdhttp.MethodGet, "http://example.com", nil)
	if reqErr != nil {
		t.Fatalf("failed to create request: %v", reqErr)
	}
	executor := routery.Apply(
		NewExecutor(client),
		routery.RetryIf[*stdhttp.Request, *stdhttp.Response](4, 0, DefaultRetryPolicy),
	)

	response, err := executor.Execute(context.Background(), request)
	if err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}
	if response.StatusCode != stdhttp.StatusOK {
		t.Fatalf("unexpected status code: got %d, want %d", response.StatusCode, stdhttp.StatusOK)
	}
	_ = response.Body.Close()

	if got := body1.closes.Load(); got != 1 {
		t.Fatalf("unexpected closes for body1: got %d, want 1", got)
	}
	if got := body2.closes.Load(); got != 1 {
		t.Fatalf("unexpected closes for body2: got %d, want 1", got)
	}
	if got := body3.closes.Load(); got != 1 {
		t.Fatalf("unexpected closes for body3: got %d, want 1", got)
	}
}

func TestRetryIfContextCanceledDuringBackoffStopsRetries(t *testing.T) {
	initialGoroutines := runtime.NumGoroutine()

	var attempts atomic.Int32
	firstAttemptCh := make(chan struct{})
	closeOnce := sync.Once{}
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		attempts.Add(1)
		closeOnce.Do(func() {
			close(firstAttemptCh)
		})
		w.WriteHeader(stdhttp.StatusServiceUnavailable)
		_, _ = w.Write([]byte("try again"))
	}))
	defer server.Close()

	request, err := stdhttp.NewRequestWithContext(context.Background(), stdhttp.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-firstAttemptCh
		cancel()
	}()

	executor := routery.Apply(
		NewExecutor(server.Client()),
		routery.RetryIf[*stdhttp.Request, *stdhttp.Response](3, time.Second, DefaultRetryPolicy),
	)

	response, executeErr := executor.Execute(ctx, request)
	if !errors.Is(executeErr, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", executeErr)
	}
	if response != nil {
		t.Fatalf("expected nil response on canceled backoff, got %+v", response)
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("unexpected number of attempts: got %d, want 1", got)
	}

	assertNoGoroutineLeak(t, initialGoroutines, 2)
}

func replayableRequest(t *testing.T, method string, replayable bool) *stdhttp.Request {
	t.Helper()

	request := mustNewRequest(t, method, nil)
	if replayable {
		request.Body = io.NopCloser(strings.NewReader("payload"))
		request.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("payload")), nil
		}
		return request
	}

	request.Body = io.NopCloser(strings.NewReader("payload"))
	request.GetBody = nil
	return request
}

func mustNewRequest(t *testing.T, method string, body io.Reader) *stdhttp.Request {
	t.Helper()

	request, err := stdhttp.NewRequestWithContext(
		context.Background(),
		method,
		"http://example.com",
		body,
	)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	return request
}

func assertNoGoroutineLeak(t *testing.T, initial int, maxDelta int) {
	t.Helper()

	time.Sleep(10 * time.Millisecond)
	final := runtime.NumGoroutine()
	if final > initial+maxDelta {
		t.Fatalf("goroutine leak detected: start=%d end=%d max_delta=%d", initial, final, maxDelta)
	}
}

type flakyNetError struct{}

func (flakyNetError) Error() string   { return "temporary network failure" }
func (flakyNetError) Timeout() bool   { return true }
func (flakyNetError) Temporary() bool { return true }

type scriptedRoundTripper struct {
	calls  atomic.Int32
	bodies []*trackingReadCloser
}

func (transport *scriptedRoundTripper) RoundTrip(*stdhttp.Request) (*stdhttp.Response, error) {
	call := transport.calls.Add(1)
	if call <= int32(len(transport.bodies)) {
		body := transport.bodies[call-1]
		return &stdhttp.Response{
			StatusCode: stdhttp.StatusServiceUnavailable,
			Status:     "503 Service Unavailable",
			Body:       body,
		}, nil
	}

	return &stdhttp.Response{
		StatusCode: stdhttp.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(strings.NewReader("ok")),
	}, nil
}
