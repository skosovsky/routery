package routery

// Bulkhead limits concurrent executions of the wrapped route handler using a semaphore.
//
// If the semaphore is full, the handler returns [ErrTooManyRequests] without blocking.
// An owned result retains its permit until Lifetime.Close, including partial error results.
func Bulkhead[Req any, Kind comparable, Reason comparable, Payload any](
	limit int,
) RouteMiddleware[Req, Kind, Reason, Payload] {
	if limit <= 0 {
		return func(RouteHandler[Req, Kind, Reason, Payload]) RouteHandler[Req, Kind, Reason, Payload] {
			return invalidRouteHandler[Req, Kind, Reason, Payload](configError("bulkhead limit must be positive"))
		}
	}

	sema := make(chan struct{}, limit)
	return func(next RouteHandler[Req, Kind, Reason, Payload]) RouteHandler[Req, Kind, Reason, Payload] {
		if next == nil {
			return invalidRouteHandler[Req, Kind, Reason, Payload](
				configError("bulkhead middleware requires non-nil next route handler"),
			)
		}

		return func(call RouteCall[Req]) (RouteResult[Kind, Reason, Payload], error) {
			if err := call.Context.Err(); err != nil {
				return AbortResult[Kind, Reason, Payload](), err
			}
			select {
			case sema <- struct{}{}:
				return invokeWithPermit(call, next, sema)
			case <-call.Context.Done():
				return AbortResult[Kind, Reason, Payload](), call.Context.Err()
			default:
				return AbortResult[Kind, Reason, Payload](), ErrTooManyRequests
			}
		}
	}
}

func invokeWithPermit[Req any, Kind comparable, Reason comparable, Payload any](
	call RouteCall[Req], next RouteHandler[Req, Kind, Reason, Payload], sema chan struct{},
) (RouteResult[Kind, Reason, Payload], error) {
	transferred := false
	defer func() {
		if !transferred {
			<-sema
		}
	}()
	if err := call.Context.Err(); err != nil {
		return AbortResult[Kind, Reason, Payload](), err
	}
	result, err := next(call)
	if result.Lifetime != nil {
		result.Lifetime.OnClose(func() { <-sema })
		transferred = true
	}
	return result, err
}
