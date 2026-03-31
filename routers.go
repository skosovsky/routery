package routery

import (
	"context"
	"sync/atomic"
	"time"
)

// Fallback executes primary first and uses secondary when primary fails.
func Fallback[Req any, Res any](primary Executor[Req, Res], secondary Executor[Req, Res]) Executor[Req, Res] {
	if primary == nil || secondary == nil {
		return invalidExecutor[Req, Res](configError("fallback requires non-nil primary and secondary executors"))
	}

	return ExecutorFunc[Req, Res](func(ctx context.Context, req Req) (Res, error) {
		response, err := primary.Execute(ctx, req)
		if err == nil {
			return response, nil
		}

		return secondary.Execute(ctx, req)
	})
}

// RetryIf retries execution when predicate returns true for an error.
func RetryIf[Req any, Res any](attempts int, backoff time.Duration, predicate func(error) bool) Middleware[Req, Res] {
	normalizedAttempts := max(attempts, 1)
	normalizedBackoff := max(backoff, time.Duration(0))
	if predicate == nil {
		return func(Executor[Req, Res]) Executor[Req, Res] {
			return invalidExecutor[Req, Res](configError("retry predicate is nil"))
		}
	}

	return func(next Executor[Req, Res]) Executor[Req, Res] {
		if next == nil {
			return invalidExecutor[Req, Res](configError("retry middleware requires non-nil next executor"))
		}

		return ExecutorFunc[Req, Res](func(ctx context.Context, req Req) (Res, error) {
			return executeWithRetry(ctx, req, next, normalizedAttempts, normalizedBackoff, predicate)
		})
	}
}

// RoundRobin distributes requests among executors sequentially.
func RoundRobin[Req any, Res any](executors ...Executor[Req, Res]) Executor[Req, Res] {
	validated, err := validateExecutors(executors, "round robin")
	if err != nil {
		return invalidExecutor[Req, Res](err)
	}

	return &roundRobinExecutor[Req, Res]{
		executors: validated,
		next:      atomic.Uint64{},
	}
}

// Timeout limits execution time for the wrapped executor.
func Timeout[Req any, Res any](timeout time.Duration) Middleware[Req, Res] {
	if timeout <= 0 {
		return func(next Executor[Req, Res]) Executor[Req, Res] {
			if next == nil {
				return invalidExecutor[Req, Res](configError("timeout middleware requires non-nil next executor"))
			}
			return next
		}
	}

	return func(next Executor[Req, Res]) Executor[Req, Res] {
		if next == nil {
			return invalidExecutor[Req, Res](configError("timeout middleware requires non-nil next executor"))
		}

		return ExecutorFunc[Req, Res](func(ctx context.Context, req Req) (Res, error) {
			timedCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			return next.Execute(timedCtx, req)
		})
	}
}

type roundRobinExecutor[Req any, Res any] struct {
	executors []Executor[Req, Res]
	next      atomic.Uint64
}

func (executor *roundRobinExecutor[Req, Res]) Execute(ctx context.Context, req Req) (Res, error) {
	executorCount := uint64(len(executor.executors))
	index := (executor.next.Add(1) - 1) % executorCount
	return executor.executors[index].Execute(ctx, req)
}

func executeWithRetry[Req any, Res any](
	ctx context.Context,
	req Req,
	next Executor[Req, Res],
	attempts int,
	backoff time.Duration,
	predicate func(error) bool,
) (Res, error) {
	wait := backoff
	var (
		lastErr error
		result  Res
	)

	for attemptIndex := range attempts {
		result, lastErr = next.Execute(ctx, req)
		if lastErr == nil {
			return result, nil
		}

		isFinalAttempt := attemptIndex == attempts-1
		if isFinalAttempt || !predicate(lastErr) {
			return result, lastErr
		}

		if wait > 0 {
			if err := sleepWithContext(ctx, wait); err != nil {
				return zeroValue[Res](), err
			}
			wait = growBackoff(wait)
		}
	}

	return result, lastErr
}

func growBackoff(current time.Duration) time.Duration {
	doubled := current * 2
	if doubled < current {
		return current
	}
	return doubled
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}

	timer := time.NewTimer(delay)
	defer func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
