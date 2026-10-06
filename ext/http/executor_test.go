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

func TestNewRouteHandlerReturnsConfigErrorForNilClient(t *testing.T) {
	t.Parallel()

	handler := NewRouteHandler(nil)
	_, err := routery.InvokeRouteHandler(
		context.Background(),
		httptest.NewRequest(stdhttp.MethodGet, "/", nil),
		handler,
	)
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestNewRouteHandlerReturnsConfigErrorForNilRequest(t *testing.T) {
	t.Parallel()

	handler := NewRouteHandler(stdhttp.DefaultClient)
	_, err := routery.InvokeRouteHandler(context.Background(), nil, handler)
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestNewRouteHandlerReturnsResponseForSuccessStatus(t *testing.T) {
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

	outcome, executeErr := routery.InvokeRouteHandler(context.Background(), request, NewRouteHandler(client))
	if !outcome.HasPayload {
		t.Fatal("expected route payload")
	}
	response := outcome.Payload
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

func TestNewRouteHandlerWrapsNon2xxAsStatusError(t *testing.T) {
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

	outcome, executeErr := routery.InvokeRouteHandler(context.Background(), request, NewRouteHandler(client))
	if executeErr == nil {
		t.Fatal("expected status error")
	}
	if outcome.HasPayload {
		t.Fatal("expected empty outcome on status error")
	}

	var statusErr *StatusError
	if !errors.As(executeErr, &statusErr) {
		t.Fatalf("expected StatusError, got %T", executeErr)
	}
	if statusErr.Response == nil {
		t.Fatal("expected non-nil response on status error")
	}
	t.Cleanup(func() {
		_ = statusErr.Response.Body.Close()
	})
	if statusErr.Code != stdhttp.StatusServiceUnavailable {
		t.Fatalf("unexpected status code: got %d, want %d", statusErr.Code, stdhttp.StatusServiceUnavailable)
	}
}

func TestPrepareRequestBuffersWithoutMutatingOriginal(t *testing.T) {
	// Arrange.
	source := &auditReadCloser{Reader: strings.NewReader("payload")}
	request := mustNewRequest(t, stdhttp.MethodPut, nil)
	request.Body, request.GetBody, request.ContentLength = source, nil, 0
	// Act.
	prepared, err := PrepareRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	first, err := cloneForAttempt(t.Context(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	second, err := cloneForAttempt(t.Context(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Body.Close()
	defer second.Body.Close()
	a, _ := io.ReadAll(first.Body)
	b, _ := io.ReadAll(second.Body)
	// Assert.
	if string(a) != "payload" || string(b) != "payload" || first.Body == second.Body {
		t.Fatalf("independent payloads: %q %q", a, b)
	}
	if request.GetBody != nil || request.ContentLength != 0 || request.Body != source {
		t.Fatal("original fields mutated")
	}
	if source.closes.Load() != 1 || prepared.ContentLength != 7 {
		t.Fatal("ownership or length")
	}
}

func TestPrepareRequestCorrectsTemplateLength(t *testing.T) {
	// Arrange.
	request := mustNewRequest(t, stdhttp.MethodPut, strings.NewReader("payload"))
	request.GetBody = nil
	request.ContentLength = 999
	// Act.
	prepared, err := PrepareRequest(request)
	// Assert.
	if err != nil {
		t.Fatal(err)
	}
	if prepared.ContentLength != 7 || request.ContentLength != 999 {
		t.Fatal("length contract")
	}
}

func TestCloneForAttemptNoBodyNoMutation(t *testing.T) {
	t.Parallel()

	request, err := stdhttp.NewRequestWithContext(context.Background(), stdhttp.MethodGet, "http://example.com", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	cloned, cloneErr := cloneForAttempt(context.Background(), request)
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

func TestDefaultRetryNeverDuplicatesCommittedPostOrPatch(t *testing.T) {
	for _, method := range []string{stdhttp.MethodPost, stdhttp.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			// Arrange.
			var writes atomic.Int32
			server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
				writes.Add(1)
				w.WriteHeader(stdhttp.StatusServiceUnavailable)
			}))
			defer server.Close()
			request, _ := stdhttp.NewRequestWithContext(t.Context(), method, server.URL, strings.NewReader("write"))
			handler := routery.ApplyRoute(
				NewRouteHandler(server.Client()),
				routery.RetryIf[*stdhttp.Request, routery.BasicKind, routery.BasicReason, *stdhttp.Response](
					2,
					0,
					DefaultRetryPolicy,
				),
			)
			// Act.
			_, err := routery.InvokeRouteHandler(t.Context(), request, handler)
			var statusErr *StatusError
			// Assert.
			if !errors.As(err, &statusErr) || writes.Load() != 1 {
				t.Fatalf("writes=%d err=%v", writes.Load(), err)
			}
			statusErr.Response.Body.Close()
		})
	}
}

func TestPrepareRequestFailurePreventsDispatch(t *testing.T) {
	for _, source := range []io.Reader{strings.NewReader("too large"), failingReader{err: errors.New("read failed")}} {
		// Arrange.
		request := mustNewRequest(t, stdhttp.MethodPut, nil)
		body := &auditReadCloser{Reader: source}
		request.Body = body
		// Act.
		prepared, err := PrepareRequest(request, WithMaxReplayBodyBytes(2))
		// Assert.
		if err == nil || prepared != nil || body.closes.Load() != 1 {
			t.Fatalf("prepared=%v err=%v closes=%d", prepared, err, body.closes.Load())
		}
	}
}

func TestUnpreparedBodyRejectedBeforeTransport(t *testing.T) {
	// Arrange.
	var calls atomic.Int32
	client := &stdhttp.Client{Transport: roundTripperFunc(func(*stdhttp.Request) (*stdhttp.Response, error) {
		calls.Add(1)
		return nil, errors.New("unexpected transport")
	})}
	request := mustNewRequest(t, stdhttp.MethodPut, io.NopCloser(strings.NewReader("payload")))
	// Act.
	_, err := routery.InvokeRouteHandler(t.Context(), request, NewRouteHandler(client))
	// Assert.
	if !errors.Is(err, routery.ErrInvalidConfig) || calls.Load() != 0 {
		t.Fatalf("calls=%d err=%v", calls.Load(), err)
	}
	request.Body.Close()
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

func TestPrepareRequestUnlimitedAndExactLimit(t *testing.T) {
	for _, limit := range []int64{0, 7} {
		// Arrange.
		request := mustNewRequest(t, stdhttp.MethodPut, io.NopCloser(strings.NewReader("payload")))
		// Act.
		prepared, err := PrepareRequest(request, WithMaxReplayBodyBytes(limit))
		// Assert.
		if err != nil || prepared.ContentLength != 7 {
			t.Fatalf("limit=%d err=%v", limit, err)
		}
	}
}

func TestNewRouteHandlerRespectsRequestContextTimeout(t *testing.T) {
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

	handler := routery.ApplyRoute(
		NewRouteHandler(client),
		Timeout(16*time.Millisecond),
	)

	_, executeErr := routery.InvokeRouteHandler(context.Background(), request, handler)
	if !errors.Is(executeErr, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", executeErr)
	}
}

func TestDefaultRetryPolicyRetryableStatusPreservesBody(t *testing.T) {
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
	if closeCounter.closes.Load() != 0 {
		t.Fatalf("predicate closed body, got %d", closeCounter.closes.Load())
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

	retry := routery.RetryIf[
		*stdhttp.Request,
		routery.BasicKind,
		routery.BasicReason,
		*stdhttp.Response,
	](2, 0, DefaultRetryPolicy)
	handler := routery.ApplyRoute(NewRouteHandler(client), retry)

	outcome, executeErr := routery.InvokeRouteHandler(context.Background(), request, handler)
	if executeErr == nil {
		t.Fatal("expected status error")
	}
	if outcome.HasPayload {
		t.Fatal("expected empty outcome on exhausted retry")
	}

	var statusErr *StatusError
	if !errors.As(executeErr, &statusErr) {
		t.Fatalf("expected StatusError, got %T", executeErr)
	}
	if statusErr.Response == nil {
		t.Fatal("expected final response on status error")
	}

	bodyBytes, readErr := io.ReadAll(statusErr.Response.Body)
	if readErr != nil {
		t.Fatalf("expected readable final body, got %v", readErr)
	}
	if string(bodyBytes) != "final-response-body" {
		t.Fatalf("unexpected final body: %q", string(bodyBytes))
	}
	if callCounter.Load() != 2 {
		t.Fatalf("unexpected call count: got %d, want 2", callCounter.Load())
	}
	_ = statusErr.Response.Body.Close()
}

func TestRetryIfClosesIntermediateStatusBodies(t *testing.T) {
	t.Parallel()

	closeCounter := &trackingReadCloser{}
	attempts := 0
	request := httptest.NewRequest(stdhttp.MethodGet, "/", nil)

	base := func(routery.RouteCall[*stdhttp.Request]) (routery.BasicRouteResult[*stdhttp.Response], error) {
		attempts++
		if attempts == 1 {
			response := &stdhttp.Response{
				StatusCode: stdhttp.StatusServiceUnavailable,
				Body:       closeCounter,
			}
			partial := routery.BasicHandled(response)
			partial.Lifetime = routery.NewLifetime(response.Body.Close)
			return partial, &StatusError{
				Request:  request,
				Response: response,
				Code:     stdhttp.StatusServiceUnavailable,
			}
		}

		return routery.BasicHandled(&stdhttp.Response{
			StatusCode: stdhttp.StatusOK,
			Body:       io.NopCloser(strings.NewReader("ok")),
		}), nil
	}

	retry := routery.RetryIf[
		*stdhttp.Request,
		routery.BasicKind,
		routery.BasicReason,
		*stdhttp.Response,
	](2, 0, DefaultRetryPolicy)
	handler := routery.ApplyRoute(base, retry)

	outcome, err := routery.InvokeRouteHandler(context.Background(), request, handler)
	if !outcome.HasPayload {
		t.Fatal("expected route payload")
	}
	response := outcome.Payload
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

type auditReadCloser struct {
	io.Reader

	closes atomic.Int32
}

func (body *auditReadCloser) Close() error {
	body.closes.Add(1)
	return nil
}

type roundTripperFunc func(*stdhttp.Request) (*stdhttp.Response, error)

func (fn roundTripperFunc) RoundTrip(request *stdhttp.Request) (*stdhttp.Response, error) {
	return fn(request)
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
