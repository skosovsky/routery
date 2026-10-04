package execution

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/skosovsky/routery"
	"github.com/skosovsky/routery/policy/attempt"
	"github.com/skosovsky/routery/policy/quota"
)

type failureClass uint8

const transientFailure failureClass = 1

type testSequence = Sequence[string, failureClass, routery.BasicKind, routery.BasicReason, string]

func sequenceFixture(t *testing.T, clock *time.Time) testSequence {
	t.Helper()
	return testSequence{
		Classify: func(Result[routery.BasicKind, routery.BasicReason, string], error) (failureClass, error) {
			return transientFailure, nil
		},
		Replay: func(Failure[failureClass]) (attempt.Replay, error) {
			return attempt.Replay{Retryable: true, Replayable: true}, nil
		},
		Schedule: func(Failure[failureClass], attempt.Decision) (attempt.ScheduleInput, error) {
			return attempt.ScheduleInput{Now: *clock}, nil
		},
		Next: func(_ context.Context, previous Step[string], _ attempt.Decision) (Step[string], error) {
			return Step[string]{Request: previous.Request, Identity: attempt.Identity{
				Operation: previous.Identity.Operation, Attempt: previous.Identity.Attempt + "/next",
			}}, nil
		},
		Now:             func() time.Time { return *clock },
		DeadlineContext: syntheticDeadlineContext,
		Wait: func(ctx context.Context, notBefore time.Time) error {
			*clock = notBefore
			return ctx.Err()
		},
		NestedAttemptsKnown: true,
	}
}

// syntheticDeadlineContext declares the fixture clock domain. Tests advance Now
// between callbacks; Sequence checks it before every dispatch authorization.
func syntheticDeadlineContext(ctx context.Context, deadline time.Time) (context.Context, context.CancelFunc) {
	child, cancel := context.WithCancel(ctx)
	return &syntheticDeadline{Context: child, deadline: deadline}, cancel
}

type syntheticDeadline struct {
	context.Context

	deadline time.Time
}

func (ctx *syntheticDeadline) Deadline() (time.Time, bool) { return ctx.deadline, true }

func TestSequenceFallbackHintAndPerAttemptAdmission(t *testing.T) {
	t.Parallel()
	// Arrange.
	coordinator, identity := setup(t)
	initial := time.Unix(100, 0)
	now := initial
	sequence := sequenceFixture(t, &now)
	failure := errors.New("proven failure before execution")
	var stages []string
	var closed bool
	sequence.Boundary = testBoundary{
		Fresh: func(_ context.Context, request string) error { stages = append(stages, "fresh:"+request); return nil },
		Admit: func(_ context.Context, request string, id attempt.Identity) (Admission, error) {
			stages = append(stages, "reserve:"+request)
			if id.Operation != identity.Operation {
				t.Error("operation changed")
			}
			return Admission{Status: quota.Admitted, Finish: func(_ context.Context, _ attempt.Event) error {
				stages = append(stages, "settle:"+request)
				return nil
			}}, nil
		},
		Dispatch: func(call routery.RouteCall[string], receipt *Receipt) (routery.BasicRouteResult[string], error) {
			event, _, err := receipt.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			stages = append(stages, "dispatch:"+call.Request)
			event.Phase = attempt.Terminal
			if call.Request == "A" {
				event.Outcome = attempt.NotExecuted
				result := routery.BasicHandled("buffered")
				result.Lifetime = routery.NewLifetime(func() error { closed = true; return nil })
				return result, errors.Join(failure, receipt.Record(event))
			}
			event.Outcome = attempt.Completed
			return routery.BasicHandled("accepted B"), receipt.Record(event)
		},
		CleanupContext: cleanupContext,
	}
	sequence.Replay = func(failure Failure[failureClass]) (attempt.Replay, error) {
		if failure.Class != transientFailure || failure.Event.Outcome != attempt.NotExecuted || failure.Remaining != 1 {
			t.Error("classification/metadata lost")
		}
		return attempt.Replay{Retryable: true, Replayable: true, UseFallback: true}, nil
	}
	sequence.Schedule = func(Failure[failureClass], attempt.Decision) (attempt.ScheduleInput, error) {
		return attempt.ScheduleInput{
			Backoff: 2 * time.Second, Hint: attempt.Hint{Present: true, NotBefore: initial.Add(30 * time.Second),
				Source: "provider boundary", Clock: "host-clock", Uncertainty: 2 * time.Second},
		}, nil
	}
	sequence.Wait = func(ctx context.Context, notBefore time.Time) error {
		if !closed || len(stages) != 5 || notBefore != initial.Add(32*time.Second) {
			t.Error("backoff retains resources or ignores hint")
		}
		now = notBefore
		return ctx.Err()
	}
	sequence.Next = func(_ context.Context, previous Step[string], decision attempt.Decision) (Step[string], error) {
		if decision.Action != attempt.Fallback {
			t.Error("fallback decision lost")
		}
		return Step[string]{
			Request:  "B",
			Identity: attempt.Identity{Operation: previous.Identity.Operation, Attempt: "physical-B"},
		}, nil
	}
	// Act.
	result, err := sequence.Run(t.Context(), coordinator, Step[string]{Request: "A", Identity: identity})
	// Assert.
	if err != nil || result.Last.Route.Payload != "accepted B" || len(result.Trace) != 2 || result.Failure != nil {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	if !errors.Is(result.Trace[0].Err, failure) || !result.NestedAttemptsKnown || now != initial.Add(32*time.Second) {
		t.Fatal("trace/accounting/schedule lost")
	}
	want := []string{
		"fresh:A",
		"reserve:A",
		"fresh:A",
		"dispatch:A",
		"settle:A",
		"fresh:B",
		"reserve:B",
		"fresh:B",
		"dispatch:B",
		"settle:B",
	}
	if !reflect.DeepEqual(stages, want) {
		t.Fatalf("stages=%v", stages)
	}
}

func TestSequenceUnsafePartialMatrix(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		committed bool
		outcome   attempt.Outcome
		replay    attempt.Replay
		action    attempt.Action
		reason    attempt.Reason
	}{
		{name: "visible partial", committed: true, replay: attempt.Replay{Retryable: true, Replayable: true, SafeDuplicate: true}, action: attempt.Stop, reason: attempt.VisibleOutput},
		{name: "buffered unknown", replay: attempt.Replay{Retryable: true, Replayable: true}, action: attempt.Stop, reason: attempt.UnknownOutcome},
		{name: "unknown reconcile", replay: attempt.Replay{CanReconcile: true}, action: attempt.Reconcile, reason: attempt.UnknownOutcome},
		{name: "not replayable", outcome: attempt.NotExecuted, replay: attempt.Replay{Retryable: true}, action: attempt.Stop, reason: attempt.UnsafeReplay},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange.
			coordinator, identity := setup(t)
			now := time.Unix(100, 0)
			sequence := sequenceFixture(t, &now)
			providerErr := errors.New("partial failure")
			calls := 0
			sequence.Boundary = testBoundary{
				Fresh: func(context.Context, string) error { return nil },
				Dispatch: func(_ routery.RouteCall[string], receipt *Receipt) (routery.BasicRouteResult[string], error) {
					calls++
					result := routery.BasicHandled("partial retained")
					return result, errors.Join(providerErr, receipt.Record(attempt.Event{
						Identity: identity, Phase: attempt.Terminal, Committed: test.committed, Outcome: test.outcome,
					}))
				},
			}
			sequence.Replay = func(Failure[failureClass]) (attempt.Replay, error) { return test.replay, nil }
			// Act.
			result, err := sequence.Run(t.Context(), coordinator, Step[string]{Request: "request", Identity: identity})
			// Assert.
			if err != nil || calls != 1 || result.Decision.Action != test.action ||
				result.Decision.Reason != test.reason {
				t.Fatalf("decision=%+v error=%v calls=%d", result.Decision, err, calls)
			}
			if result.Last.Route.Payload != "partial retained" || result.Failure == nil ||
				!errors.Is(result.Failure.Err, providerErr) {
				t.Fatal("partial result/error discarded")
			}
		})
	}
}
