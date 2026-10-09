//go:build integration

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

func TestIntegrationNewRouteHandlerReturnsResponseForSuccessStatus(t *testing.T) {
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

func TestIntegrationNewRouteHandlerWrapsNon2xxAsStatusError(t *testing.T) {
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

func TestIntegrationDefaultRetryNeverDuplicatesCommittedPostOrPatch(t *testing.T) {
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

func TestIntegrationNewRouteHandlerRespectsRequestContextTimeout(t *testing.T) {
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

func TestIntegrationRetryIfWithDefaultRetryPolicyKeepsFinalBodyOpen(t *testing.T) {
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
