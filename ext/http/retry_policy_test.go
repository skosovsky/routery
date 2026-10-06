package routeryhttp

import (
	"context"
	"errors"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"net/url"
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
		if got := statusErr.Error(); got != "routery/ext/http: unexpected status 503" {
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
			wantRetry:  false,
		},
		{
			name:       "patch 503 replayable",
			method:     stdhttp.MethodPatch,
			statusCode: stdhttp.StatusServiceUnavailable,
			replayable: true,
			wantRetry:  false,
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
	cloned, err := cloneForAttempt(context.Background(), request)
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

	cloned, err := cloneForAttempt(context.Background(), request)
	if err != nil {
		t.Fatalf("cloneForAttempt returned error: %v", err)
	}
	if cloned.Body != stdhttp.NoBody {
		t.Fatal("expected no body sentinel")
	}
}

func TestCloneForAttemptRequiresPreparation(t *testing.T) {
	// Arrange.
	request := mustNewRequest(t, stdhttp.MethodPost, strings.NewReader("payload"))
	request.GetBody = nil
	defer request.Body.Close()
	// Act.
	cloned, err := cloneForAttempt(t.Context(), request)
	// Assert.
	if cloned != nil || !errors.Is(err, routery.ErrInvalidConfig) || request.GetBody != nil {
		t.Fatalf("clone=%v err=%v", cloned, err)
	}
}

func TestCloneForAttemptBodyWithGetBody(t *testing.T) {
	t.Parallel()

	bodyCalls := atomic.Int32{}
	request := replayableRequest(t, stdhttp.MethodPost, true)
	request.GetBody = func() (io.ReadCloser, error) {
		bodyCalls.Add(1)
		return io.NopCloser(strings.NewReader("payload")), nil
	}

	cloned, err := cloneForAttempt(context.Background(), request)
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

	_, err := cloneForAttempt(context.Background(), request)
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

	retry := routery.RetryIf[
		*stdhttp.Request,
		routery.BasicKind,
		routery.BasicReason,
		*stdhttp.Response,
	](4, 0, DefaultRetryPolicy)
	handler := routery.ApplyRoute(NewRouteHandler(server.Client()), retry)

	outcome, executeErr := routery.InvokeRouteHandler(context.Background(), request, handler)
	if !outcome.HasPayload {
		t.Fatal("expected route payload")
	}
	response := outcome.Payload
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
	retry := routery.RetryIf[
		*stdhttp.Request,
		routery.BasicKind,
		routery.BasicReason,
		*stdhttp.Response,
	](4, 0, DefaultRetryPolicy)
	handler := routery.ApplyRoute(NewRouteHandler(client), retry)

	outcome, err := routery.InvokeRouteHandler(context.Background(), request, handler)
	if !outcome.HasPayload {
		t.Fatal("expected route payload")
	}
	response := outcome.Payload
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
	t.Parallel()
	// Arrange: no network workers; Done observation after policy approval proves
	// RetryIf has reached its wait select. Cancellation waits for that barrier.
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &retryWaitContext{Context: parent, waiting: make(chan struct{})}
	body := &trackingReadCloser{}
	transport := &scriptedRoundTripper{bodies: []*trackingReadCloser{body}}
	client := &stdhttp.Client{Transport: transport}
	request := mustNewRequest(t, stdhttp.MethodGet, nil)
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		select {
		case <-ctx.waiting:
			cancel()
		case <-parent.Done():
		}
	}()
	defer func() { cancel(); <-workerDone; client.CloseIdleConnections() }()
	predicate := func(ctx context.Context, request *stdhttp.Request, err error) bool {
		allowed := DefaultRetryPolicy(ctx, request, err)
		if allowed {
			ctx.(*retryWaitContext).armed.Store(true)
		}
		return allowed
	}
	handler := routery.ApplyRoute(
		NewRouteHandler(client),
		routery.RetryIf[*stdhttp.Request, routery.BasicKind, routery.BasicReason, *stdhttp.Response](
			3,
			time.Hour,
			predicate,
		),
	)
	// Act.
	outcome, executeErr := routery.InvokeRouteHandler(ctx, request, handler)
	<-workerDone
	// Assert.
	if !errors.Is(executeErr, context.Canceled) || outcome.HasPayload {
		t.Fatalf("result=%+v error=%v", outcome, executeErr)
	}
	if transport.calls.Load() != 1 || body.closes.Load() != 1 {
		t.Fatalf("calls=%d closes=%d", transport.calls.Load(), body.closes.Load())
	}
	select {
	case <-ctx.waiting:
	default:
		t.Fatal("retry wait was not observed")
	}
}

type retryWaitContext struct {
	context.Context

	armed   atomic.Bool
	once    sync.Once
	waiting chan struct{}
}

func (ctx *retryWaitContext) Done() <-chan struct{} {
	if ctx.armed.Load() {
		ctx.once.Do(func() { close(ctx.waiting) })
	}
	return ctx.Context.Done()
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

func TestRetryPolicyVetoRetainsFinalBody(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		// Arrange.
		request := replayableRequest(t, stdhttp.MethodGet, true)
		if explicit {
			request.Method = stdhttp.MethodPost
		}
		body := &trackingReadCloser{}
		response := &stdhttp.Response{StatusCode: stdhttp.StatusServiceUnavailable, Body: body}
		statusErr := &StatusError{Request: request, Response: response, Code: response.StatusCode}
		owner := routery.NewLifetime(body.Close)
		policy := routery.RetryPredicate[*stdhttp.Request](DefaultRetryPolicy)
		if explicit {
			policy = RetryPolicy(
				func(context.Context, *stdhttp.Request, error) ReplaySafety { return VerifiedDeduplication },
			)
		}
		calls := 0
		handler := routery.RetryIf[*stdhttp.Request, routery.BasicKind, routery.BasicReason, *stdhttp.Response](2, 0,
			func(ctx context.Context, req *stdhttp.Request, err error) bool { return policy(ctx, req, err) && false },
		)(func(routery.RouteCall[*stdhttp.Request]) (routery.BasicRouteResult[*stdhttp.Response], error) {
			calls++
			result := routery.BasicHandled(response)
			result.Lifetime = owner
			return result, statusErr
		})
		// Act.
		result, err := handler(routery.NewRouteCall(t.Context(), request))
		// Assert.
		if !errors.Is(err, statusErr) || calls != 1 || body.closes.Load() != 0 || result.Lifetime != owner {
			t.Fatalf("closed=%d calls=%d result=%+v err=%v", body.closes.Load(), calls, result, err)
		}
		_ = result.Lifetime.Close()
		if body.closes.Load() != 1 {
			t.Fatal("final owner did not close exactly once")
		}
	}
}
