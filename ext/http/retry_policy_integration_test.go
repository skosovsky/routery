//go:build integration

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

func TestIntegrationRetryIfWithDefaultRetryPolicyThree503Then200(t *testing.T) {
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
