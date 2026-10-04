package observability

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/skosovsky/routery"
	"github.com/skosovsky/routery/policy/attempt"
	"github.com/skosovsky/routery/policy/execution"
	"github.com/skosovsky/routery/policy/quota"
)

func TestObserversPreservePermitAndReceiptThroughComposition(t *testing.T) {
	for _, name := range []string{"logging", "metrics"} {
		t.Run(name, func(t *testing.T) { checkObserverComposition(t, name) })
	}
}

func checkObserverComposition(t *testing.T, name string) {
	t.Helper()
	// Arrange: each provider error returns its own resource; settlement remains unknown.
	failure := errors.New("provider failure")
	var calls, closes, settlements atomic.Int32
	events := make(chan observedPartial, 4)
	leaf := func(routery.RouteCall[int]) (routery.BasicRouteResult[string], error) {
		calls.Add(1)
		result := routery.BasicHandled("partial")
		result.Lifetime = routery.NewLifetime(func() error { closes.Add(1); return nil })
		t.Cleanup(func() { _ = result.Lifetime.Close() })
		return result, failure
	}
	handler := routery.Bulkhead[int, routery.BasicKind, routery.BasicReason, string](
		1,
	)(
		partialObserver(name, events)(leaf),
	)
	boundary := execution.Boundary[int, routery.BasicKind, routery.BasicReason, string]{
		Fresh: func(context.Context, int) error { return nil },
		Admit: func(context.Context, int, attempt.Identity) (execution.Admission, error) {
			return execution.Admission{Status: quota.Admitted,
				Finish: func(_ context.Context, event attempt.Event) error {
					if event.Phase != attempt.Terminal || event.Outcome != attempt.Unknown {
						t.Error("observer or cancellation invented remote outcome")
					}
					settlements.Add(1)
					return nil
				}}, nil
		},
		CleanupContext: func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), time.Second)
		},
		Dispatch: func(call routery.RouteCall[int], _ *execution.Receipt) (routery.BasicRouteResult[string], error) {
			return handler(call)
		},
	}
	coordinator, err := attempt.NewCoordinator("operation", 2)
	if err != nil {
		t.Fatal(err)
	}
	call := routery.NewRouteCall(t.Context(), 42)
	// Act: direct Boundary returns an owned partial; a second call must remain blocked.
	result, err := boundary.Run(call, coordinator, attempt.Identity{Operation: "operation", Attempt: "direct"})
	if !errors.Is(err, failure) || !result.Route.HasPayload || result.Route.Lifetime == nil {
		t.Fatal("observer lost Boundary owned partial")
	}
	_, blocked := handler(call)
	// Assert.
	if !errors.Is(blocked, routery.ErrTooManyRequests) || calls.Load() != 1 || closes.Load() != 0 ||
		settlements.Load() != 0 {
		t.Fatal("observer prematurely released stream permit or settled its receipt")
	}
	_ = result.Route.Lifetime.Close()
	_ = result.Route.Lifetime.Close()
	assertObservedSettlement(t, result.Receipt, closes.Load(), settlements.Load(), 1)
	// Act: a failed FirstCompleted branch discards/closes the next owned partial.
	returned := make(chan *execution.Receipt, 1)
	branch := func(call routery.RouteCall[int]) (routery.BasicRouteResult[string], error) {
		result, runErr := boundary.Run(call, coordinator, attempt.Identity{Operation: "operation", Attempt: "race"})
		returned <- result.Receipt
		return result.Route, runErr
	}
	_, err = routery.FirstCompleted(branch)(call)
	// Assert: resource, Bulkhead permit and independent reservation all close exactly once.
	if !errors.Is(err, failure) {
		t.Fatal("FirstCompleted lost provider error")
	}
	assertObservedSettlement(t, <-returned, closes.Load(), settlements.Load(), 2)
	probe, err := handler(call)
	if !errors.Is(err, failure) || probe.Lifetime == nil || calls.Load() != 3 {
		t.Fatal("discarded FirstCompleted resource retained its permit")
	}
	_ = probe.Lifetime.Close()
	if closes.Load() != 3 || settlements.Load() != 2 || len(events) != 3 {
		t.Fatal("duplicate resource cleanup/settlement or observer dispatch")
	}
}

func assertObservedSettlement(t *testing.T, receipt *execution.Receipt, closes, settlements, want int32) {
	t.Helper()
	if receipt == nil || closes != want || settlements != want {
		t.Fatal("resource/receipt ownership lost or settled twice")
	}
	event, _, err := receipt.Snapshot()
	if err != nil || event.Phase != attempt.Terminal || event.Outcome != attempt.Unknown {
		t.Fatal("receipt lost terminal unknown accounting")
	}
}
