package execution_test

import (
	"context"
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/skosovsky/routery"
	"github.com/skosovsky/routery/policy/attempt"
	"github.com/skosovsky/routery/policy/execution"
	"github.com/skosovsky/routery/policy/quota"
)

// singleReservationBackend is a host-owned, single-process example fixture.
// It admits only its configured physical identity. It is not a durable store;
// production hosts provide atomicity, persistence, TTL and acknowledgement recovery.
type singleReservationBackend struct {
	mu         sync.Mutex
	request    quota.ReserveRequest[string, string]
	reserved   bool
	state      quota.State
	settlement *quota.Settlement[string, string]
	release    *quota.ReleaseProof[string, string]
}

func (backend *singleReservationBackend) Reserve(
	ctx context.Context,
	request quota.ReserveRequest[string, string],
) (quota.Reservation[string, string], error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return quota.Reservation[string, string]{}, err
	}
	if request.Identity != backend.request.Identity || request.Scope != backend.request.Scope {
		return quota.Reservation[string, string]{Admission: quota.Denied}, nil
	}
	if request.Fingerprint != backend.request.Fingerprint || request.Deadline != backend.request.Deadline ||
		!maps.Equal(request.Estimated, backend.request.Estimated) {
		return quota.Reservation[string, string]{}, quota.ErrConflict
	}
	if !backend.reserved {
		backend.reserved, backend.state = true, quota.Reserved
	}
	return quota.Reservation[string, string]{Admission: quota.Admitted, Handle: request.Identity.Attempt}, nil
}

func (backend *singleReservationBackend) Commit(ctx context.Context, value quota.Settlement[string, string]) error {
	return backend.apply(ctx, value, true)
}

func (backend *singleReservationBackend) Pending(ctx context.Context, value quota.Settlement[string, string]) error {
	return backend.apply(ctx, value, false)
}

func (backend *singleReservationBackend) apply(
	ctx context.Context,
	value quota.Settlement[string, string],
	complete bool,
) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if !backend.reserved || value.Handle != backend.request.Identity.Attempt {
		return quota.ErrUnknownHandle
	}
	if value.ID == "" || value.Complete != complete || backend.state == quota.Released {
		return quota.ErrConflict
	}
	for unit := range value.Actual {
		if _, exists := backend.request.Estimated[unit]; !exists {
			return quota.ErrIncompatibleUnits
		}
	}
	if previous := backend.settlement; previous != nil {
		if value.ID != previous.ID || (previous.Complete && (!complete || !maps.Equal(value.Actual, previous.Actual))) {
			return quota.ErrConflict
		}
	}
	value.Actual = maps.Clone(value.Actual)
	backend.settlement = &value
	backend.state = quota.Pending
	if complete {
		backend.state = quota.Committed
	}
	return nil
}

func (backend *singleReservationBackend) Release(ctx context.Context, proof quota.ReleaseProof[string, string]) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if !backend.reserved || proof.Handle != backend.request.Identity.Attempt {
		return quota.ErrUnknownHandle
	}
	if !proof.NotExecuted || proof.ID == "" || backend.state == quota.Committed ||
		(backend.release != nil && *backend.release != proof) {
		return quota.ErrConflict
	}
	backend.release, backend.state = &proof, quota.Released
	return nil
}

func ExampleBoundary_quotaReconciliation() {
	// Arrange: host-defined scope/units, one physical identity and measured usage.
	ctx := context.Background()
	id := attempt.Identity{Operation: "read-operation", Attempt: "read-1"}
	request := quota.ReserveRequest[string, string]{Scope: "trusted-account", Identity: id,
		Estimated: map[string]uint64{"units": 2}, Fingerprint: "host-quota-policy"}
	backend := &singleReservationBackend{request: request}
	var session *quota.Session[string, string, string, string]
	var actual uint64
	complete := false
	boundary := execution.Boundary[string, routery.BasicKind, routery.BasicReason, string]{
		Fresh: func(context.Context, string) error { return nil },
		Admit: func(ctx context.Context, _ string, identity attempt.Identity) (execution.Admission, error) {
			request.Identity = identity
			reservation, ownedSession, err := quota.Admit(ctx, backend, request, quota.FailClosed)
			session = ownedSession
			admission := execution.Admission{Status: reservation.Admission, RetryAt: reservation.RetryAt}
			if err != nil || session == nil {
				return admission, err
			}
			admission.Finish = func(ctx context.Context, event attempt.Event) error {
				// Facts are from host/provider evidence, not inferred from cancellation.
				if event.Outcome == attempt.NotExecuted {
					return session.Release(ctx, quota.ReleaseProof[string, string]{Handle: reservation.Handle,
						ID: "settle-read-1", NotExecuted: true, Reason: "proven-before-dispatch"})
				}
				return session.Settle(ctx, "settle-read-1", map[string]uint64{"units": actual}, complete)
			}
			return admission, nil
		},
		CleanupContext: func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), time.Second)
		},
		Dispatch: func(call routery.RouteCall[string], receipt *execution.Receipt) (routery.BasicRouteResult[string], error) {
			event, _, err := receipt.Snapshot()
			if err != nil {
				return routery.BasicRouteResult[string]{}, err
			}
			event.Phase = attempt.StreamOpened // Opening is not terminal completion.
			result := routery.BasicHandled(call.Request)
			result.Lifetime = routery.NewLifetime(nil)
			return result, receipt.Record(event)
		},
	}
	coordinator, err := attempt.NewCoordinator(id.Operation, 1)
	if err != nil {
		fmt.Println(err)
		return
	}
	// Act: close an unknown stream, then reconcile a late verified usage report.
	result, err := boundary.Run(routery.NewRouteCall(ctx, "private stream handle"), coordinator, id)
	if err != nil {
		_ = result.Route.Lifetime.Close()
		fmt.Println(err)
		return
	}
	fmt.Println("admitted", result.Admission == quota.Admitted)
	if err = result.Route.Lifetime.Close(); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("pending", session.State() == quota.Pending)
	actual, complete = 7, true // Explicit host evidence; overage is not capped to 2.
	event := attempt.Event{Identity: id, Phase: attempt.Terminal, Outcome: attempt.Completed}
	if err = result.Receipt.Record(event); err != nil {
		fmt.Println(err)
		return
	}
	// Assert: stable settlement identity and duplicate evidence do not double usage.
	fmt.Println("committed", session.State() == quota.Committed, "usage", backend.settlement.Actual["units"])
	fmt.Println("repeat", result.Receipt.Record(event), "usage", backend.settlement.Actual["units"])
	// Output:
	// admitted true
	// pending true
	// committed true usage 7
	// repeat <nil> usage 7
}
