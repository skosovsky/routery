package routery

import (
	"context"
	"errors"
	"fmt"
)

// Executor executes a request and returns a response or an error.
type Executor[Req any, Res any] interface {
	Execute(ctx context.Context, req Req) (Res, error)
}

// ExecutorFunc adapts a function to the Executor interface.
type ExecutorFunc[Req any, Res any] func(ctx context.Context, req Req) (Res, error)

// Execute runs f for the provided context and request.
func (f ExecutorFunc[Req, Res]) Execute(ctx context.Context, req Req) (Res, error) {
	return f(ctx, req)
}

// Middleware decorates an executor.
type Middleware[Req any, Res any] func(Executor[Req, Res]) Executor[Req, Res]

// RetryPredicate decides whether a failed execution should be retried.
type RetryPredicate[Req any] func(ctx context.Context, req Req, err error) bool

// ErrNoExecutors indicates that no executors were provided.
var ErrNoExecutors = errors.New("routery: no executors provided")

// ErrInvalidConfig indicates invalid router or middleware configuration.
var ErrInvalidConfig = errors.New("routery: invalid configuration")

// ErrCircuitOpen indicates that a circuit breaker is open and requests are failing fast.
var ErrCircuitOpen = errors.New("routery: circuit breaker open")

// ErrTooManyRequests indicates that a bulkhead semaphore has no capacity left.
var ErrTooManyRequests = errors.New("routery: too many concurrent requests")

func zeroValue[T any]() T {
	var zero T
	return zero
}

func configError(detail string) error {
	return fmt.Errorf("%w: %s", ErrInvalidConfig, detail)
}

func invalidExecutor[Req any, Res any](err error) Executor[Req, Res] {
	return ExecutorFunc[Req, Res](func(context.Context, Req) (Res, error) {
		return zeroValue[Res](), err
	})
}

func validateExecutors[Req any, Res any](executors []Executor[Req, Res], name string) ([]Executor[Req, Res], error) {
	if len(executors) == 0 {
		return nil, ErrNoExecutors
	}

	validated := make([]Executor[Req, Res], len(executors))
	copy(validated, executors)

	for index, executor := range validated {
		if executor == nil {
			return nil, configError(fmt.Sprintf("%s executor at index %d is nil", name, index))
		}
	}

	return validated, nil
}
