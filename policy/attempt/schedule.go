package attempt

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrInvalidHint indicates invalid normalized clock or backoff metadata.
var ErrInvalidHint = errors.New("routery/attempt: invalid scheduling hint")

// Hint is a normalized provider not-before with caller-owned clock provenance.
// Uncertainty is conservatively added, never subtracted.
type Hint struct {
	Present     bool
	NotBefore   time.Time
	Source      string
	Clock       string
	Uncertainty time.Duration
}

// InvalidHintPolicy explicitly chooses rejection or ignore-with-reason.
type InvalidHintPolicy uint8

const (
	RejectHint InvalidHintPolicy = iota
	IgnoreHint
)

// ScheduleInput supplies fixed evaluation time, backoff and deadline.
type ScheduleInput struct {
	Now         time.Time
	Backoff     time.Duration
	Hint        Hint
	InvalidHint InvalidHintPolicy
	Cooldown    time.Time
	Deadline    time.Time
}

// Schedule sets a lower bound on start time; a hint never grants replay permission.
func Schedule(decision Decision, input ScheduleInput) (Decision, error) {
	if !validTimestamp(input.Now) || input.Backoff < 0 || input.InvalidHint > IgnoreHint ||
		(!input.Cooldown.IsZero() && !validTimestamp(input.Cooldown)) ||
		(!input.Deadline.IsZero() && !validTimestamp(input.Deadline)) {
		return decision, ErrInvalidHint
	}
	if decision.Action != Retry && decision.Action != Fallback {
		return decision, nil
	}
	start, err := AddDelay(input.Now, input.Backoff)
	if err != nil {
		return decision, err
	}
	if input.Hint.Present {
		hintTime, hintErr := hintStart(input.Hint)
		if hintErr != nil {
			if input.InvalidHint == RejectHint {
				return decision, ErrInvalidHint
			}
			decision.Reason = HintIgnored
		} else if hintTime.After(start) {
			start = hintTime
		}
	}
	if input.Cooldown.After(start) {
		start = input.Cooldown
	}
	decision.NotBefore = start
	decision.Deadline = input.Deadline
	if !input.Deadline.IsZero() && !start.Before(input.Deadline) {
		decision.Action = Defer
		decision.Reason = DeadlineExhausted
	}
	return decision, nil
}

func hintStart(hint Hint) (time.Time, error) {
	if hint.Source == "" || hint.Clock == "" {
		return time.Time{}, ErrInvalidHint
	}
	return AddDelay(hint.NotBefore, hint.Uncertainty)
}

// Wait waits until the declared time and checks cancellation even for zero delay.
func Wait(ctx context.Context, notBefore time.Time, now func() time.Time) error {
	if now == nil || !validTimestamp(notBefore) {
		return ErrInvalidHint
	}
	for {
		if err := waitInterval(ctx, notBefore, now); err != nil {
			return err
		}
		current := now()
		if !validTimestamp(current) {
			return ErrInvalidHint
		}
		if !current.Before(notBefore) {
			return ctx.Err()
		}
	}
}

func waitInterval(ctx context.Context, notBefore time.Time, now func() time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	current := now()
	if !validTimestamp(current) {
		return ErrInvalidHint
	}
	delay := notBefore.Sub(current)
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}

// Cooldowns holds process-local not-before hints per caller-defined scope.
// It is not a distributed admission backend or a circuit breaker.
type Cooldowns[Scope comparable] struct {
	mu    sync.Mutex
	until map[Scope]time.Time
}

// NewCooldowns creates isolated scope scheduling state.
func NewCooldowns[Scope comparable]() *Cooldowns[Scope] {
	return &Cooldowns[Scope]{mu: sync.Mutex{}, until: make(map[Scope]time.Time)}
}

// Extend monotonically extends a single scope's cooldown.
func (cooldowns *Cooldowns[Scope]) Extend(scope Scope, until time.Time) {
	cooldowns.mu.Lock()
	defer cooldowns.mu.Unlock()
	if until.After(cooldowns.until[scope]) {
		cooldowns.until[scope] = until
	}
}

// Until returns a scope's current start bound; unrelated scopes remain unaffected.
func (cooldowns *Cooldowns[Scope]) Until(scope Scope) time.Time {
	cooldowns.mu.Lock()
	defer cooldowns.mu.Unlock()
	return cooldowns.until[scope]
}
