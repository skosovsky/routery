package observability_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/skosovsky/routery"
	"github.com/skosovsky/routery/observability"
)

// SafeRecord is a host projection, not a serialization of observability.Event.
type safeRecord struct {
	Action routery.RouteAction `json:"action"`
	Failed bool                `json:"failed"`
}
type safeCollector struct {
	mu      sync.Mutex
	records []safeRecord
	actions map[routery.RouteAction]int
}

func (collector *safeCollector) log(
	_ context.Context,
	event observability.Event[string, routery.BasicKind, routery.BasicReason, io.ReadCloser],
) {
	collector.mu.Lock()
	defer collector.mu.Unlock()
	collector.records = append(collector.records, safeRecord{Action: event.Outcome.Action, Failed: event.Err != nil})
}

func (collector *safeCollector) metric(
	_ context.Context,
	_ string,
	_ time.Duration,
	result observability.ResultMeta[routery.BasicKind, routery.BasicReason],
	_ observability.PayloadMeta,
	_ error,
) {
	collector.mu.Lock()
	defer collector.mu.Unlock()
	collector.actions[result.Action]++
}
func ExampleLogging_safeProjection() {
	// Arrange: callback projects two bounded fields and never exports Request/Err/Match.
	var projected safeRecord
	callback := func(_ context.Context, event observability.Event[string, routery.BasicKind, routery.BasicReason, string]) {
		projected = safeRecord{Action: event.Outcome.Action, Failed: event.Err != nil}
	}
	base := routery.FromFunc(func(context.Context, string) (string, error) { return "private response", nil })
	handler := routery.ApplyRoute(
		base,
		observability.Logging[string, routery.BasicKind, routery.BasicReason, string]("read", callback, nil),
	)
	// Act.
	result, err := routery.InvokeRouteHandler(context.Background(), "private request", handler)
	cleanupErr := result.Lifetime.Close()
	encoded, encodeErr := json.Marshal(projected)
	// Assert.
	if err != nil || cleanupErr != nil || encodeErr != nil {
		fmt.Println("host operation failed")
		return
	}
	fmt.Println(string(encoded))
	// Output: {"action":"stop","failed":false}
}
func TestSafeObserversDoNotOwnPartialCleanupOrPermit(t *testing.T) {
	t.Parallel()
	// Arrange: taint every raw field. The fixture is local; collectors are concurrency-safe.
	const secret = "synthetic-secret-request-error-match"
	collector := &safeCollector{actions: make(map[routery.RouteAction]int)}
	closes := 0
	rawErr := errors.New(secret)
	base := routery.BasicRouteHandler[string, io.ReadCloser](
		func(routery.RouteCall[string]) (routery.BasicRouteResult[io.ReadCloser], error) {
			body := io.NopCloser(strings.NewReader(secret))
			result := routery.Handled[routery.BasicKind, routery.BasicReason, io.ReadCloser](
				routery.BasicKind(secret),
				routery.BasicReason(secret),
				body,
			)
			result.Match.RouteID = routery.RouteID(secret)
			result.Match.DecisionReason = secret
			result.Match.HasDecisionReason = true
			result.Lifetime = routery.NewLifetime(func() error { closes++; return body.Close() })
			return result, rawErr
		},
	)
	handler := routery.ApplyRoute(
		base,
		routery.Bulkhead[string, routery.BasicKind, routery.BasicReason, io.ReadCloser](1),
		observability.Logging[string, routery.BasicKind, routery.BasicReason, io.ReadCloser](
			"read",
			collector.log,
			nil,
		),
		observability.Metrics[string, routery.BasicKind, routery.BasicReason, io.ReadCloser](
			"read",
			observability.MetricsHooks[routery.BasicKind, routery.BasicReason, io.ReadCloser]{
				OnComplete: collector.metric,
			},
		),
	)
	// Act.
	result, err := routery.InvokeRouteHandler(context.Background(), secret, handler)
	_, blocked := routery.InvokeRouteHandler(context.Background(), secret, handler)
	beforeClose := closes
	encoded, encodeErr := json.Marshal(collector.records)
	closeErr := result.Lifetime.Close()
	duplicateErr := result.Lifetime.Close()
	// Assert.
	if !errors.Is(err, rawErr) || !errors.Is(blocked, routery.ErrTooManyRequests) || beforeClose != 0 || closes != 1 ||
		closeErr != nil ||
		duplicateErr != nil ||
		encodeErr != nil {
		t.Fatal(err, blocked, beforeClose, closes, closeErr, duplicateErr, encodeErr)
	}
	if strings.Contains(string(encoded), secret) || len(collector.records) != 1 ||
		collector.actions[routery.ActionAbort] != 1 ||
		len(collector.actions) != 1 {
		t.Fatalf("unsafe projection or labels: %s actions=%v", encoded, collector.actions)
	}
}
