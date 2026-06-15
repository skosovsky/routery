package routery

import (
	"context"
	"strconv"
)

// RouteHandler processes a request and records outcomes via ResultRecorder.
//
// System failures are reported via the returned error, not via ResultRecorder methods.
type RouteHandler[Req any, Res any] func(ctx context.Context, req Req, rec ResultRecorder[Res]) error

// RouteMiddleware decorates a route handler.
type RouteMiddleware[Req any, Res any] func(next RouteHandler[Req, Res]) RouteHandler[Req, Res]

// ApplyRoute applies middlewares to base in reverse order.
//
// The first middleware in the argument list becomes the outer wrapper.
func ApplyRoute[Req any, Res any](
	base RouteHandler[Req, Res],
	mws ...RouteMiddleware[Req, Res],
) RouteHandler[Req, Res] {
	if base == nil {
		return invalidRouteHandler[Req, Res](configError("base route handler is nil"))
	}

	for index := len(mws) - 1; index >= 0; index-- {
		middleware := mws[index]
		if middleware == nil {
			continue
		}

		base = middleware(base)
		if base == nil {
			return invalidRouteHandler[Req, Res](configError("middleware returned nil route handler"))
		}
	}

	return base
}

// FromFunc adapts a function that returns a result and error into a leaf RouteHandler.
func FromFunc[Req any, Res any](fn func(context.Context, Req) (Res, error)) RouteHandler[Req, Res] {
	if fn == nil {
		return invalidRouteHandler[Req, Res](configError("from func requires non-nil function"))
	}

	return func(ctx context.Context, req Req, rec ResultRecorder[Res]) error {
		result, err := fn(ctx, req)
		if err != nil {
			return err
		}

		rec.Stop(result, "")
		return nil
	}
}

func invalidRouteHandler[Req any, Res any](err error) RouteHandler[Req, Res] {
	return func(context.Context, Req, ResultRecorder[Res]) error {
		return err
	}
}

func validateRouteHandlers[Req any, Res any](
	handlers []RouteHandler[Req, Res],
	name string,
) ([]RouteHandler[Req, Res], error) {
	if len(handlers) == 0 {
		return nil, ErrNoHandlers
	}

	validated := make([]RouteHandler[Req, Res], len(handlers))
	copy(validated, handlers)

	for index, handler := range validated {
		if handler == nil {
			return nil, configError(name + " route handler at index " + strconv.Itoa(index) + " is nil")
		}
	}

	return validated, nil
}
