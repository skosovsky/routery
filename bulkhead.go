package routery

import "context"

// Bulkhead limits concurrent executions of the wrapped route handler using a semaphore.
//
// If the semaphore is full, the handler returns [ErrTooManyRequests] without blocking.
func Bulkhead[Req any, Res any](limit int) RouteMiddleware[Req, Res] {
	if limit <= 0 {
		return func(RouteHandler[Req, Res]) RouteHandler[Req, Res] {
			return invalidRouteHandler[Req, Res](configError("bulkhead limit must be positive"))
		}
	}

	sema := make(chan struct{}, limit)
	return func(next RouteHandler[Req, Res]) RouteHandler[Req, Res] {
		if next == nil {
			return invalidRouteHandler[Req, Res](configError("bulkhead middleware requires non-nil next route handler"))
		}

		return func(ctx context.Context, req Req, rec ResultRecorder[Res]) error {
			select {
			case sema <- struct{}{}:
				defer func() { <-sema }()
				return next(ctx, req, rec)
			case <-ctx.Done():
				return ctx.Err()
			default:
				return ErrTooManyRequests
			}
		}
	}
}
