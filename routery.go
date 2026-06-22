package routery

import (
	"context"
	"errors"
	"fmt"
)

// RetryPredicate decides whether a failed execution should be retried.
//
// Retry middleware invokes the predicate only when a route handler returns a non-nil error.
// Business dispositions such as ActionNext never trigger retries.
type RetryPredicate[TReq any] func(ctx context.Context, req TReq, err error) bool

// ErrNoHandlers indicates that no route handlers were provided.
var ErrNoHandlers = errors.New("routery: no handlers provided")

// ErrInvalidConfig indicates invalid router or middleware configuration.
var ErrInvalidConfig = errors.New("routery: invalid configuration")

// ErrCircuitOpen indicates that a circuit breaker is open and requests are failing fast.
var ErrCircuitOpen = errors.New("routery: circuit breaker open")

// ErrTooManyRequests indicates that a bulkhead semaphore has no capacity left.
var ErrTooManyRequests = errors.New("routery: too many concurrent requests")

// ErrNoSuccessfulOutcome indicates that no handler returned a payload outcome.
var ErrNoSuccessfulOutcome = errors.New("routery: no successful outcome")

// ErrDuplicateRoute indicates that a mutable registry already contains the route id.
var ErrDuplicateRoute = errors.New("routery: duplicate route")

// ErrRouteNotFound indicates that a mutable registry does not contain the route id.
var ErrRouteNotFound = errors.New("routery: route not found")

// ErrInvalidRouteID indicates that a route id cannot be normalized into a non-empty id.
var ErrInvalidRouteID = errors.New("routery: invalid route id")

// ErrConflictingPrefixPolicy indicates incompatible prefix ordering policies in one registry snapshot.
var ErrConflictingPrefixPolicy = errors.New("routery: conflicting prefix policy")

// ErrStaleSnapshot indicates that a caller-provided freshness policy rejected a snapshot.
var ErrStaleSnapshot = errors.New("routery: stale snapshot")

func configError(detail string) error {
	return fmt.Errorf("%w: %s", ErrInvalidConfig, detail)
}
