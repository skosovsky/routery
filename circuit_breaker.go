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

// CircuitBreaker wraps a handler with a fail-fast circuit breaker.
//
// States: Closed (requests pass), Open (requests fail with [ErrCircuitOpen]),
// HalfOpen (one probe request is allowed after resetTimeout).
//
// In HalfOpen, only a successful probe (nil error) closes the circuit.
// Client-side cancellation ([context.Canceled], [context.DeadlineExceeded]) does
// not prove the downstream recovered: the breaker stays HalfOpen so another
// probe may run.
//
// Business route statuses never affect breaker counters. Only non-nil errors
// returned from Handle can open the circuit.
//
// If isFailure is nil, any non-nil error counts as a failure for counting in
// Closed except [context.Canceled] and [context.DeadlineExceeded].
func CircuitBreaker[TReq any, TRes any](
	failureThreshold int,
	resetTimeout time.Duration,
	isFailure func(error) bool,
) HandlerMiddleware[TReq, TRes] {
	if failureThreshold < 1 {
		return func(Handler[TReq, TRes]) Handler[TReq, TRes] {
			return invalidHandler[TReq, TRes](configError("circuit breaker failure threshold must be at least 1"))
		}
	}
	if resetTimeout < 0 {
		return func(Handler[TReq, TRes]) Handler[TReq, TRes] {
			return invalidHandler[TReq, TRes](configError("circuit breaker reset timeout must be non-negative"))
		}
	}

	//nolint:exhaustruct // zero values are intentional for counters, mutex, and timestamps.
	st := &circuitBreakerState{state: cbClosed}
	return func(next Handler[TReq, TRes]) Handler[TReq, TRes] {
		if next == nil {
			return invalidHandler[TReq, TRes](configError("circuit breaker middleware requires non-nil next handler"))
		}

		return HandlerFunc[TReq, TRes](func(ctx context.Context, req TReq) (RouteResult[TRes], error) {
			if err := st.beforeRequest(resetTimeout); err != nil {
				return zeroRouteResult[TRes](), err
			}

			result, err := next.Handle(ctx, req)
			st.afterRequest(err, isFailure, failureThreshold)
			return result, err
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
