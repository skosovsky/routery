package routery

// Chain executes handlers until one returns a terminal result.
//
// ActionNext is a normal fallthrough signal, not an error and not a bool side channel.
func Chain[Req any, Kind comparable, Reason comparable, Payload any](
	handlers ...RouteHandler[Req, Kind, Reason, Payload],
) RouteHandler[Req, Kind, Reason, Payload] {
	validated, err := validateRouteHandlers(handlers, "chain")
	if err != nil {
		return invalidRouteHandler[Req, Kind, Reason, Payload](err)
	}

	return func(call RouteCall[Req]) (RouteResult[Kind, Reason, Payload], error) {
		var last RouteResult[Kind, Reason, Payload]
		for _, handler := range validated {
			if err := call.Context.Err(); err != nil {
				return AbortResult[Kind, Reason, Payload](), err
			}
			result, err := handler(call)
			if err != nil {
				result.Action = ActionAbort
				return result, err
			}
			result, err = validateReturnedResult(result)
			if err != nil {
				return result, err
			}
			last = result
			if result.Action != ActionNext {
				return result, nil
			}
			if closed, closeErr := discardResult(result, nil); closeErr != nil {
				return closed, closeErr
			}
		}

		if last.Action == ActionNext {
			return last, nil
		}

		var reason Reason
		return Next[Kind, Reason, Payload](reason), nil
	}
}
