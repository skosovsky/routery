package routery

import (
	"context"
	"strconv"
)

// RouteCall is the explicit input passed to route handlers.
//
// Context remains responsible for cancellation and deadlines. Routing metadata is carried
// separately in Match so handlers and middleware never need [context.WithValue] side channels.
type RouteCall[Req any] struct {
	Context context.Context
	Request Req
	Match   RouteMatch
	state   *routeCallState
}

type routeCallState struct {
	decisions map[any]any
}

// NewRouteCall creates a route call for direct handler invocation.
func NewRouteCall[Req any](ctx context.Context, req Req) RouteCall[Req] {
	if ctx == nil {
		ctx = context.Background()
	}

	return RouteCall[Req]{
		Context: ctx,
		Request: req,
		Match:   zeroRouteMatch(),
		state: &routeCallState{
			decisions: nil,
		},
	}
}

func (call RouteCall[Req]) withMatch(match RouteMatch) RouteCall[Req] {
	call.Match = match
	return call
}

// WithContext returns a copy of call with a different context.
func (call RouteCall[Req]) WithContext(ctx context.Context) RouteCall[Req] {
	return call.withContext(ctx)
}

func (call RouteCall[Req]) withContext(ctx context.Context) RouteCall[Req] {
	if ctx == nil {
		ctx = context.Background()
	}
	call.Context = ctx
	return call
}

// RouteHandler processes a request and returns a typed routing result.
//
// System failures are reported via the returned error, not via RouteResult.
type RouteHandler[Req any, Kind comparable, Reason comparable, Payload any] func(
	RouteCall[Req],
) (RouteResult[Kind, Reason, Payload], error)

// BasicRouteHandler is a convenience handler for adapters that do not need custom Kind or Reason types.
type BasicRouteHandler[Req any, Payload any] = RouteHandler[Req, BasicKind, BasicReason, Payload]

// RouteMiddleware decorates a route handler.
type RouteMiddleware[Req any, Kind comparable, Reason comparable, Payload any] func(
	next RouteHandler[Req, Kind, Reason, Payload],
) RouteHandler[Req, Kind, Reason, Payload]

// BasicRouteMiddleware is a convenience middleware for adapters that use BasicKind and BasicReason.
type BasicRouteMiddleware[Req any, Payload any] = RouteMiddleware[Req, BasicKind, BasicReason, Payload]

// ApplyRoute applies middlewares to base in reverse order.
//
// The first middleware in the argument list becomes the outer wrapper.
func ApplyRoute[Req any, Kind comparable, Reason comparable, Payload any](
	base RouteHandler[Req, Kind, Reason, Payload],
	mws ...RouteMiddleware[Req, Kind, Reason, Payload],
) RouteHandler[Req, Kind, Reason, Payload] {
	if base == nil {
		return invalidRouteHandler[Req, Kind, Reason, Payload](configError("base route handler is nil"))
	}

	for index := len(mws) - 1; index >= 0; index-- {
		middleware := mws[index]
		if middleware == nil {
			continue
		}

		base = middleware(base)
		if base == nil {
			return invalidRouteHandler[Req, Kind, Reason, Payload](configError("middleware returned nil route handler"))
		}
	}

	return base
}

// FromFunc adapts a function that returns a payload and error into a basic leaf RouteHandler.
func FromFunc[Req any, Payload any](fn func(context.Context, Req) (Payload, error)) BasicRouteHandler[Req, Payload] {
	if fn == nil {
		return invalidRouteHandler[Req, BasicKind, BasicReason, Payload](
			configError("from func requires non-nil function"),
		)
	}

	return func(call RouteCall[Req]) (BasicRouteResult[Payload], error) {
		result, err := fn(call.Context, call.Request)
		if err != nil {
			return AbortResult[BasicKind, BasicReason, Payload](), err
		}

		return BasicHandled(result), nil
	}
}

// FromResultFunc adapts a function that already returns a typed RouteResult.
func FromResultFunc[Req any, Kind comparable, Reason comparable, Payload any](
	fn func(context.Context, Req) (RouteResult[Kind, Reason, Payload], error),
) RouteHandler[Req, Kind, Reason, Payload] {
	if fn == nil {
		return invalidRouteHandler[Req, Kind, Reason, Payload](
			configError("from result func requires non-nil function"),
		)
	}

	return func(call RouteCall[Req]) (RouteResult[Kind, Reason, Payload], error) {
		result, err := fn(call.Context, call.Request)
		if err != nil {
			return AbortResult[Kind, Reason, Payload](), err
		}

		return result, nil
	}
}

func invalidRouteHandler[Req any, Kind comparable, Reason comparable, Payload any](
	err error,
) RouteHandler[Req, Kind, Reason, Payload] {
	return func(RouteCall[Req]) (RouteResult[Kind, Reason, Payload], error) {
		return AbortResult[Kind, Reason, Payload](), err
	}
}

func validateRouteHandlers[Req any, Kind comparable, Reason comparable, Payload any](
	handlers []RouteHandler[Req, Kind, Reason, Payload],
	name string,
) ([]RouteHandler[Req, Kind, Reason, Payload], error) {
	if len(handlers) == 0 {
		return nil, ErrNoHandlers
	}

	validated := make([]RouteHandler[Req, Kind, Reason, Payload], len(handlers))
	copy(validated, handlers)

	for index, handler := range validated {
		if handler == nil {
			return nil, configError(name + " route handler at index " + strconv.Itoa(index) + " is nil")
		}
	}

	return validated, nil
}
