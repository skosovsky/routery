package routeryotel

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/skosovsky/routery"
	"github.com/skosovsky/routery/policy/attempt"
	"github.com/skosovsky/routery/policy/execution"
)

func TestTracingOwnershipThroughBoundaryBulkheadAndFirstCompleted(t *testing.T) {
	// Arrange.
	provider := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })
	failure := errors.New("provider failure")
	var calls, closes, hooks atomic.Int32
	leaf := func(routery.RouteCall[int]) (routery.BasicRouteResult[string], error) {
		calls.Add(1)
		result := routery.BasicHandled("partial")
		result.Lifetime = routery.NewLifetime(func() error { closes.Add(1); return nil })
		result.Lifetime.OnClose(func() { hooks.Add(1) })
		t.Cleanup(func() { _ = result.Lifetime.Close() })
		return result, failure
	}
	traced := Tracing[int, routery.BasicKind, routery.BasicReason, string](provider.Tracer("test"), "owned", nil)(leaf)
	handler := routery.Bulkhead[int, routery.BasicKind, routery.BasicReason, string](1)(traced)
	boundary := execution.Boundary[int, routery.BasicKind, routery.BasicReason, string]{
		Fresh: func(context.Context, int) error { return nil },
		Dispatch: func(call routery.RouteCall[int], _ *execution.Receipt) (routery.BasicRouteResult[string], error) {
			return handler(call)
		}}
	coordinator, err := attempt.NewCoordinator("operation", 2)
	if err != nil {
		t.Fatal(err)
	}
	call := routery.NewRouteCall(t.Context(), 42)
	// Act: an observed error keeps its permit and physical receipt while owned.
	partial, err := boundary.Run(call, coordinator, attempt.Identity{Operation: "operation", Attempt: "direct"})
	if !errors.Is(err, failure) || partial.Route.Lifetime == nil || !partial.Route.HasPayload {
		t.Fatal("tracing lost Boundary partial ownership")
	}
	_, blocked := handler(call)
	// Assert.
	if !errors.Is(blocked, routery.ErrTooManyRequests) || calls.Load() != 1 || closes.Load() != 0 || hooks.Load() != 0 {
		t.Fatal("tracing released an owned partial permit or resource early")
	}
	_ = partial.Route.Lifetime.Close()
	checkTracingReceipt(t, partial.Receipt)
	// Act: FirstCompleted must clean a discarded observed error and release its permit.
	receipts := make(chan *execution.Receipt, 1)
	branch := func(call routery.RouteCall[int]) (routery.BasicRouteResult[string], error) {
		result, runErr := boundary.Run(call, coordinator, attempt.Identity{Operation: "operation", Attempt: "race"})
		receipts <- result.Receipt
		return result.Route, runErr
	}
	_, err = routery.FirstCompleted(branch)(call)
	// Assert.
	if !errors.Is(err, failure) || closes.Load() != 2 || hooks.Load() != 2 {
		t.Fatal("tracing lost discarded error resource or hooks")
	}
	checkTracingReceipt(t, <-receipts)
	probe, err := handler(call)
	if !errors.Is(err, failure) || calls.Load() != 3 || probe.Lifetime == nil {
		t.Fatal("tracing retained discarded-result permit")
	}
	_ = probe.Lifetime.Close()
	_ = probe.Lifetime.Close()
	if closes.Load() != 3 || hooks.Load() != 3 {
		t.Fatal("resource cleanup/hooks ran more than once")
	}
}

func checkTracingReceipt(t *testing.T, receipt *execution.Receipt) {
	t.Helper()
	if receipt == nil {
		t.Fatal("observed physical receipt lost")
	}
	event, _, err := receipt.Snapshot()
	if err != nil || event.Phase != attempt.Terminal || event.Outcome != attempt.Unknown {
		t.Fatal("tracing lost unknown outcome or invented terminal completion")
	}
}
