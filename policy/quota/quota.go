package quota

import (
	"context"
	"errors"
	"maps"
	"sync"
	"time"

	"github.com/skosovsky/routery/policy/attempt"
)

// ErrConflict indicates a contradictory settlement or reservation transition.
var ErrConflict = errors.New("routery/quota: conflict")

// ErrIncompatibleUnits indicates usage not declared by the reservation.
var ErrIncompatibleUnits = errors.New("routery/quota: incompatible units")

// ErrBackendUnavailable indicates classified backend unavailability.
var ErrBackendUnavailable = errors.New("routery/quota: backend unavailable")

// ErrUnknownHandle indicates an unknown or expired backend handle.
var ErrUnknownHandle = errors.New("routery/quota: unknown handle")

// ReserveError makes acknowledgement uncertainty explicit. Unclassified errors fail closed.
type ReserveError struct {
	Err        error
	UnknownAck bool
}

// Error implements error.
func (failure *ReserveError) Error() string { return "routery/quota: reserve: " + failure.Err.Error() }

// Unwrap preserves backend error identity.
func (failure *ReserveError) Unwrap() error { return failure.Err }

// Admission is a typed result, separate from backend error.
type Admission uint8

const (
	Denied Admission = iota
	Admitted
	Deferred
	Unreserved
)

// State is the local view of backend reservation settlement.
type State uint8

const (
	Reserved State = iota
	Committed
	Released
	Pending
)

// ReserveRequest carries trusted scope and attempt identity. Caller defines unit meaning.
type ReserveRequest[Scope comparable, Unit comparable] struct {
	Scope       Scope
	Identity    attempt.Identity
	Estimated   map[Unit]uint64
	Fingerprint string
	Deadline    time.Time
}

// Reservation returns an opaque handle and bounded caller-owned reason.
type Reservation[Handle comparable, Reason comparable] struct {
	Admission Admission
	Handle    Handle
	Reason    Reason
	RetryAt   time.Time
}

// Settlement is idempotent by handle and ID; actual values must never be capped to estimates.
type Settlement[Handle comparable, Unit comparable] struct {
	Handle   Handle
	ID       string
	Actual   map[Unit]uint64
	Complete bool
}

// ReleaseProof explicitly declares proven absence of usage.
type ReleaseProof[Handle comparable, Reason comparable] struct {
	Handle      Handle
	ID          string
	NotExecuted bool
	Reason      Reason
}

// Backend owns atomicity and durable idempotency across clients and processes.
// Reserve must deduplicate scope+operation+attempt, rejecting mismatched repeat inputs.
// Commit and Release must not both finalize one handle; conflicting repeats error.
// Lost acknowledgements retain identity. Pending is durable, not an automatic refund.
type Backend[Scope comparable, Unit comparable, Handle comparable, Reason comparable] interface {
	Reserve(context.Context, ReserveRequest[Scope, Unit]) (Reservation[Handle, Reason], error)
	Commit(context.Context, Settlement[Handle, Unit]) error
	Release(context.Context, ReleaseProof[Handle, Reason]) error
	Pending(context.Context, Settlement[Handle, Unit]) error
}

// FailurePolicy is declared before dispatch; fail-open loses the quota guarantee.
type FailurePolicy uint8

const (
	FailClosed FailurePolicy = iota
	FailOpen
)

// Session serializes settlement with context-aware waiters and a short state mutex.
// Backend callbacks may read State but must not reenter Settle/Release synchronously.
// In-flight I/O reports Pending until acknowledgement; durable state belongs to Backend.
type Session[Scope comparable, Unit comparable, Handle comparable, Reason comparable] struct {
	mu          sync.Mutex
	serial      chan struct{}
	backend     Backend[Scope, Unit, Handle, Reason]
	request     ReserveRequest[Scope, Unit]
	reservation Reservation[Handle, Reason]
	state       State
	settlement  *Settlement[Handle, Unit]
	release     *ReleaseProof[Handle, Reason]
}

// Admit calls the host backend before a physical dispatch and checks cancellation.
func Admit[Scope comparable, Unit comparable, Handle comparable, Reason comparable](
	ctx context.Context,
	backend Backend[Scope, Unit, Handle, Reason],
	request ReserveRequest[Scope, Unit],
	failurePolicy FailurePolicy,
) (Reservation[Handle, Reason], *Session[Scope, Unit, Handle, Reason], error) {
	if err := ctx.Err(); err != nil {
		return Reservation[Handle, Reason]{}, nil, err
	}
	if backend == nil || request.Identity.Operation == "" || request.Identity.Attempt == "" ||
		request.Fingerprint == "" ||
		failurePolicy > FailOpen {
		return Reservation[Handle, Reason]{}, nil, ErrConflict
	}
	request.Estimated = maps.Clone(request.Estimated)
	reservation, err := backend.Reserve(ctx, cloneRequest(request))
	if err != nil {
		var classified *ReserveError
		if failurePolicy == FailOpen && errors.As(err, &classified) && !classified.UnknownAck &&
			errors.Is(err, ErrBackendUnavailable) {
			reservation.Admission = Unreserved
			return reservation, nil, nil
		}
		return reservation, nil, err
	}
	if reservation.Admission != Admitted {
		if reservation.Admission != Denied && reservation.Admission != Deferred {
			return reservation, nil, ErrConflict
		}
		return reservation, nil, nil
	}
	session := &Session[Scope, Unit, Handle, Reason]{
		mu: sync.Mutex{}, serial: make(chan struct{}, 1), backend: backend, request: request, reservation: reservation,
		state: Reserved, settlement: nil, release: nil,
	}
	return reservation, session, nil
}

// State returns the current in-process settlement view.
func (session *Session[Scope, Unit, Handle, Reason]) State() State {
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.state
}

// Settle commits complete usage, or retains incomplete usage for reconciliation.
// Retrying a lost acknowledgement must use the same settlement ID and values.
func (session *Session[Scope, Unit, Handle, Reason]) Settle(
	ctx context.Context,
	id string,
	actual map[Unit]uint64,
	complete bool,
) error {
	if err := session.acquire(ctx); err != nil {
		return err
	}
	defer func() { <-session.serial }()
	session.mu.Lock()
	defer session.mu.Unlock()
	if id == "" || session.release != nil {
		return ErrConflict
	}
	for unit := range actual {
		if _, ok := session.request.Estimated[unit]; !ok {
			return ErrIncompatibleUnits
		}
	}
	next := Settlement[Handle, Unit]{
		Handle:   session.reservation.Handle,
		ID:       id,
		Actual:   maps.Clone(actual),
		Complete: complete,
	}
	if session.settlement != nil {
		previous := *session.settlement
		if previous.ID != id || (previous.Complete && (!complete || !maps.Equal(previous.Actual, actual))) {
			return ErrConflict
		}
		if session.state == Committed {
			return nil
		}
	}
	session.settlement = &next
	session.state = Pending
	if !complete {
		return session.callBackend(func() error { return session.backend.Pending(ctx, cloneSettlement(next)) })
	}
	if err := session.callBackend(
		func() error { return session.backend.Commit(ctx, cloneSettlement(next)) },
	); err != nil {
		session.state = Pending
		return err
	}
	session.state = Committed
	return nil
}

// Release finalizes proven no-usage; timeout, cancellation and TTL are not proof.
func (session *Session[Scope, Unit, Handle, Reason]) Release(
	ctx context.Context,
	proof ReleaseProof[Handle, Reason],
) error {
	if err := session.acquire(ctx); err != nil {
		return err
	}
	defer func() { <-session.serial }()
	session.mu.Lock()
	defer session.mu.Unlock()
	if proof.Handle != session.reservation.Handle || proof.ID == "" || !proof.NotExecuted {
		return ErrConflict
	}
	if session.settlement != nil && session.settlement.Complete {
		return ErrConflict
	}
	if session.release != nil && *session.release != proof {
		return ErrConflict
	}
	if session.state == Released {
		return nil
	}
	session.release = &proof
	session.state = Pending
	if err := session.callBackend(func() error { return session.backend.Release(ctx, proof) }); err != nil {
		session.state = Pending
		return err
	}
	session.state = Released
	return nil
}

func cloneRequest[Scope comparable, Unit comparable](request ReserveRequest[Scope, Unit]) ReserveRequest[Scope, Unit] {
	request.Estimated = maps.Clone(request.Estimated)
	return request
}

func cloneSettlement[Handle comparable, Unit comparable](settlement Settlement[Handle, Unit]) Settlement[Handle, Unit] {
	settlement.Actual = maps.Clone(settlement.Actual)
	return settlement
}

// acquire serializes transitions while allowing a waiter to cancel independently.
func (session *Session[Scope, Unit, Handle, Reason]) acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case session.serial <- struct{}{}:
	}
	if err := ctx.Err(); err != nil {
		<-session.serial
		return err
	}
	return nil
}

// callBackend temporarily releases the state lock, including when the callback panics.
func (session *Session[Scope, Unit, Handle, Reason]) callBackend(call func() error) error {
	session.mu.Unlock()
	defer session.mu.Lock()
	return call()
}
