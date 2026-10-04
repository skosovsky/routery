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

func TestSequenceLateCommitDuringNextBlocksDispatch(t *testing.T) {
	// Arrange.
	coordinator, id := setup(t)
	now := time.Unix(100, 0)
	calls := 0
	sequence := failingSequence(t, &now, attempt.Unknown, &calls)
	sequence.Replay = func(Failure[failureClass]) (attempt.Replay, error) {
		return attempt.Replay{Retryable: true, Replayable: true, SafeDuplicate: true}, nil
	}
	next := sequence.Next
	sequence.Next = func(ctx context.Context, step Step[string], decision attempt.Decision) (Step[string], error) {
		err := coordinator.Update(attempt.Event{
			Identity: id, Phase: attempt.Terminal, Committed: true, Outcome: attempt.Unknown,
		})
		if err != nil {
			return Step[string]{}, err
		}
		return next(ctx, step, decision)
	}
	// Act.
	result, err := sequence.Run(t.Context(), coordinator, Step[string]{Request: "request", Identity: id})
	// Assert.
	if err != nil || calls != 1 || result.Decision.Reason != attempt.VisibleOutput ||
		result.Last.Route.Payload != "partial" || result.Failure == nil || !result.Failure.Event.Committed {
		t.Fatalf("calls=%d result=%+v err=%v", calls, result, err)
	}
}

func TestSequenceDeadlineCrossedDuringAdmissionBlocksDispatch(t *testing.T) {
	// Arrange.
	coordinator, id := setup(t)
	now := time.Unix(100, 0)
	sequence := sequenceFixture(t, &now)
	sequence.Deadline = now.Add(time.Second)
	calls, releases := 0, 0
	sequence.Boundary = testBoundary{
		Fresh: func(context.Context, string) error { return nil },
		Admit: func(context.Context, string, attempt.Identity) (Admission, error) {
			now = now.Add(2 * time.Second)
			return Admission{Status: quota.Admitted, Finish: func(_ context.Context, event attempt.Event) error {
				if event.Outcome != attempt.NotExecuted {
					t.Error("admitted request was not released with explicit NotExecuted")
				}
				releases++
				return nil
			}}, nil
		},
		Dispatch: func(routery.RouteCall[string], *Receipt) (routery.BasicRouteResult[string], error) {
			calls++
			return routery.BasicHandled("unexpected"), nil
		},
		CleanupContext: cleanupContext,
	}
	// Act.
	result, err := sequence.Run(t.Context(), coordinator, Step[string]{Request: "request", Identity: id})
	// Assert.
	if !errors.Is(err, context.DeadlineExceeded) || calls != 0 || releases != 1 || result.Last.Started {
		t.Fatalf("calls=%d releases=%d result=%+v err=%v", calls, releases, result, err)
	}
}

//nolint:gocognit // Keep the phase matrix and its observable lifecycle assertions together.
func TestSequenceCommitAcrossRepeatPreparation(t *testing.T) {
	for _, phase := range []string{"wait", "next", "fresh before admission", "admit", "fresh after admission", "replay"} {
		for _, reset := range []bool{false, true} {
			t.Run(phase+map[bool]string{false: "/stop", true: "/reset"}[reset], func(t *testing.T) {
				// Arrange.
				coordinator, id := setup(t)
				now := time.Unix(100, 0)
				sequence := sequenceFixture(t, &now)
				calls, reserves, settlements, closes, freshCalls, replayCalls := 0, 0, 0, 0, 0, 0
				var previous *Receipt
				committed := false
				commit := func(at string) {
					if phase != at || committed {
						return
					}
					committed = true
					event, _, err := previous.Snapshot()
					if err != nil {
						t.Fatal(err)
					}
					event.Committed = true
					if err = previous.Record(event); err != nil {
						t.Fatal(err)
					}
				}
				sequence.Boundary = testBoundary{
					Fresh: func(context.Context, string) error {
						freshCalls++
						if freshCalls == 3 {
							commit("fresh before admission")
						}
						if freshCalls == 4 {
							commit("fresh after admission")
						}
						return nil
					},
					Admit: func(context.Context, string, attempt.Identity) (Admission, error) {
						reserves++
						if reserves == 2 {
							commit("admit")
						}
						return Admission{
							Status: quota.Admitted,
							Finish: func(ctx context.Context, event attempt.Event) error {
								settlements++
								if ctx.Err() != nil {
									t.Error("cleanup inherited invocation cancellation")
								}
								if event.Identity != id && !reset && event.Outcome != attempt.NotExecuted {
									t.Error("undispatched reservation lost release proof")
								}
								return nil
							},
						}, nil
					},
					Dispatch: func(_ routery.RouteCall[string], receipt *Receipt) (routery.BasicRouteResult[string], error) {
						calls++
						event, _, err := receipt.Snapshot()
						if err != nil {
							return routery.BasicRouteResult[string]{}, err
						}
						event.Phase = attempt.Terminal
						if calls == 1 {
							previous = receipt
							partial := routery.BasicHandled("partial")
							partial.Lifetime = routery.NewLifetime(func() error { closes++; return nil })
							return partial, errors.Join(errors.New("lost response"), receipt.Record(event))
						}
						event.Outcome = attempt.Completed
						return routery.BasicHandled("replacement"), receipt.Record(event)
					},
					CleanupContext: cleanupContext,
				}
				sequence.Replay = func(Failure[failureClass]) (attempt.Replay, error) {
					replayCalls++
					if replayCalls > 1 {
						commit("replay")
					}
					return attempt.Replay{
						Retryable:     true,
						Replayable:    true,
						SafeDuplicate: true,
						ResetProtocol: reset,
					}, nil
				}
				wait, next := sequence.Wait, sequence.Next
				sequence.Wait = func(ctx context.Context, at time.Time) error { commit("wait"); return wait(ctx, at) }
				sequence.Next = func(ctx context.Context, step Step[string], decision attempt.Decision) (Step[string], error) {
					commit("next")
					return next(ctx, step, decision)
				}
				// Act.
				result, err := sequence.Run(t.Context(), coordinator, Step[string]{Request: "request", Identity: id})
				// Assert.
				assertCommitPreparation(t, result, err, phase, reset, committed,
					repeatCounts{calls: calls, reserves: reserves, settlements: settlements, closes: closes})
			})
		}
	}
}

type repeatCounts struct {
	calls, reserves, settlements, closes int
}

func assertCommitPreparation(
	t *testing.T, result SequenceResult[failureClass, routery.BasicKind, routery.BasicReason, string],
	err error, phase string, reset, committed bool, counts repeatCounts,
) {
	t.Helper()
	wantCalls, wantReserves := 1, 2
	if reset {
		wantCalls = 2
	}
	if phase == "wait" && !reset {
		wantReserves = 1
	}
	if err != nil || !committed || counts.calls != wantCalls || counts.reserves != wantReserves ||
		counts.closes != 1 || counts.settlements != wantReserves+1 || len(result.Trace) != wantReserves {
		t.Fatalf("counts=%+v trace=%d err=%v", counts, len(result.Trace), err)
	}
	if !reset && (result.Last.Route.Payload != "partial" || result.Decision.Reason != attempt.VisibleOutput ||
		result.Failure == nil || !result.Failure.Event.Committed) {
		t.Fatalf("partial/facts lost: %+v", result)
	}
	if reset && result.Last.Route.Payload != "replacement" {
		t.Fatal("authorized reset failed")
	}
}

//nolint:gocognit // Keep the phase matrix and its observable lifecycle assertions together.
func TestSequenceDeadlineAcrossPreparation(t *testing.T) {
	for _, phase := range []string{"fresh first", "admit first", "fresh second", "wait", "next", "fresh third", "admit second", "fresh fourth"} {
		t.Run(phase, func(t *testing.T) {
			// Arrange.
			coordinator, id := setup(t)
			now := time.Unix(100, 0)
			calls, reserves, releases, freshCalls := 0, 0, 0, 0
			sequence := failingSequence(t, &now, attempt.NotExecuted, &calls)
			sequence.Deadline = now.Add(time.Second)
			expire := func(at string) {
				if phase == at {
					now = now.Add(2 * time.Second)
				}
			}
			sequence.Boundary.Fresh = func(context.Context, string) error {
				freshCalls++
				expire(
					map[int]string{1: "fresh first", 2: "fresh second", 3: "fresh third", 4: "fresh fourth"}[freshCalls],
				)
				return nil
			}
			sequence.Boundary.Admit = func(context.Context, string, attempt.Identity) (Admission, error) {
				reserves++
				expire(map[int]string{1: "admit first", 2: "admit second"}[reserves])
				return Admission{Status: quota.Admitted, Finish: func(ctx context.Context, event attempt.Event) error {
					if ctx.Err() != nil || event.Outcome != attempt.NotExecuted {
						t.Error("invalid release context/proof")
					}
					releases++
					return nil
				}}, nil
			}
			sequence.Boundary.CleanupContext = cleanupContext
			wait, next := sequence.Wait, sequence.Next
			sequence.Wait = func(ctx context.Context, at time.Time) error { err := wait(ctx, at); expire("wait"); return err }
			sequence.Next = func(ctx context.Context, step Step[string], decision attempt.Decision) (Step[string], error) {
				expire("next")
				return next(ctx, step, decision)
			}
			// Act.
			result, err := sequence.Run(t.Context(), coordinator, Step[string]{Request: "request", Identity: id})
			// Assert.
			wantCalls := 1
			if phase == "fresh first" || phase == "admit first" || phase == "fresh second" {
				wantCalls = 0
			}
			if !errors.Is(err, context.DeadlineExceeded) || calls != wantCalls || releases != reserves {
				t.Fatalf("calls=%d reserves=%d releases=%d result=%+v err=%v", calls, reserves, releases, result, err)
			}
			if calls == 1 && result.Last.Route.Payload != "partial" {
				t.Fatal("prior partial output lost")
			}
		})
	}
}

func TestSequenceRefreshesReplayAfterAdmission(t *testing.T) {
	// Arrange.
	coordinator, id := setup(t)
	now := time.Unix(100, 0)
	calls, reserves := 0, 0
	sequence := failingSequence(t, &now, attempt.Unknown, &calls)
	sequence.Boundary.Admit = func(context.Context, string, attempt.Identity) (Admission, error) {
		reserves++
		return Admission{Status: quota.Unreserved}, nil
	}
	sequence.Boundary.CleanupContext = cleanupContext
	sequence.Replay = func(Failure[failureClass]) (attempt.Replay, error) {
		return attempt.Replay{Retryable: true, Replayable: true, SafeDuplicate: reserves < 2, ResetProtocol: true}, nil
	}
	// Act.
	result, err := sequence.Run(t.Context(), coordinator, Step[string]{Request: "request", Identity: id})
	// Assert.
	if err != nil || calls != 1 || result.Decision.Reason != attempt.UnknownOutcome || len(result.Trace) != 2 {
		t.Fatalf("calls=%d result=%+v err=%v", calls, result, err)
	}
}

//nolint:gocognit // Exercise both Record orderings against real receipts with the same fixture.
func TestSequenceConcurrentRecordAroundAuthorization(t *testing.T) {
	for _, before := range []bool{true, false} {
		t.Run(map[bool]string{true: "before", false: "after"}[before], func(t *testing.T) {
			// Arrange: one producer records a late fact on a closed prior Receipt.
			coordinator, id := setup(t)
			now := time.Unix(100, 0)
			sequence := sequenceFixture(t, &now)
			trigger, recorded := make(chan *Receipt), make(chan error, 1)
			go func() {
				receipt := <-trigger
				event, _, err := receipt.Snapshot()
				if err == nil {
					event.Committed = true
					recorded <- receipt.Record(event)
				} else {
					recorded <- err
				}
			}()
			var prior *Receipt
			calls, settlements := 0, 0
			record := func() {
				trigger <- prior
				if err := <-recorded; err != nil {
					t.Fatal(err)
				}
			}
			sequence.Boundary = testBoundary{
				Fresh: func(context.Context, string) error { return nil },
				Admit: func(context.Context, string, attempt.Identity) (Admission, error) {
					return Admission{
						Status: quota.Admitted,
						Finish: func(context.Context, attempt.Event) error { settlements++; return nil },
					}, nil
				},
				Dispatch: func(_ routery.RouteCall[string], receipt *Receipt) (routery.BasicRouteResult[string], error) {
					calls++
					if calls == 1 {
						prior = receipt
						return routery.BasicHandled("partial"), errors.New("lost acknowledgement")
					}
					// Entering Dispatch is after the explicit authorization point.
					record()
					return routery.BasicHandled("replacement"), nil
				},
				CleanupContext: cleanupContext,
			}
			sequence.Replay = func(Failure[failureClass]) (attempt.Replay, error) {
				return attempt.Replay{Retryable: true, Replayable: true, SafeDuplicate: true}, nil
			}
			next := sequence.Next
			sequence.Next = func(ctx context.Context, step Step[string], decision attempt.Decision) (Step[string], error) {
				if before {
					record()
				}
				return next(ctx, step, decision)
			}
			// Act.
			result, err := sequence.Run(t.Context(), coordinator, Step[string]{Request: "request", Identity: id})
			// Assert: before blocks, after remains visible without pretending rollback.
			wantCalls := 2
			if before {
				wantCalls = 1
			}
			event, remaining, snapshotErr := prior.Snapshot()
			if err != nil || snapshotErr != nil || calls != wantCalls || !event.Committed ||
				event.Outcome != attempt.Unknown ||
				remaining != 0 ||
				settlements != 3 ||
				len(result.Trace) != 2 {
				t.Fatalf(
					"calls=%d event=%+v remaining=%d settlements=%d result=%+v err=%v/%v",
					calls,
					event,
					remaining,
					settlements,
					result,
					err,
					snapshotErr,
				)
			}
			if before && result.Decision.Reason != attempt.VisibleOutput {
				t.Fatal("early commit ignored")
			}
			if !before && result.Last.Route.Payload != "replacement" {
				t.Fatal("late fact revoked authorized result")
			}
		})
	}
}
