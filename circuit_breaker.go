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

// CircuitBreaker wraps a route handler with a fail-fast circuit breaker.
//
// Only non-nil errors returned from route handlers can open the circuit.
// Business dispositions such as ActionNext never affect breaker counters.
func CircuitBreaker[Req any, Res any](
	failureThreshold int,
	resetTimeout time.Duration,
	isFailure func(error) bool,
) RouteMiddleware[Req, Res] {
	if failureThreshold < 1 {
		return func(RouteHandler[Req, Res]) RouteHandler[Req, Res] {
			return invalidRouteHandler[Req, Res](configError("circuit breaker failure threshold must be at least 1"))
		}
	}
	if resetTimeout < 0 {
		return func(RouteHandler[Req, Res]) RouteHandler[Req, Res] {
			return invalidRouteHandler[Req, Res](configError("circuit breaker reset timeout must be non-negative"))
		}
	}

	//nolint:exhaustruct // zero values are intentional for counters, mutex, and timestamps.
	st := &circuitBreakerState{state: cbClosed}
	return func(next RouteHandler[Req, Res]) RouteHandler[Req, Res] {
		if next == nil {
			return invalidRouteHandler[Req, Res](
				configError("circuit breaker middleware requires non-nil next route handler"),
			)
		}

		return func(ctx context.Context, req Req, rec ResultRecorder[Res]) error {
			if err := st.beforeRequest(resetTimeout); err != nil {
				return err
			}

			err := next(ctx, req, rec)
			st.afterRequest(err, isFailure, failureThreshold)
			return err
		}
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
