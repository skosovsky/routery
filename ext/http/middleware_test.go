package routeryhttp

import (
	"context"
	"errors"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/skosovsky/routery"
)

func TestTimeoutBodyReadableAfterReturn(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, _ *stdhttp.Request) {
		flusher, ok := w.(stdhttp.Flusher)
		if !ok {
			t.Fatal("response writer does not support flush")
		}

		w.WriteHeader(stdhttp.StatusOK)
		flusher.Flush()
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte("delayed body"))
	}))
	t.Cleanup(server.Close)

	request, err := stdhttp.NewRequestWithContext(context.Background(), stdhttp.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	handler := routery.ApplyRoute(
		NewRouteHandler(server.Client()),
		Timeout(time.Second),
	)

	outcome, executeErr := routery.InvokeRouteHandler(context.Background(), request, handler)
	if executeErr != nil {
		t.Fatalf("execute returned unexpected error: %v", executeErr)
	}
	if !outcome.HasPayload {
		t.Fatal("expected route payload")
	}
	response := outcome.Payload
	t.Cleanup(func() {
		_ = response.Body.Close()
	})

	body, readErr := io.ReadAll(response.Body)
	if readErr != nil {
		t.Fatalf("failed to read response body: %v", readErr)
	}
	if string(body) != "delayed body" {
		t.Fatalf("unexpected response body: %q", string(body))
	}
}

func TestTimeoutCancelOnBodyClose(t *testing.T) {
	t.Parallel()

	done := make(chan struct{})
	base := func(call routery.RouteCall[*stdhttp.Request]) (routery.BasicRouteResult[*stdhttp.Response], error) {
		go func() {
			<-call.Context.Done()
			close(done)
		}()

		return routery.BasicHandled(&stdhttp.Response{
			StatusCode: stdhttp.StatusOK,
			Body:       io.NopCloser(strings.NewReader("body")),
		}), nil
	}

	outcome, err := routery.InvokeRouteHandler(
		context.Background(),
		httptest.NewRequest(stdhttp.MethodGet, "/", nil),
		Timeout(time.Second)(base),
	)
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if !outcome.HasPayload {
		t.Fatal("expected route payload")
	}
	if closeErr := outcome.Payload.Body.Close(); closeErr != nil {
		t.Fatalf("close returned unexpected error: %v", closeErr)
	}

	assertClosed(t, done)
}

func TestTimeoutCancelOnError(t *testing.T) {
	t.Parallel()

	done := make(chan struct{})
	wantErr := errors.New("network failed")
	base := func(call routery.RouteCall[*stdhttp.Request]) (routery.BasicRouteResult[*stdhttp.Response], error) {
		go func() {
			<-call.Context.Done()
			close(done)
		}()

		return routery.AbortResult[routery.BasicKind, routery.BasicReason, *stdhttp.Response](), wantErr
	}

	_, err := routery.InvokeRouteHandler(
		context.Background(),
		httptest.NewRequest(stdhttp.MethodGet, "/", nil),
		Timeout(time.Second)(base),
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("unexpected error: got %v, want %v", err, wantErr)
	}

	assertClosed(t, done)
}

func TestHTTPTimeoutCopiesNextAndIgnoreOutcome(t *testing.T) {
	t.Parallel()

	t.Run("next", func(t *testing.T) {
		t.Parallel()

		base := func(routery.RouteCall[*stdhttp.Request]) (routery.BasicRouteResult[*stdhttp.Response], error) {
			return routery.BasicNext[*stdhttp.Response](routery.BasicReason("delegate")), nil
		}
		result, err := routery.InvokeRouteHandler(
			context.Background(),
			httptest.NewRequest(stdhttp.MethodGet, "/", nil),
			Timeout(time.Second)(base),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Action != routery.ActionNext {
			t.Fatalf("got action %q; want next", result.Action)
		}
		if result.Reason != routery.BasicReason("delegate") {
			t.Fatalf("got reason %q; want delegate", result.Reason)
		}
	})

	t.Run("ignore", func(t *testing.T) {
		t.Parallel()

		base := func(routery.RouteCall[*stdhttp.Request]) (routery.BasicRouteResult[*stdhttp.Response], error) {
			return routery.BasicIgnored[*stdhttp.Response](routery.BasicReason("skip")), nil
		}
		result, err := routery.InvokeRouteHandler(
			context.Background(),
			httptest.NewRequest(stdhttp.MethodGet, "/", nil),
			Timeout(time.Second)(base),
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Action != routery.ActionStop {
			t.Fatalf("got action %q; want stop", result.Action)
		}
		if result.Reason != routery.BasicReason("skip") {
			t.Fatalf("got reason %q; want skip", result.Reason)
		}
	})
}

func TestTimeoutNoBodyResponseCancelsImmediately(t *testing.T) {
	t.Parallel()

	done := make(chan struct{})
	base := func(call routery.RouteCall[*stdhttp.Request]) (routery.BasicRouteResult[*stdhttp.Response], error) {
		go func() {
			<-call.Context.Done()
			close(done)
		}()

		return routery.BasicHandled(&stdhttp.Response{
			StatusCode: stdhttp.StatusNoContent,
			Body:       stdhttp.NoBody,
		}), nil
	}

	outcome, err := routery.InvokeRouteHandler(
		context.Background(),
		httptest.NewRequest(stdhttp.MethodGet, "/", nil),
		Timeout(time.Second)(base),
	)
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if !outcome.HasPayload {
		t.Fatal("expected route payload")
	}
	if outcome.Payload.Body != stdhttp.NoBody {
		t.Fatal("expected no body sentinel")
	}

	assertClosed(t, done)
}

func TestTimeoutDoubleCloseSafe(t *testing.T) {
	t.Parallel()

	base := func(routery.RouteCall[*stdhttp.Request]) (routery.BasicRouteResult[*stdhttp.Response], error) {
		return routery.BasicHandled(&stdhttp.Response{
			StatusCode: stdhttp.StatusOK,
			Body:       io.NopCloser(strings.NewReader("body")),
		}), nil
	}

	outcome, err := routery.InvokeRouteHandler(
		context.Background(),
		httptest.NewRequest(stdhttp.MethodGet, "/", nil),
		Timeout(time.Second)(base),
	)
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if !outcome.HasPayload {
		t.Fatal("expected route payload")
	}
	response := outcome.Payload
	if closeErr := response.Body.Close(); closeErr != nil {
		t.Fatalf("first close returned unexpected error: %v", closeErr)
	}
	if closeErr := response.Body.Close(); closeErr != nil {
		t.Fatalf("second close returned unexpected error: %v", closeErr)
	}
}

func TestCancelTimerBodyCancelOnce(t *testing.T) {
	t.Parallel()

	var cancels atomic.Int32
	body := &cancelTimerBody{
		ReadCloser: io.NopCloser(strings.NewReader("body")),
		cancelOnce: sync.Once{},
		cancel: func() {
			cancels.Add(1)
		},
	}

	if err := body.Close(); err != nil {
		t.Fatalf("first close returned unexpected error: %v", err)
	}
	if err := body.Close(); err != nil {
		t.Fatalf("second close returned unexpected error: %v", err)
	}
	if cancels.Load() != 1 {
		t.Fatalf("unexpected cancel count: got %d, want 1", cancels.Load())
	}
}

func TestTimeoutCancelOnPanicInNext(t *testing.T) {
	t.Parallel()

	const panicValue = "boom"

	done := make(chan struct{})
	base := func(call routery.RouteCall[*stdhttp.Request]) (routery.BasicRouteResult[*stdhttp.Response], error) {
		go func() {
			<-call.Context.Done()
			close(done)
		}()

		panic(panicValue)
	}

	defer func() {
		recovered := recover()
		if recovered != panicValue {
			t.Fatalf("unexpected panic: got %v, want %v", recovered, panicValue)
		}

		assertClosed(t, done)
	}()

	_, _ = routery.InvokeRouteHandler(
		context.Background(),
		httptest.NewRequest(stdhttp.MethodGet, "/", nil),
		Timeout(time.Second)(base),
	)
	t.Fatal("expected panic")
}

func TestTimeoutZeroPassesThrough(t *testing.T) {
	t.Parallel()

	wrapped := Timeout(0)(recordingRouteHandler)
	outcome, err := routery.InvokeRouteHandler(
		context.Background(),
		httptest.NewRequest(stdhttp.MethodGet, "/", nil),
		wrapped,
	)
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if !outcome.HasPayload {
		t.Fatal("expected route payload")
	}
	if outcome.Payload.StatusCode != stdhttp.StatusOK {
		t.Fatalf("unexpected status code: got %d, want %d", outcome.Payload.StatusCode, stdhttp.StatusOK)
	}
}

func TestTimeoutDoesNotCloneRequestShadowGuard(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(stdhttp.MethodGet, "/", nil)
	base := func(call routery.RouteCall[*stdhttp.Request]) (routery.BasicRouteResult[*stdhttp.Response], error) {
		if call.Request != request {
			t.Fatal("timeout middleware cloned the request")
		}

		return routery.BasicHandled(&stdhttp.Response{
			StatusCode: stdhttp.StatusNoContent,
			Body:       stdhttp.NoBody,
		}), nil
	}

	outcome, err := routery.InvokeRouteHandler(context.Background(), request, Timeout(time.Second)(base))
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if !outcome.HasPayload {
		t.Fatal("expected route payload")
	}
	if outcome.Payload.Body != stdhttp.NoBody {
		t.Fatal("expected no body sentinel")
	}
}

func TestTimeoutWithRetryIfPost503NoGetBodyBodyReplayed(t *testing.T) {
	t.Parallel()

	const payload = "payload"

	var attempts atomic.Int32
	bodies := make(chan string, 2)
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read request body: %v", err)
		}
		_ = r.Body.Close()
		bodies <- string(body)

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

	retry := routery.RetryIf[
		*stdhttp.Request,
		routery.BasicKind,
		routery.BasicReason,
		*stdhttp.Response,
	](2, 0, DefaultRetryPolicy)
	handler := routery.ApplyRoute(NewRouteHandler(server.Client()), retry, Timeout(time.Second))

	outcome, executeErr := routery.InvokeRouteHandler(context.Background(), request, handler)
	if executeErr != nil {
		t.Fatalf("execute returned unexpected error: %v", executeErr)
	}
	if !outcome.HasPayload {
		t.Fatal("expected route payload")
	}
	response := outcome.Payload
	t.Cleanup(func() {
		_ = response.Body.Close()
	})

	body, readErr := io.ReadAll(response.Body)
	if readErr != nil {
		t.Fatalf("failed to read response body: %v", readErr)
	}
	if string(body) != "ok" {
		t.Fatalf("unexpected response body: %q", string(body))
	}
	if attempts.Load() != 2 {
		t.Fatalf("unexpected attempt count: got %d, want 2", attempts.Load())
	}
	for attempt := 1; attempt <= 2; attempt++ {
		got := <-bodies
		if got != payload {
			t.Fatalf("unexpected body at attempt %d: %q", attempt, got)
		}
	}
}

func assertClosed(t *testing.T, done <-chan struct{}) {
	t.Helper()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("context was not cancelled")
	}
}

func recordingRouteHandler(
	routery.RouteCall[*stdhttp.Request],
) (routery.BasicRouteResult[*stdhttp.Response], error) {
	return routery.BasicHandled(&stdhttp.Response{
		StatusCode: stdhttp.StatusOK,
		Body:       io.NopCloser(strings.NewReader("ok")),
	}), nil
}
