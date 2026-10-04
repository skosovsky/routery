package execution

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/skosovsky/routery"
	"github.com/skosovsky/routery/policy/attempt"
)

func TestHostRetriesClassifiedPreDispatchFailureWithNewPhysicalIdentity(t *testing.T) {
	// Arrange: host owns preflight classification; this is not a backend-failure retry.
	coordinator, firstID := setup(t)
	preflightFailure := errors.New("host preflight temporarily unavailable")
	calls := 0
	boundary := testBoundary{
		Fresh: func(_ context.Context, request string) error {
			if request == "stale-preflight" {
				return preflightFailure
			}
			return nil
		},
		Dispatch: func(_ routery.RouteCall[string], receipt *Receipt) (routery.BasicRouteResult[string], error) {
			calls++
			event, _, err := receipt.Snapshot()
			if err != nil {
				return routery.BasicRouteResult[string]{}, err
			}
			event.Phase, event.Outcome = attempt.Terminal, attempt.Completed
			return routery.BasicHandled("accepted"), receipt.Record(event)
		},
	}
	// Act: first allocation consumes budget, but no external dispatch occurs.
	first, firstErr := boundary.Run(routery.NewRouteCall(t.Context(), "stale-preflight"), coordinator, firstID)
	if !errors.Is(firstErr, preflightFailure) || first.Started || calls != 0 || first.Receipt == nil {
		t.Fatal("preflight failure dispatched or lost its classification")
	}
	event, remaining, err := first.Receipt.Snapshot()
	if err != nil || event.Outcome != attempt.NotExecuted || remaining != 1 {
		t.Fatal("pre-dispatch identity/budget/proof lost")
	}
	// Host permits ONLY this classified preflight failure with explicit proof.
	decision, err := attempt.Decide(t.Context(), event, remaining,
		attempt.Replay{Retryable: true, Replayable: true})
	if err != nil || decision.Action != attempt.Retry {
		t.Fatal("proven-not-executed preflight cannot be repeated")
	}
	now := time.Unix(100, 0)
	decision, err = attempt.Schedule(decision, attempt.ScheduleInput{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if err = attempt.Wait(t.Context(), decision.NotBefore, func() time.Time { return now }); err != nil {
		t.Fatal(err)
	}
	// Host refreshes/rebuilds its request explicitly; no implicit binding replacement.
	secondID := attempt.Identity{Operation: firstID.Operation, Attempt: "refreshed-preflight"}
	second, err := boundary.Run(routery.NewRouteCall(t.Context(), "fresh-preflight"), coordinator, secondID)
	secondEvent, finalRemaining, snapshotErr := second.Receipt.Snapshot()
	// Assert: new physical identity, same operation, one actual provider call, budget exhausted.
	if err != nil || snapshotErr != nil || calls != 1 || !second.Started || second.Route.Payload != "accepted" ||
		secondEvent.Identity != secondID || secondID == firstID || secondID.Operation != firstID.Operation || finalRemaining != 0 {
		t.Fatalf("err=%v calls=%d event=%+v remaining=%d", err, calls, secondEvent, finalRemaining)
	}
}
