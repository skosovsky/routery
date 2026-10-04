package execution

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/skosovsky/routery"
	"github.com/skosovsky/routery/policy/attempt"
	"github.com/skosovsky/routery/policy/quota"
)

type testRace = Race[string, routery.BasicKind, routery.BasicReason, string]

func readOnlyPermissions() Permissions {
	return Permissions{Replayable: true, DuplicateCost: true, ReadOnly: true}
}

func racePlans(names ...string) []Step[string] {
	plans := make([]Step[string], len(names))
	for index, name := range names {
		plans[index] = Step[string]{Request: name, Identity: attempt.Identity{Operation: "operation", Attempt: name}}
	}
	return plans
}

func TestRaceRejectsInvalidResultAndOwnsWinner(t *testing.T) {
	t.Parallel()
	// Arrange.
	coordinator, _ := setup(t)
	invalidClosed := make(chan struct{})
	invalidSettled := make(chan struct{})
	winnerContext := make(chan context.Context, 1)
	var invalidCloses, winnerCloses atomic.Int32
	var mu sync.Mutex
	reservations := make(map[attempt.Identity]int)
	settlements := make(map[attempt.Identity]attempt.Event)
	race := testRace{
		Workers: 2, Permissions: readOnlyPermissions(),
		Boundary: testBoundary{
			Fresh: func(context.Context, string) error { return nil },
			Admit: func(_ context.Context, _ string, id attempt.Identity) (Admission, error) {
				mu.Lock()
				reservations[id]++
				mu.Unlock()
				return Admission{Status: quota.Admitted, Finish: func(_ context.Context, event attempt.Event) error {
					mu.Lock()
					settlements[id] = event
					mu.Unlock()
					if id.Attempt == "invalid" {
						close(invalidSettled)
					}
					return nil
				}}, nil
			},
			Dispatch: func(call routery.RouteCall[string], receipt *Receipt) (routery.BasicRouteResult[string], error) {
				event, _, err := receipt.Snapshot()
				if err != nil {
					return routery.AbortResult[routery.BasicKind, routery.BasicReason, string](), err
				}
				event.Phase, event.Outcome = attempt.Terminal, attempt.Completed
				result := routery.BasicHandled(call.Request)
				if call.Request == "invalid" {
					result.Lifetime = routery.NewLifetime(
						func() error { invalidCloses.Add(1); close(invalidClosed); return nil },
					)
				} else {
					<-invalidClosed
					<-invalidSettled
					winnerContext <- call.Context
					result.Lifetime = routery.NewLifetime(func() error { winnerCloses.Add(1); return nil })
				}
				return result, receipt.Record(event)
			},
			CleanupContext: cleanupContext,
		},
		Accept: func(_ context.Context, result Result[routery.BasicKind, routery.BasicReason, string]) (bool, error) {
			return result.Route.Payload == "valid", nil
		},
	}
	// Act.
	result, err := race.Run(t.Context(), coordinator, racePlans("invalid", "valid"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = result.Winner.Route.Lifetime.Close() })
	ctx := <-winnerContext
	// Assert.
	if result.Status != RaceAccepted || result.Winner.Route.Payload != "valid" || invalidCloses.Load() != 1 ||
		winnerCloses.Load() != 0 ||
		ctx.Err() != nil {
		t.Fatalf("result=%+v invalid=%d winner=%d ctx=%v", result, invalidCloses.Load(), winnerCloses.Load(), ctx.Err())
	}
	if err := result.Winner.Route.Lifetime.Close(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reservations) != 2 || len(settlements) != 2 || winnerCloses.Load() != 1 ||
		!errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf(
			"reservations=%v settlements=%v closes=%d ctx=%v",
			reservations,
			settlements,
			winnerCloses.Load(),
			ctx.Err(),
		)
	}
	for _, count := range reservations {
		if count != 1 {
			t.Fatal("shared or duplicated reservation")
		}
	}
}

func TestRaceRequiresExplicitDuplicatePermissions(t *testing.T) {
	t.Parallel()
	for _, permissions := range []Permissions{
		{}, {Replayable: true}, {Replayable: true, DuplicateCost: true}, {DuplicateCost: true, ReadOnly: true},
	} {
		// Arrange.
		coordinator, _ := setup(t)
		var calls atomic.Int32
		race := testRace{Workers: 2, Permissions: permissions,
			Boundary: testBoundary{Fresh: func(context.Context, string) error { return nil },
				Admit: func(context.Context, string, attempt.Identity) (Admission, error) {
					calls.Add(1)
					return Admission{Status: quota.Denied}, nil
				},
				CleanupContext: cleanupContext,
				Dispatch: func(routery.RouteCall[string], *Receipt) (routery.BasicRouteResult[string], error) {
					calls.Add(1)
					return routery.BasicHandled("forbidden"), nil
				}},
			Accept: func(context.Context, Result[routery.BasicKind, routery.BasicReason, string]) (bool, error) {
				return true, nil
			},
		}
		// Act.
		result, err := race.Run(t.Context(), coordinator, racePlans("A", "B"))
		// Assert.
		if err != nil || result.Status != DuplicationForbidden || calls.Load() != 0 {
			t.Fatalf("permissions=%+v result=%+v error=%v calls=%d", permissions, result, err, calls.Load())
		}
	}
}

func TestRaceAttemptBudgetAndDeniedAdmission(t *testing.T) {
	t.Parallel()
	// Arrange.
	coordinator, err := attempt.NewCoordinator("operation", 1)
	if err != nil {
		t.Fatal(err)
	}
	var reserves, dispatches atomic.Int32
	race := testRace{Workers: 1, Permissions: readOnlyPermissions(),
		Boundary: testBoundary{
			Fresh: func(context.Context, string) error { return nil },
			Admit: func(context.Context, string, attempt.Identity) (Admission, error) {
				reserves.Add(1)
				return Admission{Status: quota.Denied}, nil
			},
			Dispatch: func(routery.RouteCall[string], *Receipt) (routery.BasicRouteResult[string], error) {
				dispatches.Add(1)
				return routery.BasicHandled("forbidden"), nil
			},
			CleanupContext: cleanupContext,
		},
		Accept: func(context.Context, Result[routery.BasicKind, routery.BasicReason, string]) (bool, error) {
			return true, nil
		},
	}
	// Act.
	result, err := race.Run(t.Context(), coordinator, racePlans("denied", "out-of-budget"))
	// Assert.
	entries := result.Journal.Snapshot()
	if err != nil || result.Status != NoAccepted || reserves.Load() != 1 || dispatches.Load() != 0 ||
		len(entries) != 2 {
		t.Fatalf(
			"result=%+v error=%v reserves=%d dispatches=%d entries=%d",
			result,
			err,
			reserves.Load(),
			dispatches.Load(),
			len(entries),
		)
	}
	if entries[0].Result.Admission != quota.Denied || !entries[1].Result.BudgetExhausted ||
		entries[1].Identity.Attempt != "out-of-budget" {
		t.Fatal("typed denial/budget metadata lost")
	}
}

func TestRaceDuplicateIdentityAndCancellationNeverDispatch(t *testing.T) {
	t.Parallel()
	// Arrange.
	coordinator, _ := setup(t)
	var calls atomic.Int32
	race := testRace{Workers: 2, Permissions: readOnlyPermissions(),
		Boundary: testBoundary{Fresh: func(context.Context, string) error { return nil },
			Dispatch: func(routery.RouteCall[string], *Receipt) (routery.BasicRouteResult[string], error) {
				calls.Add(1)
				return routery.BasicHandled("forbidden"), nil
			}},
		Accept: func(context.Context, Result[routery.BasicKind, routery.BasicReason, string]) (bool, error) {
			return true, nil
		},
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	// Act.
	_, duplicateErr := race.Run(t.Context(), coordinator, racePlans("duplicate", "duplicate"))
	_, cancelErr := race.Run(ctx, coordinator, racePlans("first", "second"))
	// Assert.
	if !errors.Is(duplicateErr, attempt.ErrInvalidEvent) || !errors.Is(cancelErr, context.Canceled) ||
		calls.Load() != 0 {
		t.Fatalf("duplicate=%v cancellation=%v calls=%d", duplicateErr, cancelErr, calls.Load())
	}
}

func TestRaceCompleteOnlyDoesNotAcceptStreamHandle(t *testing.T) {
	t.Parallel()
	// Arrange.
	coordinator, _ := setup(t)
	var closes, acceptedCallbacks atomic.Int32
	race := testRace{Workers: 1,
		Boundary: testBoundary{Fresh: func(context.Context, string) error { return nil },
			Dispatch: func(_ routery.RouteCall[string], receipt *Receipt) (routery.BasicRouteResult[string], error) {
				event, _, err := receipt.Snapshot()
				if err != nil {
					return routery.AbortResult[routery.BasicKind, routery.BasicReason, string](), err
				}
				event.Phase = attempt.StreamOpened
				result := routery.BasicHandled("first fragment")
				result.Lifetime = routery.NewLifetime(func() error { closes.Add(1); return nil })
				return result, receipt.Record(event)
			}},
		Accept: func(context.Context, Result[routery.BasicKind, routery.BasicReason, string]) (bool, error) {
			acceptedCallbacks.Add(1)
			return true, nil
		},
	}
	// Act.
	result, err := race.Run(t.Context(), coordinator, racePlans("stream"))
	// Assert.
	if err != nil || result.Status != NoAccepted || closes.Load() != 1 || acceptedCallbacks.Load() != 0 {
		t.Fatalf("result=%+v error=%v closes=%d callbacks=%d", result, err, closes.Load(), acceptedCallbacks.Load())
	}
	entries := result.Journal.Snapshot()
	event, _, snapshotErr := entries[0].Result.Receipt.Snapshot()
	if snapshotErr != nil || event.Outcome != attempt.Unknown || event.Phase != attempt.Terminal {
		t.Fatal("discarding a handle inferred zero cost or lost outcome")
	}
}

func TestRaceRejectsCommittedOutputDuringValidation(t *testing.T) {
	t.Parallel()
	// Arrange.
	coordinator, _ := setup(t)
	var closes atomic.Int32
	race := testRace{Workers: 1,
		Boundary: testBoundary{Fresh: func(context.Context, string) error { return nil },
			Dispatch: func(_ routery.RouteCall[string], receipt *Receipt) (routery.BasicRouteResult[string], error) {
				event, _, err := receipt.Snapshot()
				if err != nil {
					return routery.AbortResult[routery.BasicKind, routery.BasicReason, string](), err
				}
				event.Phase = attempt.Terminal
				result := routery.BasicHandled("held output")
				result.Lifetime = routery.NewLifetime(func() error { closes.Add(1); return nil })
				return result, receipt.Record(event)
			}},
		Accept: func(_ context.Context, result Result[routery.BasicKind, routery.BasicReason, string]) (bool, error) {
			event, _, err := result.Receipt.Snapshot()
			if err != nil {
				return false, err
			}
			event.Committed = true
			return true, result.Receipt.Record(event)
		},
	}
	// Act.
	result, err := race.Run(t.Context(), coordinator, racePlans("branch"))
	// Assert.
	if !errors.Is(err, attempt.ErrInvalidEvent) || result.Status != NoAccepted || closes.Load() != 1 {
		t.Fatalf("result=%+v error=%v closes=%d", result, err, closes.Load())
	}
	if result.Journal.Snapshot()[0].Accepted {
		t.Fatal("committed branch output treated as buffered accepted output")
	}
}
