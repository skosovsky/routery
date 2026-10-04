package execution

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/skosovsky/routery"
	"github.com/skosovsky/routery/policy/attempt"
	"github.com/skosovsky/routery/policy/quota"
)

// ErrInvalidBoundary indicates missing callbacks or an invalid admission result.
var ErrInvalidBoundary = errors.New("routery/execution: invalid boundary")

// Admission is the host's typed admission decision and idempotent settlement callback.
// Finish receives explicit outcome facts, not inferred usage. It must not reenter Receipt.
type Admission struct {
	Status  quota.Admission
	RetryAt time.Time
	Finish  func(context.Context, attempt.Event) error
}

// Result preserves route output, admission and accounting even when dispatch fails.
type Result[Kind comparable, Reason comparable, Payload any] struct {
	Route           routery.RouteResult[Kind, Reason, Payload]
	Admission       quota.Admission
	RetryAt         time.Time
	Receipt         *Receipt
	Started         bool
	BudgetExhausted bool
}

// Boundary implements one physical dispatch; selection and scheduling are host-owned.
// Fresh checks the selected binding against current facts and must not silently reselect.
// CleanupContext returns a separately bounded context not cancelled with the attempt.
type Boundary[Req any, Kind comparable, Reason comparable, Payload any] struct {
	Fresh          func(context.Context, Req) error
	Admit          func(context.Context, Req, attempt.Identity) (Admission, error)
	Dispatch       func(routery.RouteCall[Req], *Receipt) (routery.RouteResult[Kind, Reason, Payload], error)
	CleanupContext func() (context.Context, context.CancelFunc)
}

// Receipt holds explicit physical attempt facts and observable settlement errors.
// It may outlive Run to accept late definitive events and host reconciliation.
type Receipt struct {
	mu             sync.Mutex
	coordinator    *attempt.Coordinator
	identity       attempt.Identity
	finish         func(context.Context, attempt.Event) error
	cleanupContext func() (context.Context, context.CancelFunc)
	closed         bool
	err            error
}

// Record validates monotonic facts. After closing, changed facts trigger reconciliation.
func (receipt *Receipt) Record(event attempt.Event) error {
	receipt.mu.Lock()
	defer receipt.mu.Unlock()
	if event.Identity != receipt.identity {
		return attempt.ErrInvalidEvent
	}
	previous, _, err := receipt.coordinator.Snapshot(receipt.identity)
	if err != nil {
		return err
	}
	if err = receipt.coordinator.Update(event); err != nil {
		return err
	}
	if receipt.closed && previous != event {
		return receipt.settle(event)
	}
	return nil
}

// Snapshot returns current facts, remaining budget and the latest settlement error.
func (receipt *Receipt) Snapshot() (attempt.Event, int, error) {
	receipt.mu.Lock()
	defer receipt.mu.Unlock()
	event, remaining, err := receipt.coordinator.Snapshot(receipt.identity)
	return event, remaining, errors.Join(err, receipt.err)
}

// Reconcile repeats the same host settlement, including after a lost acknowledgement.
// Host usage evidence may improve while the attempt event stays terminal unknown.
func (receipt *Receipt) Reconcile() error {
	receipt.mu.Lock()
	defer receipt.mu.Unlock()
	if !receipt.closed {
		return attempt.ErrInvalidEvent
	}
	event, _, err := receipt.coordinator.Snapshot(receipt.identity)
	if err != nil {
		return err
	}
	return receipt.settle(event)
}

func (receipt *Receipt) settle(event attempt.Event) error {
	if receipt.finish == nil {
		return nil
	}
	ctx, cancel := receipt.cleanupContext()
	if ctx == nil || cancel == nil {
		if cancel != nil {
			cancel()
		}
		receipt.err = ErrInvalidBoundary
		return receipt.err
	}
	defer cancel()
	receipt.err = receipt.finish(ctx, event)
	return receipt.err
}

func (receipt *Receipt) close(notExecuted bool) error {
	receipt.mu.Lock()
	defer receipt.mu.Unlock()
	if receipt.closed {
		return receipt.err
	}
	event, _, err := receipt.coordinator.Snapshot(receipt.identity)
	if err != nil {
		return err
	}
	event.Phase = attempt.Terminal
	if notExecuted {
		event.Outcome = attempt.NotExecuted
	}
	if err = receipt.coordinator.Update(event); err != nil {
		return err
	}
	receipt.closed = true
	return receipt.settle(event)
}

// Run consumes one unique physical identity and never starts another attempt.
// Denied/deferred admission is returned without an error; Started distinguishes dispatch.
func (boundary Boundary[Req, Kind, Reason, Payload]) Run(
	call routery.RouteCall[Req], coordinator *attempt.Coordinator, identity attempt.Identity,
) (Result[Kind, Reason, Payload], error) {
	result := Result[Kind, Reason, Payload]{
		Route: routery.AbortResult[Kind, Reason, Payload](), Admission: quota.Unreserved,
		RetryAt: time.Time{}, Receipt: nil, Started: false, BudgetExhausted: false,
	}
	if boundary.Fresh == nil || boundary.Dispatch == nil || coordinator == nil ||
		(boundary.Admit != nil && boundary.CleanupContext == nil) {
		return result, ErrInvalidBoundary
	}
	if err := call.Context.Err(); err != nil {
		return result, err
	}
	start, err := coordinator.Begin(identity)
	result.BudgetExhausted = err == nil && !start.Started
	if err != nil || !start.Started {
		return result, err
	}
	receipt := &Receipt{
		mu: sync.Mutex{}, coordinator: coordinator, identity: identity, finish: nil,
		cleanupContext: boundary.CleanupContext, closed: false, err: nil,
	}
	result.Receipt = receipt
	admission, err := boundary.prepare(call, identity)
	result.Admission, result.RetryAt = admission.Status, admission.RetryAt
	if err != nil {
		return result, errors.Join(err, receipt.close(true))
	}
	receipt.finish = admission.Finish
	if admission.Status == quota.Denied || admission.Status == quota.Deferred {
		return result, receipt.close(true)
	}
	return boundary.invoke(call, result)
}

func (boundary Boundary[Req, Kind, Reason, Payload]) prepare(
	call routery.RouteCall[Req], identity attempt.Identity,
) (Admission, error) {
	admission := Admission{Status: quota.Unreserved, RetryAt: time.Time{}, Finish: nil}
	if err := boundary.Fresh(call.Context, call.Request); err != nil {
		return admission, err
	}
	if err := call.Context.Err(); err != nil {
		return admission, err
	}
	if boundary.Admit == nil {
		return admission, nil
	}
	admission, err := boundary.Admit(call.Context, call.Request, identity)
	if err != nil {
		return admission, err
	}
	if admission.Status > quota.Unreserved || (admission.Status == quota.Admitted && admission.Finish == nil) {
		return admission, ErrInvalidBoundary
	}
	return admission, nil
}

func (boundary Boundary[Req, Kind, Reason, Payload]) invoke(
	call routery.RouteCall[Req], result Result[Kind, Reason, Payload],
) (Result[Kind, Reason, Payload], error) {
	if err := boundary.Fresh(call.Context, call.Request); err != nil {
		return result, errors.Join(err, result.Receipt.close(true))
	}
	if err := call.Context.Err(); err != nil {
		return result, errors.Join(err, result.Receipt.close(true))
	}
	event, _, err := result.Receipt.Snapshot()
	if err != nil {
		return result, err
	}
	event.Phase = attempt.Dispatched
	if err = result.Receipt.Record(event); err != nil {
		return result, errors.Join(err, result.Receipt.close(true))
	}
	result.Started = true
	returned := false
	defer func() {
		if !returned {
			_ = result.Receipt.close(false)
		}
	}()
	result.Route, err = boundary.Dispatch(call, result.Receipt)
	result.Route, err = routery.ValidateRouteResult(result.Route, err)
	returned = true
	if result.Route.Lifetime == nil {
		return result, errors.Join(err, result.Receipt.close(false))
	}
	result.Route.Lifetime.OnClose(func() { _ = result.Receipt.close(false) })
	return result, err
}

func isControlError(err error) bool {
	return errors.Is(err, attempt.ErrInvalidEvent) || errors.Is(err, ErrInvalidBoundary) ||
		errors.Is(err, routery.ErrInvalidConfig)
}
