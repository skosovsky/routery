package routery

import (
	"context"
	"errors"
	"sync"
	"time"
)

const (
	cbClosed = iota
	cbOpen
	cbHalfOpen
)

type circuitBreakerState struct {
	mu            sync.Mutex
	failures      int
	state         int
	openedAt      time.Time
	probeInFlight bool
}

// CircuitBreaker wraps an executor with a fail-fast circuit breaker.
//
// States: Closed (requests pass), Open (requests fail with [ErrCircuitOpen]),
// HalfOpen (one probe request is allowed after resetTimeout).
//
// In HalfOpen, only a successful probe (nil error) closes the circuit.
// Client-side cancellation ([context.Canceled], [context.DeadlineExceeded]) does
// not prove the downstream recovered: the breaker stays HalfOpen so another
// probe may run.
//
// If isFailure is nil, any non-nil error counts as a failure for counting in
// Closed except [context.Canceled] and [context.DeadlineExceeded].
func CircuitBreaker[Req any, Res any](
	failureThreshold int,
	resetTimeout time.Duration,
	isFailure func(error) bool,
) Middleware[Req, Res] {
	if failureThreshold < 1 {
		return func(Executor[Req, Res]) Executor[Req, Res] {
			return invalidExecutor[Req, Res](configError("circuit breaker failure threshold must be at least 1"))
		}
	}
	if resetTimeout < 0 {
		return func(Executor[Req, Res]) Executor[Req, Res] {
			return invalidExecutor[Req, Res](configError("circuit breaker reset timeout must be non-negative"))
		}
	}

	//nolint:exhaustruct // zero values are intentional for counters, mutex, and timestamps.
	st := &circuitBreakerState{state: cbClosed}
	return func(next Executor[Req, Res]) Executor[Req, Res] {
		if next == nil {
			return invalidExecutor[Req, Res](configError("circuit breaker middleware requires non-nil next executor"))
		}

		return ExecutorFunc[Req, Res](func(ctx context.Context, req Req) (Res, error) {
			if err := st.beforeRequest(resetTimeout); err != nil {
				return zeroValue[Res](), err
			}

			res, err := next.Execute(ctx, req)
			st.afterRequest(err, isFailure, failureThreshold)
			return res, err
		})
	}
}

func (st *circuitBreakerState) beforeRequest(resetTimeout time.Duration) error {
	st.mu.Lock()
	defer st.mu.Unlock()

	switch st.state {
	case cbClosed:
		return nil
	case cbOpen:
		if time.Since(st.openedAt) < resetTimeout {
			return ErrCircuitOpen
		}
		st.state = cbHalfOpen
		fallthrough
	case cbHalfOpen:
		if st.probeInFlight {
			return ErrCircuitOpen
		}
		st.probeInFlight = true
		return nil
	}
	return nil
}

func (st *circuitBreakerState) afterRequest(err error, isFailure func(error) bool, failureThreshold int) {
	st.mu.Lock()
	defer st.mu.Unlock()

	failed := circuitFailure(err, isFailure)

	switch st.state {
	case cbClosed:
		if failed {
			st.failures++
			if st.failures >= failureThreshold {
				st.state = cbOpen
				st.openedAt = time.Now()
				st.failures = 0
			}
			return
		}
		st.failures = 0
	case cbHalfOpen:
		st.probeInFlight = false
		if err == nil {
			st.state = cbClosed
			st.failures = 0
			return
		}
		if circuitFailure(err, isFailure) {
			st.state = cbOpen
			st.openedAt = time.Now()
			st.failures = 0
		}
		// Non-fatal errors (e.g. client cancel/deadline): stay HalfOpen for a later probe.
	}
}

func circuitFailure(err error, isFailure func(error) bool) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if isFailure != nil {
		return isFailure(err)
	}
	return true
}
