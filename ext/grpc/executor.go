package routerygrpc

import (
	"context"
	"fmt"

	"github.com/skosovsky/routery"
)

// UnaryInvoker performs one unary RPC (typically a thin wrapper around a generated stub).
type UnaryInvoker[Req any, Res any] func(ctx context.Context, req Req) (Res, error)

// NewUnaryExecutor wraps invoker as a [routery.Executor]. If invoker is nil, returns an
// executor that always fails with [routery.ErrInvalidConfig].
func NewUnaryExecutor[Req any, Res any](invoker UnaryInvoker[Req, Res]) routery.Executor[Req, Res] {
	if invoker == nil {
		return invalidExecutor[Req, Res](configError("unary invoker is nil"))
	}
	return routery.ExecutorFunc[Req, Res](invoker)
}

func configError(detail string) error {
	return fmt.Errorf("%w: %s", routery.ErrInvalidConfig, detail)
}

func invalidExecutor[Req any, Res any](err error) routery.Executor[Req, Res] {
	return routery.ExecutorFunc[Req, Res](func(context.Context, Req) (Res, error) {
		var zero Res
		return zero, err
	})
}
