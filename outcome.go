package routery

// RouteAction describes the routing engine control flow after a handler invocation.
type RouteAction string

const (
	// ActionNext continues to the next route in the table or chain.
	ActionNext RouteAction = "next"
	// ActionStop terminates routing with a typed result.
	ActionStop RouteAction = "stop"
	// ActionAbort indicates a system failure reported by the engine when a handler returns an error.
	ActionAbort RouteAction = "abort"
)

// BasicKind is a small convenience terminal-kind type for adapters and simple examples.
//
// It is not required by the core routing contract: callers can provide their own Kind type.
type BasicKind string

const (
	BasicKindHandled BasicKind = "handled"
	BasicKindIgnored BasicKind = "ignored"
	BasicKindAsync   BasicKind = "async"
)

// BasicReason is a convenience reason type for adapters and simple examples.
//
// It is not required by the core routing contract: callers can provide their own Reason type.
type BasicReason string

const (
	BasicReasonNone    BasicReason = ""
	BasicReasonNoMatch BasicReason = "no_match"
)

// RouteResult is the typed result of a route dispatch or leaf handler invocation.
//
// Action is the engine control-flow instruction. Kind and Reason belong to the caller's
// domain and are generic to avoid forcing string reason codes into the core contract.
type RouteResult[Kind comparable, Reason comparable, Payload any] struct {
	Action     RouteAction
	Kind       Kind
	Reason     Reason
	Payload    Payload
	HasPayload bool
	Match      RouteMatch
}

// BasicRouteResult is a convenience result for adapters that do not need custom Kind or Reason types.
type BasicRouteResult[Payload any] = RouteResult[BasicKind, BasicReason, Payload]

// Handled returns a terminal result with payload.
func Handled[Kind comparable, Reason comparable, Payload any](
	kind Kind,
	reason Reason,
	payload Payload,
) RouteResult[Kind, Reason, Payload] {
	return RouteResult[Kind, Reason, Payload]{
		Action:     ActionStop,
		Kind:       kind,
		Reason:     reason,
		Payload:    payload,
		HasPayload: true,
		Match:      zeroRouteMatch(),
	}
}

// Ignored returns a terminal result without payload.
func Ignored[Kind comparable, Reason comparable, Payload any](
	kind Kind,
	reason Reason,
) RouteResult[Kind, Reason, Payload] {
	var payload Payload
	return RouteResult[Kind, Reason, Payload]{
		Action:     ActionStop,
		Kind:       kind,
		Reason:     reason,
		Payload:    payload,
		HasPayload: false,
		Match:      zeroRouteMatch(),
	}
}

// Async returns a terminal asynchronous result with payload.
func Async[Kind comparable, Reason comparable, Payload any](
	kind Kind,
	reason Reason,
	payload Payload,
) RouteResult[Kind, Reason, Payload] {
	return RouteResult[Kind, Reason, Payload]{
		Action:     ActionStop,
		Kind:       kind,
		Reason:     reason,
		Payload:    payload,
		HasPayload: true,
		Match:      zeroRouteMatch(),
	}
}

// Next returns a non-terminal result that delegates to the next route.
func Next[Kind comparable, Reason comparable, Payload any](
	reason Reason,
) RouteResult[Kind, Reason, Payload] {
	var kind Kind
	var payload Payload
	return RouteResult[Kind, Reason, Payload]{
		Action:     ActionNext,
		Kind:       kind,
		Reason:     reason,
		Payload:    payload,
		HasPayload: false,
		Match:      zeroRouteMatch(),
	}
}

// AbortResult returns a system-failure result. The error remains the authoritative failure value.
func AbortResult[Kind comparable, Reason comparable, Payload any]() RouteResult[Kind, Reason, Payload] {
	var kind Kind
	var reason Reason
	var payload Payload
	return RouteResult[Kind, Reason, Payload]{
		Action:     ActionAbort,
		Kind:       kind,
		Reason:     reason,
		Payload:    payload,
		HasPayload: false,
		Match:      zeroRouteMatch(),
	}
}

func abortWithoutError[Kind comparable, Reason comparable, Payload any](
	match RouteMatch,
) (RouteResult[Kind, Reason, Payload], error) {
	return AbortResult[Kind, Reason, Payload]().WithMatch(match),
		configError("route returned abort action without error")
}

func validateReturnedResult[Kind comparable, Reason comparable, Payload any](
	result RouteResult[Kind, Reason, Payload],
) (RouteResult[Kind, Reason, Payload], error) {
	switch result.Action {
	case ActionNext, ActionStop:
		return result, nil
	case ActionAbort:
		return abortWithoutError[Kind, Reason, Payload](result.Match)
	default:
		return AbortResult[Kind, Reason, Payload]().WithMatch(result.Match),
			configError("unexpected route action: " + string(result.Action))
	}
}

// WithMatch returns a copy of result annotated with route metadata.
func (result RouteResult[Kind, Reason, Payload]) WithMatch(match RouteMatch) RouteResult[Kind, Reason, Payload] {
	result.Match = match
	return result
}

// BasicHandled returns a basic terminal result with payload.
func BasicHandled[Payload any](payload Payload) BasicRouteResult[Payload] {
	return Handled(BasicKindHandled, BasicReasonNone, payload)
}

// BasicIgnored returns a basic terminal result without payload.
func BasicIgnored[Payload any](reason BasicReason) BasicRouteResult[Payload] {
	return Ignored[BasicKind, BasicReason, Payload](BasicKindIgnored, reason)
}

// BasicAsync returns a basic asynchronous result with payload.
func BasicAsync[Payload any](payload Payload, reason BasicReason) BasicRouteResult[Payload] {
	return Async(BasicKindAsync, reason, payload)
}

// BasicNext returns a basic non-terminal result.
func BasicNext[Payload any](reason BasicReason) BasicRouteResult[Payload] {
	return Next[BasicKind, BasicReason, Payload](reason)
}
