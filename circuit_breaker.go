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
	generation    uint64
}

// CircuitBreaker wraps a route handler with a fail-fast circuit breaker.
//
// Only non-nil errors returned from route handlers can open the circuit.
// Business routing results such as ActionNext never affect breaker counters.
func CircuitBreaker[Req any, Kind comparable, Reason comparable, Payload any](
	failureThreshold int,
	resetTimeout time.Duration,
	isFailure func(error) bool,
) RouteMiddleware[Req, Kind, Reason, Payload] {
	if failureThreshold < 1 {
		return func(RouteHandler[Req, Kind, Reason, Payload]) RouteHandler[Req, Kind, Reason, Payload] {
			return invalidRouteHandler[Req, Kind, Reason, Payload](
				configError("circuit breaker failure threshold must be at least 1"),
			)
		}
	}
	if resetTimeout < 0 {
		return func(RouteHandler[Req, Kind, Reason, Payload]) RouteHandler[Req, Kind, Reason, Payload] {
			return invalidRouteHandler[Req, Kind, Reason, Payload](
				configError("circuit breaker reset timeout must be non-negative"),
			)
		}
	}

	//nolint:exhaustruct_v5 // zero values are intentional for counters, mutex, and timestamps.
	st := &circuitBreakerState{state: cbClosed}
	return func(next RouteHandler[Req, Kind, Reason, Payload]) RouteHandler[Req, Kind, Reason, Payload] {
		if next == nil {
			return invalidRouteHandler[Req, Kind, Reason, Payload](
				configError("circuit breaker middleware requires non-nil next route handler"),
			)
		}

		return func(call RouteCall[Req]) (RouteResult[Kind, Reason, Payload], error) {
			admission, admissionErr := st.beforeRequest(resetTimeout)
			if admissionErr != nil {
				return AbortResult[Kind, Reason, Payload](), admissionErr
			}

			completed := false
			defer func() {
				if !completed {
					st.abandon(admission)
				}
			}()
			result, err := next(call)
			failed := circuitFailure(err, isFailure)
			st.afterRequest(admission, err, failed, failureThreshold)
			completed = true
			return result, err
		}
	}
}

type circuitAdmission struct {
	generation uint64
	probe      bool
}

func (st *circuitBreakerState) beforeRequest(resetTimeout time.Duration) (circuitAdmission, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.state == cbOpen {
		if time.Since(st.openedAt) < resetTimeout {
			return circuitAdmission{}, ErrCircuitOpen
		}
		st.state = cbHalfOpen
		st.generation++
	}
	if st.state == cbHalfOpen {
		if st.probeInFlight {
			return circuitAdmission{}, ErrCircuitOpen
		}
		st.probeInFlight = true
		st.generation++
		return circuitAdmission{generation: st.generation, probe: true}, nil
	}
	return circuitAdmission{generation: st.generation, probe: false}, nil
}

func (st *circuitBreakerState) afterRequest(admission circuitAdmission, err error, failed bool, threshold int) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if admission.generation != st.generation {
		return
	}
	if admission.probe {
		if st.state != cbHalfOpen || !st.probeInFlight {
			return
		}
		st.probeInFlight = false
		if err == nil {
			st.state = cbClosed
			st.failures = 0
			st.generation++
		} else if failed {
			st.open()
		}
		return
	}
	if st.state != cbClosed {
		return
	}
	if !failed {
		st.failures = 0
		return
	}
	st.failures++
	if st.failures >= threshold {
		st.open()
	}
}

func (st *circuitBreakerState) open() {
	st.state = cbOpen
	st.openedAt = time.Now()
	st.failures = 0
	st.probeInFlight = false
	st.generation++
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

func (st *circuitBreakerState) abandon(admission circuitAdmission) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if admission.probe && admission.generation == st.generation && st.state == cbHalfOpen {
		st.probeInFlight = false
	}
}
