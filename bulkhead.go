package routery

import "context"

// Bulkhead limits concurrent executions of the wrapped executor using a semaphore.
//
// If the semaphore is full, Execute returns [ErrTooManyRequests] without blocking.
// If ctx is canceled while competing for the semaphore, ctx.Err() is returned.
func Bulkhead[Req any, Res any](limit int) Middleware[Req, Res] {
	if limit <= 0 {
		return func(Executor[Req, Res]) Executor[Req, Res] {
			return invalidExecutor[Req, Res](configError("bulkhead limit must be positive"))
		}
	}

	sema := make(chan struct{}, limit)
	return func(next Executor[Req, Res]) Executor[Req, Res] {
		if next == nil {
			return invalidExecutor[Req, Res](configError("bulkhead middleware requires non-nil next executor"))
		}

		return ExecutorFunc[Req, Res](func(ctx context.Context, req Req) (Res, error) {
			select {
			case sema <- struct{}{}:
				defer func() { <-sema }()
				return next.Execute(ctx, req)
			case <-ctx.Done():
				return zeroValue[Res](), ctx.Err()
			default:
				return zeroValue[Res](), ErrTooManyRequests
			}
		})
	}
}
