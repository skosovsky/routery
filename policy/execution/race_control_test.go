package execution

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/skosovsky/routery"
	"github.com/skosovsky/routery/policy/attempt"
)

func TestRaceControlErrorCannotAuthorizeQueuedDispatch(t *testing.T) {
	for _, failure := range []error{attempt.ErrInvalidEvent, ErrInvalidBoundary,
		fmt.Errorf("wrapped: %w", attempt.ErrInvalidEvent), errors.Join(errors.New("provider"), ErrInvalidBoundary)} {
		t.Run(failure.Error(), func(t *testing.T) {
			// Arrange.
			coordinator, _ := setup(t)
			calls, closes := 0, 0
			race := testRace{Workers: 1, Permissions: readOnlyPermissions(),
				Boundary: testBoundary{
					Fresh: func(context.Context, string) error { return nil },
					Dispatch: func(call routery.RouteCall[string], _ *Receipt) (routery.BasicRouteResult[string], error) {
						calls++
						result := routery.BasicHandled(call.Request)
						result.Lifetime = routery.NewLifetime(func() error { closes++; return nil })
						return result, failure
					},
				},
				Accept: func(context.Context, Result[routery.BasicKind, routery.BasicReason, string]) (bool, error) {
					return true, nil
				}}
			// Act.
			result, err := race.Run(t.Context(), coordinator, racePlans("invalid", "queued"))
			// Assert.
			if !errors.Is(err, failure) || result.Status == RaceAccepted || calls != 1 || closes != 1 {
				t.Fatalf("status=%v err=%v calls=%d closes=%d", result.Status, err, calls, closes)
			}
			entries := result.Journal.Snapshot()
			if len(entries) != 1 || !errors.Is(entries[0].Err, failure) || entries[0].Accepted {
				t.Fatal("fatal attempt accounting was lost")
			}
		})
	}
}

type finalizationBarrierContext struct {
	context.Context

	accepted *atomic.Bool
	checking chan struct{}
	observed chan struct{}
	once     sync.Once
}

func (ctx *finalizationBarrierContext) Err() error {
	if ctx.accepted.Load() {
		ctx.once.Do(func() { close(ctx.checking) })
		<-ctx.observed
	}
	return ctx.Context.Err()
}

func TestRaceFatalDuringWinnerValidationPreventsAcceptance(t *testing.T) {
	// Arrange: the root context pauses checkWinner after initial fatal observation.
	coordinator, _ := setup(t)
	var accepted atomic.Bool
	var winnerCloses atomic.Int32
	checking, observed, badStarted := make(chan struct{}), make(chan struct{}), make(chan struct{})
	ctx := &finalizationBarrierContext{
		Context:  t.Context(),
		accepted: &accepted,
		checking: checking,
		observed: observed,
	}
	race := testRace{Workers: 2, Permissions: readOnlyPermissions(),
		Boundary: testBoundary{Fresh: func(context.Context, string) error { return nil },
			Dispatch: func(call routery.RouteCall[string], receipt *Receipt) (routery.BasicRouteResult[string], error) {
				result := routery.BasicHandled(call.Request)
				if call.Request == "bad" {
					close(badStarted)
					<-checking
					result.Lifetime = routery.NewLifetime(func() error { close(observed); return nil })
					return result, attempt.ErrInvalidEvent
				}
				<-badStarted
				result.Lifetime = routery.NewLifetime(func() error { winnerCloses.Add(1); return nil })
				event, _, err := receipt.Snapshot()
				if err != nil {
					return result, err
				}
				event.Phase, event.Outcome = attempt.Terminal, attempt.Completed
				return result, receipt.Record(event)
			}},
		Accept: func(context.Context, Result[routery.BasicKind, routery.BasicReason, string]) (bool, error) {
			accepted.Store(true)
			return true, nil
		}}
	// Act.
	result, err := race.Run(ctx, coordinator, racePlans("bad", "valid"))
	// Assert.
	if !errors.Is(err, attempt.ErrInvalidEvent) || result.Status == RaceAccepted || winnerCloses.Load() != 1 {
		t.Fatalf("status=%v err=%v closes=%d", result.Status, err, winnerCloses.Load())
	}
}

func TestRaceFatalErrorOverridesConcurrentWinnerBeforeCleanup(t *testing.T) {
	// Arrange.
	coordinator, _ := setup(t)
	cleanupStarted := make(chan struct{})
	validStarted := make(chan struct{})
	releaseCleanup := make(chan struct{})
	badClosed := make(chan struct{})
	var winnerCloses atomic.Int32
	race := testRace{Workers: 2, Permissions: readOnlyPermissions(),
		Boundary: testBoundary{Fresh: func(context.Context, string) error { return nil },
			Dispatch: func(call routery.RouteCall[string], receipt *Receipt) (routery.BasicRouteResult[string], error) {
				result := routery.BasicHandled(call.Request)
				if call.Request == "bad" {
					<-validStarted
					result.Lifetime = routery.NewLifetime(func() error {
						close(cleanupStarted)
						<-releaseCleanup
						close(badClosed)
						return nil
					})
					return result, attempt.ErrInvalidEvent
				}
				close(validStarted)
				<-cleanupStarted
				event, _, err := receipt.Snapshot()
				if err != nil {
					return result, err
				}
				event.Phase, event.Outcome = attempt.Terminal, attempt.Completed
				result.Lifetime = routery.NewLifetime(func() error { winnerCloses.Add(1); return nil })
				return result, receipt.Record(event)
			}},
		Accept: func(context.Context, Result[routery.BasicKind, routery.BasicReason, string]) (bool, error) {
			return true, nil
		}}
	// Act: winner completes while fatal branch cleanup is still blocked.
	result, err := race.Run(t.Context(), coordinator, racePlans("bad", "valid"))
	close(releaseCleanup)
	<-badClosed
	// Assert.
	if !errors.Is(err, attempt.ErrInvalidEvent) || result.Status == RaceAccepted || winnerCloses.Load() != 1 {
		t.Fatalf("status=%v err=%v winnerCloses=%d", result.Status, err, winnerCloses.Load())
	}
	// Late accounting must be retained independently of the return from Run.
	for len(result.Journal.Snapshot()) < 2 {
		select {
		case <-result.Journal.Changes():
		case <-t.Context().Done():
			t.Fatal("late fatal accounting missing")
		}
	}
	entries := result.Journal.Snapshot()
	if len(entries) != 2 {
		t.Fatal("already-started attempt accounting lost")
	}
}

func TestRaceFatalErrorStopsQueueAcrossWorkers(t *testing.T) {
	// Arrange.
	coordinator, err := attempt.NewCoordinator("operation", 4)
	if err != nil {
		t.Fatal(err)
	}
	otherStarted := make(chan struct{})
	fatalObserved := make(chan struct{})
	var calls, closes atomic.Int32
	race := testRace{Workers: 2, Permissions: readOnlyPermissions(),
		Boundary: testBoundary{Fresh: func(context.Context, string) error { return nil },
			Dispatch: func(call routery.RouteCall[string], _ *Receipt) (routery.BasicRouteResult[string], error) {
				calls.Add(1)
				result := routery.BasicHandled(call.Request)
				result.Lifetime = routery.NewLifetime(func() error { closes.Add(1); return nil })
				if call.Request == "bad" {
					<-otherStarted
					result.Lifetime.OnClose(func() { close(fatalObserved) })
					return result, attempt.ErrInvalidEvent
				}
				if call.Request == "rejected" {
					close(otherStarted)
					<-fatalObserved
				}
				return result, nil
			}},
		Accept: func(context.Context, Result[routery.BasicKind, routery.BasicReason, string]) (bool, error) {
			return false, nil
		}}
	// Act.
	result, err := race.Run(t.Context(), coordinator, racePlans("bad", "rejected", "queued-1", "queued-2"))
	// Assert.
	if !errors.Is(err, attempt.ErrInvalidEvent) || calls.Load() != 2 || closes.Load() != 2 {
		t.Fatalf("err=%v calls=%d closes=%d", err, calls.Load(), closes.Load())
	}
	if len(result.Journal.Snapshot()) != 2 {
		t.Fatal("already-started attempts lost or queued attempts started")
	}
}
