package execution

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/skosovsky/routery"
	"github.com/skosovsky/routery/policy/attempt"
	"github.com/skosovsky/routery/policy/quota"
)

type testBoundary = Boundary[string, routery.BasicKind, routery.BasicReason, string]

func setup(t *testing.T) (*attempt.Coordinator, attempt.Identity) {
	t.Helper()
	coordinator, err := attempt.NewCoordinator("operation", 2)
	if err != nil {
		t.Fatal(err)
	}
	return coordinator, attempt.Identity{Operation: "operation", Attempt: "physical-1"}
}

func cleanupContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), time.Second)
}

func TestBoundaryOrderingAndDefinitiveOutcome(t *testing.T) {
	t.Parallel()
	// Arrange.
	coordinator, identity := setup(t)
	var stages []string
	var settled attempt.Event
	boundary := testBoundary{
		Fresh: func(context.Context, string) error { stages = append(stages, "fresh"); return nil },
		Admit: func(context.Context, string, attempt.Identity) (Admission, error) {
			stages = append(stages, "reserve")
			return Admission{Status: quota.Admitted, Finish: func(_ context.Context, event attempt.Event) error {
				settled = event
				stages = append(stages, "settle")
				return nil
			}}, nil
		},
		Dispatch: func(_ routery.RouteCall[string], receipt *Receipt) (routery.BasicRouteResult[string], error) {
			stages = append(stages, "dispatch")
			return routery.BasicHandled("accepted"), receipt.Record(attempt.Event{
				Identity: identity, Phase: attempt.Terminal, Outcome: attempt.Completed,
			})
		},
		CleanupContext: cleanupContext,
	}
	// Act.
	result, err := boundary.Run(routery.NewRouteCall(t.Context(), "request"), coordinator, identity)
	// Assert.
	if err != nil || !result.Started || result.Route.Payload != "accepted" {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	if !reflect.DeepEqual(stages, []string{"fresh", "reserve", "fresh", "dispatch", "settle"}) {
		t.Fatalf("ordering: %v", stages)
	}
	if settled.Outcome != attempt.Completed || settled.Identity != identity {
		t.Fatalf("lost explicit evidence: %+v", settled)
	}
}

func TestBoundaryDeniedDeferredAndCancellation(t *testing.T) {
	t.Parallel()
	for _, status := range []quota.Admission{quota.Denied, quota.Deferred, quota.Admitted} {
		t.Run(
			map[quota.Admission]string{quota.Denied: "denied", quota.Deferred: "deferred", quota.Admitted: "cancel after reserve", quota.Unreserved: "unreserved"}[status],
			func(t *testing.T) {
				t.Parallel()
				checkNoDispatch(t, status)
			},
		)
	}
}

func checkNoDispatch(t *testing.T, status quota.Admission) {
	t.Helper()
	// Arrange.
	coordinator, identity := setup(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var dispatches, releases atomic.Int32
	boundary := testBoundary{
		Fresh: func(context.Context, string) error { return nil },
		Admit: func(context.Context, string, attempt.Identity) (Admission, error) {
			admission := Admission{Status: status}
			if status == quota.Admitted {
				cancel()
				admission.Finish = func(ctx context.Context, event attempt.Event) error {
					if ctx.Err() != nil || event.Outcome != attempt.NotExecuted {
						t.Error("cancelled cleanup or missing no-dispatch proof")
					}
					releases.Add(1)
					return nil
				}
			}
			return admission, nil
		},
		Dispatch: func(routery.RouteCall[string], *Receipt) (routery.BasicRouteResult[string], error) {
			dispatches.Add(1)
			return routery.BasicHandled("forbidden"), nil
		},
		CleanupContext: cleanupContext,
	}
	// Act.
	result, err := boundary.Run(routery.NewRouteCall(ctx, "request"), coordinator, identity)
	// Assert.
	if result.Started || dispatches.Load() != 0 || result.Admission != status {
		t.Fatalf("dispatch escaped admission: %+v", result)
	}
	if status == quota.Admitted {
		if !errors.Is(err, context.Canceled) || releases.Load() != 1 {
			t.Fatalf("error=%v releases=%d", err, releases.Load())
		}
	} else if err != nil || releases.Load() != 0 {
		t.Fatalf("expected typed denial/defer: error=%v releases=%d", err, releases.Load())
	}
}

func TestBoundaryStreamPendingAndLateReconciliation(t *testing.T) {
	t.Parallel()
	// Arrange.
	coordinator, identity := setup(t)
	var settlements []attempt.Event
	var closes atomic.Int32
	boundary := testBoundary{
		Fresh: func(context.Context, string) error { return nil },
		Admit: func(context.Context, string, attempt.Identity) (Admission, error) {
			return Admission{Status: quota.Admitted, Finish: func(_ context.Context, event attempt.Event) error {
				settlements = append(settlements, event)
				return nil
			}}, nil
		},
		Dispatch: func(_ routery.RouteCall[string], receipt *Receipt) (routery.BasicRouteResult[string], error) {
			result := routery.BasicHandled("owned stream")
			result.Lifetime = routery.NewLifetime(func() error { closes.Add(1); return nil })
			return result, receipt.Record(attempt.Event{Identity: identity, Phase: attempt.StreamOpened})
		},
		CleanupContext: cleanupContext,
	}
	// Act.
	result, err := boundary.Run(routery.NewRouteCall(t.Context(), "request"), coordinator, identity)
	if err != nil {
		t.Fatal(err)
	}
	if len(settlements) != 0 {
		t.Fatal("headers prematurely settled the stream")
	}
	if err := result.Route.Lifetime.Close(); err != nil {
		t.Fatal(err)
	}
	if err := result.Route.Lifetime.Close(); err != nil {
		t.Fatal(err)
	}
	late := attempt.Event{Identity: identity, Phase: attempt.Terminal, Outcome: attempt.Completed}
	if err := result.Receipt.Record(late); err != nil {
		t.Fatal(err)
	}
	if err := result.Receipt.Record(late); err != nil {
		t.Fatal(err)
	}
	// Assert.
	if closes.Load() != 1 || len(settlements) != 2 || settlements[0].Outcome != attempt.Unknown ||
		settlements[1] != late {
		t.Fatalf("closes=%d settlements=%+v", closes.Load(), settlements)
	}
}

func TestBoundaryFreshnessAfterAdmission(t *testing.T) {
	t.Parallel()
	// Arrange.
	coordinator, identity := setup(t)
	stale := false
	var releases atomic.Int32
	boundary := testBoundary{
		Fresh: func(context.Context, string) error {
			if stale {
				return routery.ErrStaleSnapshot
			}
			return nil
		},
		Admit: func(context.Context, string, attempt.Identity) (Admission, error) {
			stale = true
			return Admission{Status: quota.Admitted, Finish: func(_ context.Context, event attempt.Event) error {
				if event.Outcome == attempt.NotExecuted {
					releases.Add(1)
				}
				return nil
			}}, nil
		},
		Dispatch: func(routery.RouteCall[string], *Receipt) (routery.BasicRouteResult[string], error) {
			t.Error("stale binding dispatched")
			return routery.BasicHandled("forbidden"), nil
		},
		CleanupContext: cleanupContext,
	}
	// Act.
	result, err := boundary.Run(routery.NewRouteCall(t.Context(), "request"), coordinator, identity)
	// Assert.
	if !errors.Is(err, routery.ErrStaleSnapshot) || result.Started || releases.Load() != 1 {
		t.Fatalf("result=%+v error=%v releases=%d", result, err, releases.Load())
	}
}

func TestBoundaryLocalFailureRetainsUnknown(t *testing.T) {
	t.Parallel()
	// Arrange.
	coordinator, identity := setup(t)
	failure := errors.New("transport lost after sending")
	var settled attempt.Event
	boundary := testBoundary{
		Fresh: func(context.Context, string) error { return nil },
		Admit: func(context.Context, string, attempt.Identity) (Admission, error) {
			return Admission{Status: quota.Admitted, Finish: func(_ context.Context, event attempt.Event) error {
				settled = event
				return nil
			}}, nil
		},
		Dispatch: func(routery.RouteCall[string], *Receipt) (routery.BasicRouteResult[string], error) {
			return routery.AbortResult[routery.BasicKind, routery.BasicReason, string](), failure
		},
		CleanupContext: cleanupContext,
	}
	// Act.
	result, err := boundary.Run(routery.NewRouteCall(t.Context(), "request"), coordinator, identity)
	// Assert.
	if !errors.Is(err, failure) || !result.Started || settled.Outcome != attempt.Unknown ||
		settled.Phase != attempt.Terminal {
		t.Fatalf("result=%+v event=%+v error=%v", result, settled, err)
	}
}

func TestBoundaryLostSettlementAckCanReconcile(t *testing.T) {
	t.Parallel()
	// Arrange.
	coordinator, identity := setup(t)
	lostAck := errors.New("settlement acknowledgement lost")
	var callbacks, effects int
	boundary := testBoundary{
		Fresh: func(context.Context, string) error { return nil },
		Admit: func(context.Context, string, attempt.Identity) (Admission, error) {
			return Admission{Status: quota.Admitted, Finish: func(_ context.Context, event attempt.Event) error {
				if event.Identity != identity {
					t.Error("settlement identity changed")
				}
				callbacks++
				if callbacks == 1 {
					effects++
					return lostAck
				}
				return nil
			}}, nil
		},
		Dispatch: func(_ routery.RouteCall[string], receipt *Receipt) (routery.BasicRouteResult[string], error) {
			return routery.BasicHandled("complete"), receipt.Record(attempt.Event{
				Identity: identity, Phase: attempt.Terminal, Outcome: attempt.Completed,
			})
		},
		CleanupContext: cleanupContext,
	}
	// Act.
	result, err := boundary.Run(routery.NewRouteCall(t.Context(), "request"), coordinator, identity)
	if !errors.Is(err, lostAck) {
		t.Fatalf("hidden settlement error: %v", err)
	}
	_, _, snapshotErr := result.Receipt.Snapshot()
	reconcileErr := result.Receipt.Reconcile()
	_, remaining, finalErr := result.Receipt.Snapshot()
	// Assert.
	if !errors.Is(snapshotErr, lostAck) || reconcileErr != nil || finalErr != nil || callbacks != 2 || effects != 1 ||
		remaining != 1 {
		t.Fatalf(
			"snapshot=%v reconcile=%v final=%v callbacks=%d effects=%d budget=%d",
			snapshotErr,
			reconcileErr,
			finalErr,
			callbacks,
			effects,
			remaining,
		)
	}
}

func TestBoundaryUniqueIdentityAndBudget(t *testing.T) {
	t.Parallel()
	// Arrange.
	coordinator, identity := setup(t)
	var calls atomic.Int32
	boundary := testBoundary{
		Fresh: func(context.Context, string) error { return nil },
		Dispatch: func(routery.RouteCall[string], *Receipt) (routery.BasicRouteResult[string], error) {
			calls.Add(1)
			return routery.BasicHandled("value"), nil
		},
	}
	call := routery.NewRouteCall(t.Context(), "request")
	// Act.
	first, firstErr := boundary.Run(call, coordinator, identity)
	_, duplicateErr := boundary.Run(call, coordinator, identity)
	second, secondErr := boundary.Run(
		call,
		coordinator,
		attempt.Identity{Operation: identity.Operation, Attempt: "physical-2"},
	)
	exhausted, exhaustedErr := boundary.Run(
		call,
		coordinator,
		attempt.Identity{Operation: identity.Operation, Attempt: "physical-3"},
	)
	// Assert.
	if firstErr != nil || secondErr != nil || exhaustedErr != nil || !first.Started || !second.Started {
		t.Fatal("valid attempts failed")
	}
	if !errors.Is(duplicateErr, attempt.ErrInvalidEvent) || !exhausted.BudgetExhausted || exhausted.Started ||
		calls.Load() != 2 {
		t.Fatalf("duplicate=%v exhausted=%+v calls=%d", duplicateErr, exhausted, calls.Load())
	}
}
