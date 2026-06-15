package routerygrpc

import (
	"context"
	"fmt"

	"github.com/skosovsky/routery"
)

// UnaryInvoker performs one unary RPC (typically a thin wrapper around a generated stub).
type UnaryInvoker[Req any, Res any] func(ctx context.Context, req Req) (Res, error)

// NewUnaryRouteHandler wraps invoker as a [routery.RouteHandler].
func NewUnaryRouteHandler[Req any, Res any](invoker UnaryInvoker[Req, Res]) routery.RouteHandler[Req, Res] {
	if invoker == nil {
		return invalidRouteHandler[Req, Res](configError("unary invoker is nil"))
	}

	return func(ctx context.Context, req Req, rec routery.ResultRecorder[Res]) error {
		response, err := invoker(ctx, req)
		if err != nil {
			return err
		}

		rec.Stop(response, "")
		return nil
	}
}

func configError(detail string) error {
	return fmt.Errorf("%w: %s", routery.ErrInvalidConfig, detail)
}

func invalidRouteHandler[Req any, Res any](err error) routery.RouteHandler[Req, Res] {
	return func(context.Context, Req, routery.ResultRecorder[Res]) error {
		return err
	}
}
