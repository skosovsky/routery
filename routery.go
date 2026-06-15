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

// ErrNoSuccessfulOutcome indicates that no handler recorded a payload outcome.
var ErrNoSuccessfulOutcome = errors.New("routery: no successful outcome")

func configError(detail string) error {
	return fmt.Errorf("%w: %s", ErrInvalidConfig, detail)
}
