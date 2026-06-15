package routery

import "context"

// InvokeRouteHandler runs a route handler and returns the recorded outcome and error.
// It is intended for tests and simple leaf invocations outside RouteTable.
func InvokeRouteHandler[Req any, Res any](
	ctx context.Context,
	req Req,
	handler RouteHandler[Req, Res],
) (RouteOutcome[Res], error) {
	rec := NewResultRecorder[Res]()
	if err := handler(ctx, req, rec); err != nil {
		return abortOutcome[Res](), err
	}

	return outcomeFromRecorder(rec), nil
}
