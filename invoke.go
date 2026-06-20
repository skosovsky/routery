package routery

import "context"

// InvokeRouteHandler runs a route handler and returns its typed result and error.
// It is intended for tests and simple leaf invocations outside RouteTable.
func InvokeRouteHandler[Req any, Kind comparable, Reason comparable, Payload any](
	ctx context.Context,
	req Req,
	handler RouteHandler[Req, Kind, Reason, Payload],
) (RouteResult[Kind, Reason, Payload], error) {
	if handler == nil {
		err := configError("route handler is nil")
		return AbortResult[Kind, Reason, Payload](), err
	}

	result, err := handler(NewRouteCall(ctx, req))
	if err != nil {
		return AbortResult[Kind, Reason, Payload]().WithMatch(result.Match), err
	}

	return validateReturnedResult(result)
}
