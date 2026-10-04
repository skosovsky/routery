package execution

import (
	"context"
	"sync"
	"testing"

	"github.com/skosovsky/routery"
	"github.com/skosovsky/routery/policy/attempt"
	"github.com/skosovsky/routery/policy/quota"
)

// This is an in-memory conformance fixture, not a production quota store.
type quotaFixture struct {
	mu     sync.Mutex
	live   int
	limit  int
	actual uint64
	states map[string]quota.State
}

func (store *quotaFixture) Reserve(
	_ context.Context,
	request quota.ReserveRequest[string, string],
) (quota.Reservation[string, string], error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	id := request.Identity.Attempt
	if _, exists := store.states[id]; exists {
		return quota.Reservation[string, string]{Admission: quota.Admitted, Handle: id}, nil
	}
	if store.live >= max(1, store.limit) {
		return quota.Reservation[string, string]{Admission: quota.Denied}, nil
	}
	store.live++
	store.states[id] = quota.Reserved
	return quota.Reservation[string, string]{Admission: quota.Admitted, Handle: id}, nil
}

func (store *quotaFixture) Commit(_ context.Context, settlement quota.Settlement[string, string]) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	state, exists := store.states[settlement.Handle]
	if !exists {
		return quota.ErrUnknownHandle
	}
	if state == quota.Released {
		return quota.ErrConflict
	}
	if state != quota.Committed {
		store.actual += settlement.Actual["units"]
		store.live--
		store.states[settlement.Handle] = quota.Committed
	}
	return nil
}

func (store *quotaFixture) Release(_ context.Context, proof quota.ReleaseProof[string, string]) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	state, exists := store.states[proof.Handle]
	if !exists {
		return quota.ErrUnknownHandle
	}
	if state == quota.Committed {
		return quota.ErrConflict
	}
	if state != quota.Released {
		store.live--
		store.states[proof.Handle] = quota.Released
	}
	return nil
}

func (store *quotaFixture) Pending(_ context.Context, settlement quota.Settlement[string, string]) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	state, exists := store.states[settlement.Handle]
	if !exists {
		return quota.ErrUnknownHandle
	}
	if state == quota.Committed || state == quota.Released {
		return quota.ErrConflict
	}
	store.states[settlement.Handle] = quota.Pending
	return nil
}

func (store *quotaFixture) admit(ctx context.Context, _ string, id attempt.Identity) (Admission, error) {
	reservation, session, err := quota.Admit(ctx, store, quota.ReserveRequest[string, string]{
		Scope: "trusted-account", Identity: id, Estimated: map[string]uint64{"units": 1}, Fingerprint: "quota-policy",
	}, quota.FailClosed)
	admission := Admission{Status: reservation.Admission, RetryAt: reservation.RetryAt}
	if err != nil || session == nil {
		return admission, err
	}
	admission.Finish = func(ctx context.Context, event attempt.Event) error {
		// Here the controlled provider contract explicitly defines Completed as
		// a verified terminal report with five measured units, not HTTP success.
		if event.Outcome == attempt.NotExecuted {
			return session.Release(ctx, quota.ReleaseProof[string, string]{
				Handle: reservation.Handle, ID: id.Attempt + "/usage", NotExecuted: true, Reason: "never dispatched",
			})
		}
		if event.Outcome == attempt.Completed {
			return session.Settle(ctx, id.Attempt+"/usage", map[string]uint64{"units": 5}, true)
		}
		return session.Settle(ctx, id.Attempt+"/usage", nil, false)
	}
	return admission, nil
}

func TestBoundaryQuotaHeldThroughStreamAndPending(t *testing.T) {
	t.Parallel()
	// Arrange.
	store := &quotaFixture{states: make(map[string]quota.State)}
	coordinator, identity := setup(t)
	boundary := testBoundary{
		Fresh: func(context.Context, string) error { return nil },
		Admit: store.admit,
		Dispatch: func(_ routery.RouteCall[string], receipt *Receipt) (routery.BasicRouteResult[string], error) {
			event, _, err := receipt.Snapshot()
			if err != nil {
				return routery.AbortResult[routery.BasicKind, routery.BasicReason, string](), err
			}
			event.Phase = attempt.StreamOpened
			result := routery.BasicHandled("stream")
			result.Lifetime = routery.NewLifetime(nil)
			return result, receipt.Record(event)
		},
		CleanupContext: cleanupContext,
	}
	call := routery.NewRouteCall(t.Context(), "request")
	// Act.
	winner, err := boundary.Run(call, coordinator, identity)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = winner.Route.Lifetime.Close() })
	if store.live != 1 || store.states[identity.Attempt] != quota.Reserved {
		t.Fatal("stream handle prematurely finalized reservation")
	}
	// A second admission must be denied while the first returned handle is STILL OPEN.
	activeCoordinator, activeID := setup(t)
	activeStore := &quotaFixture{states: make(map[string]quota.State)}
	activeBoundary := boundary
	activeBoundary.Admit = activeStore.admit
	activeWinner, activeErr := activeBoundary.Run(call, activeCoordinator, activeID)
	if activeErr != nil {
		t.Fatal(activeErr)
	}
	t.Cleanup(func() { _ = activeWinner.Route.Lifetime.Close() })
	activeDenied, activeDeniedErr := activeBoundary.Run(call, activeCoordinator,
		attempt.Identity{Operation: activeID.Operation, Attempt: "while-open"})
	if activeDeniedErr != nil || activeDenied.Started || activeDenied.Admission != quota.Denied ||
		activeStore.live != 1 || activeStore.states[activeID.Attempt] != quota.Reserved {
		t.Fatal("live stream did not retain its concurrency reservation")
	}
	if err := winner.Route.Lifetime.Close(); err != nil {
		t.Fatal(err)
	}
	denied, deniedErr := boundary.Run(
		call,
		coordinator,
		attempt.Identity{Operation: identity.Operation, Attempt: "second"},
	)
	if deniedErr != nil || denied.Started || denied.Admission != quota.Denied || store.live != 1 {
		t.Fatal("unknown stream usage was refunded")
	}
	late := attempt.Event{Identity: identity, Phase: attempt.Terminal, Outcome: attempt.Completed}
	if err := winner.Receipt.Record(late); err != nil {
		t.Fatal(err)
	}
	if err := winner.Receipt.Reconcile(); err != nil {
		t.Fatal(err)
	}
	// Assert.
	if store.live != 0 || store.actual != 5 || store.states[identity.Attempt] != quota.Committed {
		t.Fatalf("lost overage/double settlement: live=%d actual=%d state=%v", store.live, store.actual, store.states)
	}
}
