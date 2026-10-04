package execution

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/skosovsky/routery"
	"github.com/skosovsky/routery/policy/attempt"
	"github.com/skosovsky/routery/policy/quota"
)

func TestSequenceConsumerResetDoesNotReplaceReplayEvidence(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		reset     bool
		duplicate bool
		calls     int
		reason    attempt.Reason
	}{
		{name: "no reset", duplicate: true, calls: 1, reason: attempt.VisibleOutput},
		{name: "reset alone", reset: true, calls: 1, reason: attempt.UnknownOutcome},
		{name: "reset and duplicate evidence", reset: true, duplicate: true, calls: 2, reason: attempt.ExecutionReturned},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			// Arrange: the host owns the consumer reset protocol and visible content.
			coordinator, id := setup(t)
			now := time.Unix(100, 0)
			sequence := sequenceFixture(t, &now)
			calls, generation := 0, 0
			visible := ""
			sequence.Boundary = testBoundary{
				Fresh: func(context.Context, string) error { return nil },
				Dispatch: func(_ routery.RouteCall[string], receipt *Receipt) (routery.BasicRouteResult[string], error) {
					calls++
					event, _, err := receipt.Snapshot()
					if err != nil {
						return routery.BasicRouteResult[string]{}, err
					}
					event.Phase = attempt.Terminal
					if calls == 1 {
						visible = "partial generation zero"
						event.Committed = true
						return routery.BasicHandled(
								visible,
							), errors.Join(
								errors.New("connection lost"),
								receipt.Record(event),
							)
					}
					visible = "replacement generation one"
					event.Outcome = attempt.Completed
					return routery.BasicHandled(visible), receipt.Record(event)
				},
			}
			sequence.Replay = func(Failure[failureClass]) (attempt.Replay, error) {
				if scenario.reset {
					// Controlled host handshake completed before ResetProtocol becomes true.
					generation++
					visible = ""
				}
				return attempt.Replay{Retryable: true, Replayable: true,
					ResetProtocol: scenario.reset, SafeDuplicate: scenario.duplicate}, nil
			}
			// Act.
			result, err := sequence.Run(t.Context(), coordinator, Step[string]{Request: "request", Identity: id})
			// Assert: reset does not erase irreversible delivery facts or unknown effects.
			if err != nil || calls != scenario.calls || result.Decision.Reason != scenario.reason {
				t.Fatalf("calls=%d decision=%+v err=%v", calls, result.Decision, err)
			}
			assertSequenceReset(t, coordinator, id, result, calls, generation, visible)
		})
	}
}

func assertSequenceReset(
	t *testing.T,
	coordinator *attempt.Coordinator,
	id attempt.Identity,
	result SequenceResult[failureClass, routery.BasicKind, routery.BasicReason, string],
	calls, generation int,
	visible string,
) {
	t.Helper()
	original, _, err := coordinator.Snapshot(id)
	if err != nil || !original.Committed || original.Outcome != attempt.Unknown {
		t.Fatal("reset rewrote original execution facts")
	}
	if calls == 2 && (generation != 1 || visible != "replacement generation one" ||
		len(result.Trace) != 2 || result.Trace[0].Identity == result.Trace[1].Identity) {
		t.Fatal("replacement mixed generations or recycled physical identity")
	}
}

func TestSequenceNestedPhysicalAttemptsConsumeSharedBudget(t *testing.T) {
	// Arrange: a host-visible nested boundary uses the same coordinator and quota port.
	coordinator, outerID := setup(t)
	now := time.Unix(100, 0)
	store := &quotaFixture{limit: 2, states: make(map[string]quota.State)}
	nestedID := attempt.Identity{Operation: outerID.Operation, Attempt: "nested-physical"}
	nestedCalls, outerCalls := 0, 0
	inner := testBoundary{
		Fresh: func(context.Context, string) error { return nil },
		Admit: store.admit, CleanupContext: cleanupContext,
		Dispatch: func(_ routery.RouteCall[string], receipt *Receipt) (routery.BasicRouteResult[string], error) {
			nestedCalls++
			event, _, err := receipt.Snapshot()
			if err != nil {
				return routery.BasicRouteResult[string]{}, err
			}
			event.Phase, event.Outcome = attempt.Terminal, attempt.Completed
			return routery.BasicHandled("nested completed"), receipt.Record(event)
		},
	}
	sequence := sequenceFixture(t, &now)
	sequence.Boundary = testBoundary{
		Fresh: func(context.Context, string) error { return nil },
		Admit: store.admit, CleanupContext: cleanupContext,
		Dispatch: func(call routery.RouteCall[string], _ *Receipt) (routery.BasicRouteResult[string], error) {
			outerCalls++
			if _, err := inner.Run(call, coordinator, nestedID); err != nil {
				return routery.BasicRouteResult[string]{}, err
			}
			// The outer boundary lacks its own terminal usage report; nested proof is
			// not substituted for the separate outer remote outcome or reservation.
			return routery.BasicRouteResult[string]{}, errors.New("outer acknowledgement lost")
		},
	}
	sequence.Replay = func(Failure[failureClass]) (attempt.Replay, error) {
		return attempt.Replay{Retryable: true, Replayable: true, SafeDuplicate: true}, nil
	}
	sequence.Next = func(context.Context, Step[string], attempt.Decision) (Step[string], error) {
		t.Error("nested physical call failed to consume budget")
		return Step[string]{}, nil
	}
	// Act.
	result, err := sequence.Run(t.Context(), coordinator, Step[string]{Request: "request", Identity: outerID})
	// Assert: no unaccounted repeat; outer unknown is retained rather than refunded.
	if err != nil || result.Decision.Reason != attempt.BudgetExhausted || outerCalls != 1 || nestedCalls != 1 ||
		!result.NestedAttemptsKnown || store.actual != 5 || store.live != 1 || len(store.states) != 2 ||
		store.states[outerID.Attempt] != quota.Pending || store.states[nestedID.Attempt] != quota.Committed {
		t.Fatalf("decision=%+v err=%v usage=%d states=%v", result.Decision, err, store.actual, store.states)
	}
}

func TestSequenceReportsUnobservableNestedAttempts(t *testing.T) {
	// Arrange: host explicitly cannot observe physical retries below the boundary.
	coordinator, id := setup(t)
	now := time.Unix(100, 0)
	calls := 0
	sequence := failingSequence(t, &now, attempt.Unknown, &calls)
	sequence.NestedAttemptsKnown = false
	// Act.
	result, err := sequence.Run(t.Context(), coordinator, Step[string]{Request: "request", Identity: id})
	// Assert: do not silently turn one observed boundary into an exact remote count.
	if err != nil || result.NestedAttemptsKnown || calls != 1 || result.Decision.Reason != attempt.UnknownOutcome {
		t.Fatalf("visibility=%v calls=%d err=%v", result.NestedAttemptsKnown, calls, err)
	}
}
