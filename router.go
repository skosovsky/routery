package routery

import "context"

// Router is the immutable dispatch entry point for a built route table.
type Router[Req any, Kind comparable, Reason comparable, Payload any] interface {
	Dispatch(ctx context.Context, req Req) (RouteResult[Kind, Reason, Payload], error)
	DispatchWithSink(
		ctx context.Context,
		req Req,
		sink OutcomeSink[Kind, Reason, Payload],
	) (RouteResult[Kind, Reason, Payload], error)
	Snapshot() RouteSnapshot[tableSnapshot]
}

// BasicRouter is a convenience router for adapters that do not need custom Kind or Reason types.
type BasicRouter[Req any, Payload any] = Router[Req, BasicKind, BasicReason, Payload]

type routerImpl[Req any, Kind comparable, Reason comparable, Payload any] struct {
	table       *builtTable[Req, Kind, Reason, Payload]
	fingerprint string
}

// Snapshot returns the current routing snapshot with fingerprint.
// Fingerprint reflects route topology (ids, priorities, match kinds, and static keys),
// not handler function identity.
func (router *routerImpl[Req, Kind, Reason, Payload]) Snapshot() RouteSnapshot[tableSnapshot] {
	return RouteSnapshot[tableSnapshot]{
		Fingerprint: router.fingerprint,
		State:       router.table.snapshotState(),
	}
}

// Dispatch routes a request through the built table.
func (router *routerImpl[Req, Kind, Reason, Payload]) Dispatch(
	ctx context.Context,
	req Req,
) (RouteResult[Kind, Reason, Payload], error) {
	return router.DispatchWithSink(ctx, req, nil)
}

// DispatchWithSink routes a request and emits route events into sink.
func (router *routerImpl[Req, Kind, Reason, Payload]) DispatchWithSink(
	ctx context.Context,
	req Req,
	sink OutcomeSink[Kind, Reason, Payload],
) (RouteResult[Kind, Reason, Payload], error) {
	if ctx == nil {
		ctx = context.Background()
	}

	return dispatchTable(NewRouteCall(ctx, req), router.table, nil, 0, sink)
}

func dispatchTable[Req any, Kind comparable, Reason comparable, Payload any](
	call RouteCall[Req],
	table *builtTable[Req, Kind, Reason, Payload],
	parentPath []RouteID,
	depth int,
	sink OutcomeSink[Kind, Reason, Payload],
) (RouteResult[Kind, Reason, Payload], error) {
	var lastNext RouteResult[Kind, Reason, Payload]
	hasNext := false

	for _, entry := range table.routes {
		result, terminal, err := dispatchEntry(call, entry, parentPath, depth, sink)
		if err != nil {
			return result, err
		}
		if terminal {
			return result, nil
		}
		if result.Action == ActionNext {
			lastNext = result
			hasNext = true
		}
	}

	if table.fallback != nil {
		return dispatchFallback(call, table.fallback, parentPath, depth, sink)
	}

	if hasNext {
		return lastNext, nil
	}

	return noRouteResult[Kind, Reason, Payload](), nil
}

func dispatchEntry[Req any, Kind comparable, Reason comparable, Payload any](
	call RouteCall[Req],
	entry routeEntry[Req, Kind, Reason, Payload],
	parentPath []RouteID,
	depth int,
	sink OutcomeSink[Kind, Reason, Payload],
) (RouteResult[Kind, Reason, Payload], bool, error) {
	match, ok, err := entry.matchRoute(call, parentPath, depth)
	if err != nil {
		result := AbortResult[Kind, Reason, Payload]().WithMatch(match)
		emitRouteEvent(sink, match, result, err)
		return result, true, err
	}
	if !ok {
		var result RouteResult[Kind, Reason, Payload]
		return result, false, nil
	}

	if entry.nested != nil {
		result, nestedErr := dispatchTable(call.withMatch(match), entry.nested, match.Path, depth+1, sink)
		return result, result.Action != ActionNext || nestedErr != nil, nestedErr
	}

	return dispatchHandler(call, entry.handler, match, sink)
}

func dispatchHandler[Req any, Kind comparable, Reason comparable, Payload any](
	call RouteCall[Req],
	handler RouteHandler[Req, Kind, Reason, Payload],
	match RouteMatch,
	sink OutcomeSink[Kind, Reason, Payload],
) (RouteResult[Kind, Reason, Payload], bool, error) {
	result, err := handler(call.withMatch(match))
	if err != nil {
		result = AbortResult[Kind, Reason, Payload]().WithMatch(match)
		emitRouteEvent(sink, match, result, err)
		return result, true, err
	}

	result = result.WithMatch(match)
	result, validationErr := validateReturnedResult(result)
	if validationErr != nil {
		emitRouteEvent(sink, match, result, validationErr)
		return result, true, validationErr
	}
	emitRouteEvent(sink, match, result, nil)

	if result.Action == ActionNext {
		return result, false, nil
	}

	return result, true, nil
}

func dispatchFallback[Req any, Kind comparable, Reason comparable, Payload any](
	call RouteCall[Req],
	fallback RouteHandler[Req, Kind, Reason, Payload],
	parentPath []RouteID,
	depth int,
	sink OutcomeSink[Kind, Reason, Payload],
) (RouteResult[Kind, Reason, Payload], error) {
	match := fallbackMatch(parentPath, depth)
	result, err := fallback(call.withMatch(match))
	if err != nil {
		result = AbortResult[Kind, Reason, Payload]().WithMatch(match)
		emitRouteEvent(sink, match, result, err)
		return result, err
	}

	result = result.WithMatch(match)
	result, validationErr := validateReturnedResult(result)
	if validationErr != nil {
		emitRouteEvent(sink, match, result, validationErr)
		return result, validationErr
	}
	emitRouteEvent(sink, match, result, nil)

	return result, nil
}

func noRouteResult[Kind comparable, Reason comparable, Payload any]() RouteResult[Kind, Reason, Payload] {
	var reason Reason
	return Next[Kind, Reason, Payload](reason)
}

func fallbackMatch(parentPath []RouteID, depth int) RouteMatch {
	path := appendRouteID(parentPath, RouteID("fallback"))
	return RouteMatch{
		RouteID:           RouteID("fallback"),
		Path:              path,
		Priority:          0,
		Depth:             depth,
		Kind:              MatchKindFallback,
		Key:               "",
		Prefix:            "",
		Remainder:         "",
		DecisionReason:    nil,
		HasDecisionReason: false,
	}
}

func appendRouteID(parent []RouteID, id RouteID) []RouteID {
	path := make([]RouteID, 0, len(parent)+1)
	path = append(path, parent...)
	path = append(path, id)
	return path
}
