package routerygrpc

import (
	"context"
	"fmt"

	"github.com/skosovsky/routery"
)

// UnaryInvoker performs one unary RPC (typically a thin wrapper around a generated stub).
type UnaryInvoker[Req any, Res any] func(ctx context.Context, req Req) (Res, error)

// NewUnaryHandler wraps invoker as a [routery.Handler]. If invoker is nil, returns a
// handler that always fails with [routery.ErrInvalidConfig].
func NewUnaryHandler[Req any, Res any](invoker UnaryInvoker[Req, Res]) routery.Handler[Req, Res] {
	if invoker == nil {
		return invalidHandler[Req, Res](configError("unary invoker is nil"))
	}

	return routery.HandlerFunc[Req, Res](func(ctx context.Context, req Req) (routery.RouteResult[Res], error) {
		response, err := invoker(ctx, req)
		if err != nil {
			return routery.RouteResult[Res]{}, err
		}

		return routery.Handled(response), nil
	})
}

func configError(detail string) error {
	return fmt.Errorf("%w: %s", routery.ErrInvalidConfig, detail)
}

func invalidHandler[Req any, Res any](err error) routery.Handler[Req, Res] {
	return routery.HandlerFunc[Req, Res](func(context.Context, Req) (routery.RouteResult[Res], error) {
		return routery.RouteResult[Res]{}, err
	})
}
