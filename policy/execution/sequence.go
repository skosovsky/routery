package execution

import (
	"context"
	"errors"
	"time"

	"github.com/skosovsky/routery"
	"github.com/skosovsky/routery/policy/attempt"
	"github.com/skosovsky/routery/policy/quota"
)

// Step declares a request/binding projection and one physical identity.
type Step[Req any] struct {
	Request  Req
	Identity attempt.Identity
}

// Failure keeps caller classification separate from remote facts and local error.
type Failure[Class comparable] struct {
	Class     Class
	Event     attempt.Event
	Remaining int
	Err       error
}

// TraceEntry retains physical results, resources and errors, even after an accepted repeat.
type TraceEntry[Kind comparable, Reason comparable, Payload any] struct {
	Identity attempt.Identity
	Result   Result[Kind, Reason, Payload]
	Err      error
}

// SequenceResult exposes the final result and expected stop/reconcile/defer decision.
// Provider failures are retained in Failure.Err rather than replacing typed decisions.
type SequenceResult[Class comparable, Kind comparable, Reason comparable, Payload any] struct {
	Last                Result[Kind, Reason, Payload]
	Trace               []TraceEntry[Kind, Reason, Payload]
	Failure             *Failure[Class]
	Decision            attempt.Decision
	NestedAttemptsKnown bool
}

// Sequence composes one retry owner with Boundary; no provider policy is hardcoded.
// Next performs explicit reselect/rebuild when needed and must preserve operation identity.
// Wait is optional; nil uses attempt.Wait with Now. Now and context deadlines must share a clock domain; callbacks are caller-owned.
type Sequence[Req any, Class comparable, Kind comparable, Reason comparable, Payload any] struct {
	Boundary Boundary[Req, Kind, Reason, Payload]
	Classify func(Result[Kind, Reason, Payload], error) (Class, error)
	Replay   func(Failure[Class]) (attempt.Replay, error)
	Schedule func(Failure[Class], attempt.Decision) (attempt.ScheduleInput, error)
	Next     func(context.Context, Step[Req], attempt.Decision) (Step[Req], error)
	Now      func() time.Time
	Wait     func(context.Context, time.Time) error
	Deadline time.Time
	// DeadlineContext is optional; nil uses wall-clock context.WithDeadline.
	// Synthetic clocks require a factory that propagates cancellation in their domain.
	DeadlineContext     func(context.Context, time.Time) (context.Context, context.CancelFunc)
	NestedAttemptsKnown bool
}

// Run dispatches the initial step, then only repeats when explicit replay and scheduling permit.
func (sequence Sequence[Req, Class, Kind, Reason, Payload]) Run(
	ctx context.Context, coordinator *attempt.Coordinator, step Step[Req],
) (SequenceResult[Class, Kind, Reason, Payload], error) {
	var result SequenceResult[Class, Kind, Reason, Payload]
	result.NestedAttemptsKnown = sequence.NestedAttemptsKnown
	if sequence.Classify == nil || sequence.Replay == nil || sequence.Schedule == nil ||
		sequence.Next == nil || sequence.Now == nil {
		return result, ErrInvalidBoundary
	}
	sequence.Deadline = earliestDeadline(ctx, sequence.Deadline, time.Time{})
	var previous *Result[Kind, Reason, Payload]
	for {
		if err := sequence.checkStart(ctx, time.Time{}); err != nil {
			result.Decision = stopDecision(result.Decision, err)
			return result, err
		}
		last, runErr := sequence.runStep(ctx, coordinator, step, previous, result.Failure, &result.Decision)
		result.Trace = append(
			result.Trace,
			TraceEntry[Kind, Reason, Payload]{Identity: step.Identity, Result: last, Err: runErr},
		)
		if previous != nil && !last.Started {
			result.Decision = preflightDecision(result.Decision, last, step.Identity, runErr)
			return result, runErr
		}
		result.Last = last
		result.Failure = nil
		if !last.Started || runErr == nil || isControlError(runErr) {
			result.Decision = completedDecision(last, step.Identity, runErr)
			return result, runErr
		}
		failure, replay, decision, err := sequence.decide(ctx, last, runErr)
		result.Failure, result.Decision = &failure, decision
		if err != nil {
			result.Decision = stopDecision(result.Decision, err)
			return result, err
		}
		if !isRepeat(decision.Action) {
			return result, nil
		}
		sequence.Deadline = earliestDeadline(ctx, sequence.Deadline, decision.Deadline)
		step, result.Decision, err = sequence.advance(ctx, step, last, replay, decision)
		result.Failure.Event, result.Failure.Remaining, _ = last.Receipt.Snapshot()
		if err != nil {
			result.Decision = stopDecision(result.Decision, err)
			return result, err
		}
		if !isRepeat(result.Decision.Action) {
			return result, nil
		}
		previous = &last
	}
}

func preflightDecision[Kind comparable, Reason comparable, Payload any](
	decision attempt.Decision, last Result[Kind, Reason, Payload], identity attempt.Identity, err error,
) attempt.Decision {
	if isRepeat(decision.Action) {
		return completedDecision(last, identity, err)
	}
	return decision
}

func completedDecision[Kind comparable, Reason comparable, Payload any](
	last Result[Kind, Reason, Payload], identity attempt.Identity, err error,
) attempt.Decision {
	decision := terminalDecision(last)
	if last.Receipt == nil {
		decision.Event.Identity = identity
	}
	if err != nil {
		decision = stopDecision(decision, err)
	}
	return decision
}

func terminalDecision[Kind comparable, Reason comparable, Payload any](
	last Result[Kind, Reason, Payload],
) attempt.Decision {
	decision := attempt.Decision{
		Action: attempt.Stop,
		Reason: attempt.ExecutionReturned,
		Event: attempt.Event{
			Identity:  attempt.Identity{Operation: "", Attempt: ""},
			Phase:     attempt.BeforeDispatch,
			Committed: false,
			Outcome:   attempt.Unknown,
		},
		NotBefore: time.Time{},
		Deadline:  time.Time{},
	}
	if last.Receipt != nil {
		decision.Event, _, _ = last.Receipt.Snapshot()
	}
	switch {
	case last.BudgetExhausted:
		decision.Reason = attempt.BudgetExhausted
	case last.Admission == quota.Denied:
		decision.Reason = attempt.AdmissionDenied
	case last.Admission == quota.Deferred:
		decision.Action, decision.Reason, decision.NotBefore = attempt.Defer, attempt.AdmissionDeferred, last.RetryAt
	}
	return decision
}

func (sequence Sequence[Req, Class, Kind, Reason, Payload]) decide(
	ctx context.Context, last Result[Kind, Reason, Payload], runErr error,
) (Failure[Class], attempt.Replay, attempt.Decision, error) {
	var failure Failure[Class]
	var replay attempt.Replay
	var decision attempt.Decision
	event, remaining, err := last.Receipt.Snapshot()
	failure.Event, failure.Remaining, failure.Err = event, remaining, runErr
	if err != nil {
		return failure, replay, decision, errors.Join(runErr, err)
	}
	failure.Class, err = sequence.Classify(last, runErr)
	if err != nil {
		return failure, replay, decision, err
	}
	replay, err = sequence.Replay(failure)
	if err != nil {
		return failure, replay, decision, err
	}
	decision, err = attempt.Decide(ctx, event, remaining, replay)
	if err != nil || (decision.Action != attempt.Retry && decision.Action != attempt.Fallback) {
		return failure, replay, decision, err
	}
	input, err := sequence.Schedule(failure, decision)
	if err != nil {
		return failure, replay, decision, err
	}
	input.Now = sequence.Now()
	input.Deadline = earliestDeadline(ctx, input.Deadline, sequence.Deadline)
	decision, err = attempt.Schedule(decision, input)
	return failure, replay, decision, err
}

func (sequence Sequence[Req, Class, Kind, Reason, Payload]) advance(
	ctx context.Context,
	step Step[Req],
	last Result[Kind, Reason, Payload],
	replay attempt.Replay,
	decision attempt.Decision,
) (Step[Req], attempt.Decision, error) {
	bounded, cancel, err := sequence.boundedContext(ctx)
	if err != nil {
		return step, decision, err
	}
	defer cancel()
	ctx = bounded
	if closeErr := last.Route.Lifetime.Close(); closeErr != nil {
		return step, decision, closeErr
	}
	_, _, err = last.Receipt.Snapshot()
	if err != nil {
		return step, decision, err
	}
	if sequence.Wait != nil {
		err = sequence.Wait(ctx, decision.NotBefore)
	} else {
		err = attempt.Wait(ctx, decision.NotBefore, sequence.Now)
	}
	if err != nil {
		return step, decision, err
	}
	if err = sequence.checkStart(ctx, decision.NotBefore); err != nil {
		return step, decision, err
	}
	event, remaining, err := last.Receipt.Snapshot()
	if err != nil {
		return step, decision, err
	}
	freshDecision, err := attempt.Decide(ctx, event, remaining, replay)
	freshDecision.NotBefore = decision.NotBefore
	freshDecision.Deadline = decision.Deadline
	if err != nil || (freshDecision.Action != attempt.Retry && freshDecision.Action != attempt.Fallback) {
		return step, freshDecision, err
	}
	next, err := sequence.Next(ctx, step, freshDecision)
	if err != nil {
		return step, freshDecision, err
	}
	if next.Identity.Operation != step.Identity.Operation {
		return step, freshDecision, attempt.ErrInvalidEvent
	}
	return next, freshDecision, nil
}

func (sequence Sequence[Req, Class, Kind, Reason, Payload]) boundedContext(ctx context.Context) (
	context.Context, context.CancelFunc, error,
) {
	deadline := earliestDeadline(ctx, sequence.Deadline, time.Time{})
	if deadline.IsZero() {
		bounded, cancel := context.WithCancel(ctx)
		return bounded, cancel, nil
	}
	factory := sequence.DeadlineContext
	if factory == nil {
		factory = context.WithDeadline
	}
	bounded, cancel := factory(ctx, deadline)
	if bounded == nil || cancel == nil {
		if cancel != nil {
			cancel()
		}
		return nil, nil, ErrInvalidBoundary
	}
	actual, ok := bounded.Deadline()
	if !ok || actual.After(deadline) {
		cancel()
		return nil, nil, ErrInvalidBoundary
	}
	return bounded, cancel, nil
}

func (sequence Sequence[Req, Class, Kind, Reason, Payload]) runStep(
	ctx context.Context, coordinator *attempt.Coordinator, step Step[Req],
	previous *Result[Kind, Reason, Payload], failure *Failure[Class], decision *attempt.Decision,
) (Result[Kind, Reason, Payload], error) {
	bounded, cancel, err := sequence.boundedContext(ctx)
	if err != nil {
		return Result[Kind, Reason, Payload]{}, err
	}
	transferred := false
	defer func() {
		if !transferred {
			cancel()
		}
	}()
	boundary := sequence.Boundary
	if boundary.Fresh != nil {
		fresh := boundary.Fresh
		boundary.Fresh = func(ctx context.Context, request Req) error {
			if startErr := sequence.checkStart(ctx, time.Time{}); startErr != nil {
				return startErr
			}
			if freshErr := fresh(ctx, request); freshErr != nil {
				return freshErr
			}
			return sequence.checkStart(ctx, time.Time{})
		}
	}
	var authorize func() (bool, error)
	if previous != nil {
		authorize = func() (bool, error) {
			return sequence.authorize(bounded, previous.Receipt, step.Identity, failure, decision)
		}
	}
	result, err := boundary.run(routery.NewRouteCall(bounded, step.Request), coordinator, step.Identity, authorize)
	if result.Route.Lifetime != nil {
		result.Route.Lifetime.OnClose(cancel)
		transferred = true
	}
	return result, err
}

func (sequence Sequence[Req, Class, Kind, Reason, Payload]) authorize(
	ctx context.Context, previous *Receipt, next attempt.Identity, failure *Failure[Class], decision *attempt.Decision,
) (bool, error) {
	wanted := decision.Action
	for {
		event, remaining, err := previous.Snapshot()
		failure.Event, failure.Remaining = event, remaining
		if err != nil {
			return false, err
		}
		// The next identity already holds a budget slot, included in this evaluation.
		evidence := *failure
		evidence.Remaining++
		replay, err := sequence.Replay(evidence)
		if err != nil {
			return false, err
		}
		if startErr := sequence.checkStart(ctx, time.Time{}); startErr != nil {
			return false, startErr
		}
		fresh, err := attempt.Decide(ctx, event, evidence.Remaining, replay)
		if err != nil {
			return false, err
		}
		if (fresh.Action == attempt.Retry || fresh.Action == attempt.Fallback) && fresh.Action != wanted {
			return false, ErrInvalidBoundary
		}
		fresh, err = previous.authorizeRepeat(event, next, replay, ctx.Done(), func() error {
			return sequence.checkStart(ctx, time.Time{})
		})
		if errors.Is(err, attempt.ErrAuthorizationCancelled) {
			return false, ctx.Err()
		}
		if errors.Is(err, attempt.ErrEventChanged) {
			continue
		}
		if err != nil {
			return false, err
		}
		fresh.NotBefore, fresh.Deadline = decision.NotBefore, decision.Deadline
		*decision = fresh
		return fresh.Action == attempt.Retry || fresh.Action == attempt.Fallback, nil
	}
}

func (sequence Sequence[Req, Class, Kind, Reason, Payload]) checkStart(ctx context.Context, notBefore time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := sequence.Now()
	if _, err := attempt.AddDelay(now, 0); err != nil {
		return err
	}
	if !sequence.Deadline.IsZero() {
		if _, err := attempt.AddDelay(sequence.Deadline, 0); err != nil {
			return err
		}
	}
	if !notBefore.IsZero() && now.Before(notBefore) {
		return attempt.ErrInvalidHint
	}
	deadline := earliestDeadline(ctx, sequence.Deadline, time.Time{})
	if !deadline.IsZero() && !now.Before(deadline) {
		return context.DeadlineExceeded
	}
	return nil
}

func earliestDeadline(ctx context.Context, first, second time.Time) time.Time {
	if !second.IsZero() && (first.IsZero() || second.Before(first)) {
		first = second
	}
	if deadline, ok := ctx.Deadline(); ok && (first.IsZero() || deadline.Before(first)) {
		first = deadline
	}
	return first
}

func stopDecision(decision attempt.Decision, err error) attempt.Decision {
	decision.Action, decision.Reason = attempt.Stop, attempt.NotRetryable
	if errors.Is(err, context.Canceled) {
		decision.Reason = attempt.Cancelled
	} else if errors.Is(err, context.DeadlineExceeded) {
		decision.Reason = attempt.DeadlineExhausted
	}
	return decision
}

func isRepeat(action attempt.Action) bool {
	return action == attempt.Retry || action == attempt.Fallback
}
