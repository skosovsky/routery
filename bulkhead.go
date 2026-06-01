package routery

import "context"

// Bulkhead limits concurrent executions of the wrapped handler using a semaphore.
//
// If the semaphore is full, Handle returns [ErrTooManyRequests] without blocking.
// If ctx is canceled while competing for the semaphore, ctx.Err() is returned.
//
// Business route statuses do not affect bulkhead capacity accounting.
func Bulkhead[TReq any, TRes any](limit int) HandlerMiddleware[TReq, TRes] {
	if limit <= 0 {
		return func(Handler[TReq, TRes]) Handler[TReq, TRes] {
			return invalidHandler[TReq, TRes](configError("bulkhead limit must be positive"))
		}
	}

	sema := make(chan struct{}, limit)
	return func(next Handler[TReq, TRes]) Handler[TReq, TRes] {
		if next == nil {
			return invalidHandler[TReq, TRes](configError("bulkhead middleware requires non-nil next handler"))
		}

		return HandlerFunc[TReq, TRes](func(ctx context.Context, req TReq) (RouteResult[TRes], error) {
			select {
			case sema <- struct{}{}:
				defer func() { <-sema }()
				return next.Handle(ctx, req)
			case <-ctx.Done():
				return zeroRouteResult[TRes](), ctx.Err()
			default:
				return zeroRouteResult[TRes](), ErrTooManyRequests
			}
		})
	}
}
