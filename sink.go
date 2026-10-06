package routery

// RouteEvent describes one observed route handler invocation.
type RouteEvent[Kind comparable, Reason comparable, Payload any] struct {
	Match  RouteMatch
	Result RouteResult[Kind, Reason, Payload]
	Err    error
}

// OutcomeSink receives synchronous route events emitted by DispatchWithSink.
// Match paths are detached. Payload, decision reasons, errors and Lifetime are borrowed
// read-only references: Observe must not mutate them or close the canonical owner.
// Hosts must project and redact raw events before exporting telemetry.
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

	result.Match = cloneRouteMatch(result.Match)
	sink.Observe(RouteEvent[Kind, Reason, Payload]{
		Match:  cloneRouteMatch(match),
		Result: result,
		Err:    err,
	})
}
