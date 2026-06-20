package routery

// RouteEvent describes one observed route handler invocation.
type RouteEvent[Kind comparable, Reason comparable, Payload any] struct {
	Match  RouteMatch
	Result RouteResult[Kind, Reason, Payload]
	Err    error
}

// OutcomeSink receives route events emitted by DispatchWithSink.
type OutcomeSink[Kind comparable, Reason comparable, Payload any] interface {
	Observe(RouteEvent[Kind, Reason, Payload])
}

// OutcomeSinkFunc adapts a function to OutcomeSink.
type OutcomeSinkFunc[Kind comparable, Reason comparable, Payload any] func(RouteEvent[Kind, Reason, Payload])

// Observe implements OutcomeSink.
func (fn OutcomeSinkFunc[Kind, Reason, Payload]) Observe(event RouteEvent[Kind, Reason, Payload]) {
	if fn != nil {
		fn(event)
	}
}

func emitRouteEvent[Kind comparable, Reason comparable, Payload any](
	sink OutcomeSink[Kind, Reason, Payload],
	match RouteMatch,
	result RouteResult[Kind, Reason, Payload],
	err error,
) {
	if sink == nil {
		return
	}

	sink.Observe(RouteEvent[Kind, Reason, Payload]{
		Match:  match,
		Result: result,
		Err:    err,
	})
}
