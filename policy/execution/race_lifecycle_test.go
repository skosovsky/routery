package execution

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/skosovsky/routery"
	"github.com/skosovsky/routery/policy/attempt"
	"github.com/skosovsky/routery/policy/quota"
)

func waitEntries(
	t *testing.T,
	journal *Journal[routery.BasicKind, routery.BasicReason, string],
	count int,
) []RaceEntry[routery.BasicKind, routery.BasicReason, string] {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	for {
		entries := journal.Snapshot()
		if len(entries) >= count {
			return entries
		}
		select {
		case <-ctx.Done():
			t.Fatal("missing returned attempt journal entries")
		case <-journal.Changes():
		}
	}
}

func observedAdmission(
	store *quotaFixture,
	settlements chan<- attempt.Event,
) func(context.Context, string, attempt.Identity) (Admission, error) {
	return func(ctx context.Context, request string, id attempt.Identity) (Admission, error) {
		admission, err := store.admit(ctx, request, id)
		if err != nil || admission.Finish == nil {
			return admission, err
		}
		finish := admission.Finish
		admission.Finish = func(ctx context.Context, event attempt.Event) error {
			if err := finish(ctx, event); err != nil {
				return err
			}
			settlements <- event
			return nil
		}
		return admission, nil
	}
}

func TestRaceEarlyWinnerAndLateLoserRemainAccounted(t *testing.T) {
	t.Parallel()
	// Arrange.
	coordinator, _ := setup(t)
	loserStarted, allowLate := make(chan struct{}), make(chan struct{})
	loserCancelled := make(chan struct{})
	winnerContext := make(chan context.Context, 1)
	settlements := make(chan attempt.Event, 8)
	store := &quotaFixture{limit: 2, states: make(map[string]quota.State)}
	var closes atomic.Int32
	race := testRace{Workers: 2, Permissions: readOnlyPermissions(), Profile: EarlyOwned,
		Boundary: testBoundary{
			Fresh: func(context.Context, string) error { return nil },
			Admit: observedAdmission(store, settlements),
			Dispatch: func(call routery.RouteCall[string], receipt *Receipt) (routery.BasicRouteResult[string], error) {
				if call.Request == "loser" {
					close(loserStarted)
					<-call.Context.Done()
					close(loserCancelled)
					<-allowLate
				} else {
					<-loserStarted
					winnerContext <- call.Context
				}
				event, _, err := receipt.Snapshot()
				if err != nil {
					return routery.AbortResult[routery.BasicKind, routery.BasicReason, string](), err
				}
				event.Phase = attempt.StreamOpened
				result := routery.BasicHandled(call.Request)
				result.Lifetime = routery.NewLifetime(func() error { closes.Add(1); return nil })
				return result, receipt.Record(event)
			},
			CleanupContext: cleanupContext,
		},
		Accept: func(_ context.Context, result Result[routery.BasicKind, routery.BasicReason, string]) (bool, error) {
			return result.Route.Payload == "winner", nil
		},
	}
	// Act.
	result, err := race.Run(t.Context(), coordinator, racePlans("winner", "loser"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = result.Winner.Route.Lifetime.Close() })
	ctx := <-winnerContext
	<-loserCancelled
	close(allowLate)
	entries := waitEntries(t, result.Journal, 2)
	// Assert.
	if result.Status != RaceAccepted || result.Winner.Route.Payload != "winner" || ctx.Err() != nil ||
		closes.Load() != 1 {
		t.Fatalf("result=%+v context=%v closes=%d", result, ctx.Err(), closes.Load())
	}
	loserUsage := <-settlements
	if loserUsage.Identity.Attempt != "loser" || loserUsage.Outcome != attempt.Unknown ||
		loserUsage.Phase != attempt.Terminal {
		t.Fatalf("loser cancellation inferred no cost: %+v", loserUsage)
	}
	if len(entries) != 2 || entries[0].Identity.Attempt == entries[1].Identity.Attempt {
		t.Fatal("lost independent attempt accounting")
	}
	if store.live != 2 || store.states["loser"] != quota.Pending || store.states["winner"] != quota.Reserved {
		t.Fatal("loser cancellation refunded quota or winner headers settled prematurely")
	}
	if err := result.Winner.Route.Lifetime.Close(); err != nil {
		t.Fatal(err)
	}
	winnerUsage := <-settlements
	if winnerUsage.Identity.Attempt != "winner" || winnerUsage.Outcome != attempt.Unknown || closes.Load() != 2 ||
		!errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("winner=%+v closes=%d context=%v", winnerUsage, closes.Load(), ctx.Err())
	}
	verifyRaceOverage(t, entries, store)
}

func verifyRaceOverage(
	t *testing.T,
	entries []RaceEntry[routery.BasicKind, routery.BasicReason, string],
	store *quotaFixture,
) {
	t.Helper()
	for _, entry := range entries {
		event := attempt.Event{Identity: entry.Identity, Phase: attempt.Terminal, Outcome: attempt.Completed}
		if err := entry.Result.Receipt.Record(event); err != nil {
			t.Fatal(err)
		}
		if err := entry.Result.Receipt.Reconcile(); err != nil {
			t.Fatal(err)
		}
	}
	if store.live != 0 || store.actual != 10 || len(store.states) != 2 {
		t.Fatalf("lost per-branch usage/overage: live=%d actual=%d states=%v", store.live, store.actual, store.states)
	}
}

func TestRaceConcurrencyIncludesOwnedResources(t *testing.T) {
	t.Parallel()
	// Arrange.
	coordinator, err := attempt.NewCoordinator("operation", 4)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 4)
	release, rejectedClosed := make(chan struct{}), make(chan struct{})
	var held, maximum, rejects, calls atomic.Int32
	race := testRace{Workers: 2, Permissions: readOnlyPermissions(),
		Boundary: testBoundary{
			Fresh: func(context.Context, string) error { return nil },
			Dispatch: func(call routery.RouteCall[string], receipt *Receipt) (routery.BasicRouteResult[string], error) {
				calls.Add(1)
				active := held.Add(1)
				for previous := maximum.Load(); active > previous; previous = maximum.Load() {
					if maximum.CompareAndSwap(previous, active) {
						break
					}
				}
				started <- struct{}{}
				<-release
				event, _, snapshotErr := receipt.Snapshot()
				if snapshotErr != nil {
					t.Fatal(snapshotErr)
				}
				event.Phase, event.Outcome = attempt.Terminal, attempt.Completed
				result := routery.BasicHandled(call.Request)
				result.Lifetime = routery.NewLifetime(func() error {
					held.Add(-1)
					if call.Request != "winner" && rejects.Add(1) == 3 {
						close(rejectedClosed)
					}
					return nil
				})
				return result, receipt.Record(event)
			},
		},
		Accept: func(_ context.Context, result Result[routery.BasicKind, routery.BasicReason, string]) (bool, error) {
			if result.Route.Payload == "winner" {
				<-rejectedClosed
				return true, nil
			}
			return false, nil
		},
	}
	type response struct {
		result RaceResult[routery.BasicKind, routery.BasicReason, string]
		err    error
	}
	done := make(chan response, 1)
	go func() {
		result, runErr := race.Run(t.Context(), coordinator, racePlans("first", "second", "third", "winner"))
		done <- response{result: result, err: runErr}
	}()
	// Act.
	<-started
	<-started
	if calls.Load() != 2 || held.Load() != 2 {
		t.Fatal("unbounded dispatch before barrier")
	}
	close(release)
	got := <-done
	t.Cleanup(func() { _ = got.result.Winner.Route.Lifetime.Close() })
	// Assert.
	if got.err != nil || got.result.Status != RaceAccepted || calls.Load() != 4 || maximum.Load() != 2 ||
		held.Load() != 1 {
		t.Fatalf(
			"error=%v status=%v calls=%d maximum=%d held=%d",
			got.err,
			got.result.Status,
			calls.Load(),
			maximum.Load(),
			held.Load(),
		)
	}
	if err := got.result.Winner.Route.Lifetime.Close(); err != nil {
		t.Fatal(err)
	}
	if held.Load() != 0 {
		t.Fatal("winner resource permit retained after close")
	}
}
