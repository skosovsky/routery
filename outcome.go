package routery

// RouteOutcome is the aggregated result of a route dispatch or leaf handler invocation.
type RouteOutcome[Res any] struct {
	Payload    Res
	HasPayload bool
	Action     RouteAction
	ReasonCode string
}

func outcomeFromRecorder[Res any](rec ResultRecorder[Res]) RouteOutcome[Res] {
	payload, ok := rec.Payload()
	return RouteOutcome[Res]{
		Payload:    payload,
		HasPayload: ok,
		Action:     rec.Action(),
		ReasonCode: rec.ReasonCode(),
	}
}

func abortOutcome[Res any]() RouteOutcome[Res] {
	return RouteOutcome[Res]{Action: ActionAbort} //nolint:exhaustruct // only Action is set on handler error.
}
