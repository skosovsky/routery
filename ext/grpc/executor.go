package routerygrpc

import (
	"context"
	"fmt"

	"github.com/skosovsky/routery"
)

// UnaryInvoker performs one unary RPC (typically a thin wrapper around a generated stub).
type UnaryInvoker[Req any, Res any] func(ctx context.Context, req Req) (Res, error)

// NewUnaryRouteHandler wraps invoker as a [routery.BasicRouteHandler].
func NewUnaryRouteHandler[Req any, Res any](invoker UnaryInvoker[Req, Res]) routery.BasicRouteHandler[Req, Res] {
	if invoker == nil {
		return invalidRouteHandler[Req, Res](configError("unary invoker is nil"))
	}

	return func(call routery.RouteCall[Req]) (routery.BasicRouteResult[Res], error) {
		response, err := invoker(call.Context, call.Request)
		if err != nil {
			return routery.AbortResult[routery.BasicKind, routery.BasicReason, Res](), err
		}

		return routery.BasicHandled(response), nil
	}
}

func configError(detail string) error {
	return fmt.Errorf("%w: %s", routery.ErrInvalidConfig, detail)
}

func invalidRouteHandler[Req any, Res any](err error) routery.BasicRouteHandler[Req, Res] {
	return func(routery.RouteCall[Req]) (routery.BasicRouteResult[Res], error) {
		return routery.AbortResult[routery.BasicKind, routery.BasicReason, Res](), err
	}
}
