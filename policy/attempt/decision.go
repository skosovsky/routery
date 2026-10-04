package attempt

import (
	"context"
	"time"
)

// Action is an attempt policy decision, separate from route control flow.
type Action uint8

const (
	Stop Action = iota
	Retry
	Fallback
	Reconcile
	Defer
)

// Reason is a bounded provider-neutral explanation.
type Reason uint8

const (
	NotRetryable Reason = iota
	VisibleOutput
	UnsafeReplay
	UnknownOutcome
	BudgetExhausted
	DeadlineExhausted
	RetryAllowed
	HintIgnored
	ExecutionReturned
	AdmissionDenied
	AdmissionDeferred
	Cancelled
)

// Replay declares caller-owned safety evidence; it does not approve side effects.
type Replay struct {
	Retryable     bool
	Replayable    bool
	SafeDuplicate bool
	ResetProtocol bool
	CanReconcile  bool
	UseFallback   bool
}

// Decision includes the original attempt facts even on stop or cancellation.
type Decision struct {
	Action    Action
	Reason    Reason
	Event     Event
	NotBefore time.Time
	Deadline  time.Time
}

// Decide evaluates replay safety before scheduling another physical attempt.
// ResetProtocol means the host has already arranged explicit consumer reset.
func Decide(ctx context.Context, event Event, remaining int, replay Replay) (Decision, error) {
	decision := Decision{
		Action:    Stop,
		Reason:    NotRetryable,
		Event:     event,
		NotBefore: time.Time{},
		Deadline:  time.Time{},
	}
	if err := ctx.Err(); err != nil {
		return decision, err
	}
	if !validEvent(event) || event.Identity.Operation == "" || event.Identity.Attempt == "" {
		return decision, ErrInvalidEvent
	}
	if event.Committed && !replay.ResetProtocol {
		decision.Reason = VisibleOutput
		return decision, nil
	}
	if event.Outcome == Unknown && !replay.SafeDuplicate {
		decision.Reason = UnknownOutcome
		if replay.CanReconcile {
			decision.Action = Reconcile
		}
		return decision, nil
	}
	if !replay.Retryable {
		return decision, nil
	}
	if !replay.Replayable || (event.Outcome == Completed && !replay.SafeDuplicate) {
		decision.Reason = UnsafeReplay
		return decision, nil
	}
	if remaining < 1 {
		decision.Reason = BudgetExhausted
		return decision, nil
	}
	decision.Action = Retry
	if replay.UseFallback {
		decision.Action = Fallback
	}
	decision.Reason = RetryAllowed
	return decision, nil
}
