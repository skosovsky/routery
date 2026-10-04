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

func failingSequence(t *testing.T, clock *time.Time, outcome attempt.Outcome, calls *int) testSequence {
	t.Helper()
	sequence := sequenceFixture(t, clock)
	sequence.Boundary = testBoundary{
		Fresh: func(context.Context, string) error { return nil },
		Dispatch: func(_ routery.RouteCall[string], receipt *Receipt) (routery.BasicRouteResult[string], error) {
			*calls++
			event, _, err := receipt.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			event.Phase, event.Outcome = attempt.Terminal, outcome
			return routery.BasicHandled("partial"), errors.Join(errors.New("provider failure"), receipt.Record(event))
		},
	}
	return sequence
}

func TestSequenceCancellationAroundBackoff(t *testing.T) {
	t.Parallel()
	for _, delay := range []time.Duration{0, time.Minute} {
		t.Run(delay.String(), func(t *testing.T) {
			t.Parallel()
			// Arrange.
			coordinator, identity := setup(t)
			now := time.Unix(100, 0)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls, nextCalls := 0, 0
			sequence := failingSequence(t, &now, attempt.NotExecuted, &calls)
			sequence.Schedule = func(Failure[failureClass], attempt.Decision) (attempt.ScheduleInput, error) {
				return attempt.ScheduleInput{Backoff: delay}, nil
			}
			sequence.Wait = func(context.Context, time.Time) error { cancel(); return nil }
			sequence.Next = func(context.Context, Step[string], attempt.Decision) (Step[string], error) {
				nextCalls++
				return Step[string]{}, nil
			}
			// Act.
			result, err := sequence.Run(ctx, coordinator, Step[string]{Request: "request", Identity: identity})
			// Assert.
			if !errors.Is(err, context.Canceled) || calls != 1 || nextCalls != 0 || len(result.Trace) != 1 {
				t.Fatalf("error=%v calls=%d next=%d trace=%d", err, calls, nextCalls, len(result.Trace))
			}
		})
	}
}

func TestSequenceHintBeyondDeadlineDefers(t *testing.T) {
	t.Parallel()
	// Arrange.
	coordinator, identity := setup(t)
	now := time.Unix(100, 0)
	calls := 0
	sequence := failingSequence(t, &now, attempt.NotExecuted, &calls)
	sequence.Deadline = now.Add(5 * time.Second)
	sequence.Schedule = func(Failure[failureClass], attempt.Decision) (attempt.ScheduleInput, error) {
		return attempt.ScheduleInput{Deadline: now.Add(time.Hour), Hint: attempt.Hint{
			Present: true, NotBefore: now.Add(30 * time.Second), Source: "provider", Clock: "host",
		}}, nil
	}
	sequence.Wait = func(context.Context, time.Time) error { t.Error("defer waited"); return nil }
	// Act.
	result, err := sequence.Run(t.Context(), coordinator, Step[string]{Request: "request", Identity: identity})
	// Assert.
	if err != nil || result.Decision.Action != attempt.Defer || result.Decision.Reason != attempt.DeadlineExhausted ||
		calls != 1 {
		t.Fatalf("decision=%+v error=%v calls=%d", result.Decision, err, calls)
	}
	if result.Decision.Deadline != sequence.Deadline || result.Decision.NotBefore != now.Add(30*time.Second) ||
		result.Failure == nil {
		t.Fatal("deadline extended or failure lost")
	}
}

func TestSequenceWaitCannotDispatchEarlyOrAfterDeadline(t *testing.T) {
	t.Parallel()
	for _, early := range []bool{true, false} {
		t.Run(map[bool]string{true: "early wait", false: "late wait"}[early], func(t *testing.T) {
			t.Parallel()
			// Arrange.
			coordinator, identity := setup(t)
			now := time.Unix(100, 0)
			calls := 0
			sequence := failingSequence(t, &now, attempt.NotExecuted, &calls)
			sequence.Schedule = func(Failure[failureClass], attempt.Decision) (attempt.ScheduleInput, error) {
				return attempt.ScheduleInput{Backoff: time.Second, Deadline: now.Add(2 * time.Second)}, nil
			}
			sequence.Wait = func(context.Context, time.Time) error {
				if !early {
					now = now.Add(3 * time.Second)
				}
				return nil
			}
			// Act.
			_, err := sequence.Run(t.Context(), coordinator, Step[string]{Request: "request", Identity: identity})
			// Assert.
			want := context.DeadlineExceeded
			if early {
				want = attempt.ErrInvalidHint
			}
			if !errors.Is(err, want) || calls != 1 {
				t.Fatalf("error=%v calls=%d", err, calls)
			}
		})
	}
}

func TestSequenceRechecksLateCommitBeforeRepeat(t *testing.T) {
	t.Parallel()
	// Arrange.
	coordinator, identity := setup(t)
	now := time.Unix(100, 0)
	calls := 0
	sequence := failingSequence(t, &now, attempt.Unknown, &calls)
	sequence.Replay = func(Failure[failureClass]) (attempt.Replay, error) {
		return attempt.Replay{Retryable: true, Replayable: true, SafeDuplicate: true}, nil
	}
	sequence.Wait = func(context.Context, time.Time) error {
		return coordinator.Update(
			attempt.Event{Identity: identity, Phase: attempt.Terminal, Committed: true, Outcome: attempt.Unknown},
		)
	}
	// Act.
	result, err := sequence.Run(t.Context(), coordinator, Step[string]{Request: "request", Identity: identity})
	// Assert.
	if err != nil || result.Decision.Reason != attempt.VisibleOutput || result.Decision.Action != attempt.Stop ||
		calls != 1 {
		t.Fatalf("decision=%+v error=%v calls=%d", result.Decision, err, calls)
	}
	if result.Failure == nil || !result.Failure.Event.Committed {
		t.Fatal("late committed fact lost")
	}
}

func TestSequenceSettlementFailureBlocksRepeat(t *testing.T) {
	t.Parallel()
	// Arrange.
	coordinator, identity := setup(t)
	now := time.Unix(100, 0)
	calls := 0
	sequence := failingSequence(t, &now, attempt.NotExecuted, &calls)
	settlementErr := errors.New("backend unavailable during release")
	sequence.Boundary.Admit = func(context.Context, string, attempt.Identity) (Admission, error) {
		return Admission{
			Status: quota.Admitted,
			Finish: func(context.Context, attempt.Event) error { return settlementErr },
		}, nil
	}
	sequence.Boundary.CleanupContext = cleanupContext
	originalDispatch := sequence.Boundary.Dispatch
	sequence.Boundary.Dispatch = func(call routery.RouteCall[string], receipt *Receipt) (routery.BasicRouteResult[string], error) {
		result, err := originalDispatch(call, receipt)
		result.Lifetime = routery.NewLifetime(nil)
		return result, err
	}
	sequence.Wait = func(context.Context, time.Time) error { t.Error("wait after failed settlement"); return nil }
	// Act.
	result, err := sequence.Run(t.Context(), coordinator, Step[string]{Request: "request", Identity: identity})
	// Assert.
	if !errors.Is(err, settlementErr) || calls != 1 || result.Failure == nil {
		t.Fatalf("result=%+v error=%v calls=%d", result, err, calls)
	}
}

func TestSequenceCancelledBeforeInitialAttempt(t *testing.T) {
	t.Parallel()
	// Arrange.
	coordinator, identity := setup(t)
	now := time.Unix(100, 0)
	calls := 0
	sequence := failingSequence(t, &now, attempt.NotExecuted, &calls)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	// Act.
	result, err := sequence.Run(ctx, coordinator, Step[string]{Request: "request", Identity: identity})
	// Assert.
	if !errors.Is(err, context.Canceled) || calls != 0 || len(result.Trace) != 0 ||
		result.Decision.Action != attempt.Stop ||
		result.Decision.Reason != attempt.Cancelled {
		t.Fatalf("result=%+v error=%v calls=%d", result, err, calls)
	}
}

func TestSequenceUnknownAdmissionAckCannotRetry(t *testing.T) {
	t.Parallel()
	// Arrange.
	coordinator, identity := setup(t)
	now := time.Unix(100, 0)
	calls, reserves := 0, 0
	sequence := failingSequence(t, &now, attempt.NotExecuted, &calls)
	lostAck := &quota.ReserveError{Err: quota.ErrBackendUnavailable, UnknownAck: true}
	sequence.Boundary.Admit = func(context.Context, string, attempt.Identity) (Admission, error) {
		reserves++
		return Admission{Status: quota.Admitted}, lostAck
	}
	sequence.Boundary.CleanupContext = cleanupContext
	sequence.Classify = func(Result[routery.BasicKind, routery.BasicReason, string], error) (failureClass, error) {
		t.Error("classified reservation failure as provider retry")
		return transientFailure, nil
	}
	// Act.
	result, err := sequence.Run(t.Context(), coordinator, Step[string]{Request: "request", Identity: identity})
	// Assert.
	var classified *quota.ReserveError
	if !errors.As(err, &classified) || !classified.UnknownAck || calls != 0 || reserves != 1 ||
		result.Last.Receipt == nil {
		t.Fatalf("result=%+v error=%v calls=%d reserves=%d", result, err, calls, reserves)
	}
	event, remaining, snapshotErr := result.Last.Receipt.Snapshot()
	if snapshotErr != nil || event.Identity != identity || remaining != 1 {
		t.Fatal("uncertain admission identity lost")
	}
}

func TestSequenceMalformedLifecycleCannotBecomeProviderRetry(t *testing.T) {
	// Arrange: a permissive host replay predicate cannot legalize invalid phase facts.
	coordinator, id := setup(t)
	now := time.Unix(100, 0)
	sequence := sequenceFixture(t, &now)
	calls := 0
	sequence.Boundary = testBoundary{
		Fresh: func(context.Context, string) error { return nil },
		Dispatch: func(_ routery.RouteCall[string], receipt *Receipt) (routery.BasicRouteResult[string], error) {
			calls++
			event, _, err := receipt.Snapshot()
			if err != nil {
				return routery.BasicRouteResult[string]{}, err
			}
			event.Phase = attempt.Phase(255)
			return routery.BasicRouteResult[string]{}, receipt.Record(event)
		},
	}
	sequence.Replay = func(Failure[failureClass]) (attempt.Replay, error) {
		return attempt.Replay{Retryable: true, Replayable: true, SafeDuplicate: true}, nil
	}
	// Act.
	result, err := sequence.Run(t.Context(), coordinator, Step[string]{Request: "request", Identity: id})
	// Assert: invalid lifecycle is a control error, not a retryable provider failure.
	if !errors.Is(err, attempt.ErrInvalidEvent) || calls != 1 || len(result.Trace) != 1 ||
		result.Decision.Action != attempt.Stop {
		t.Fatalf("err=%v calls=%d trace=%d decision=%+v", err, calls, len(result.Trace), result.Decision)
	}
}
