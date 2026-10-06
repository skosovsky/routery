package routeryredis

import (
	"context"
	"errors"
	"fmt"

	"github.com/redis/go-redis/v9"

	"github.com/skosovsky/routery"
)

const errorPrefix = "routery/ext/redis"

// CommandInvoker executes a command through a caller-owned Redis client and returns its handle.
type CommandInvoker[Req any] func(ctx context.Context, req Req) (redis.Cmder, error)

// ScanResult maps a successful command to the response type after [redis.Cmder.Err] is nil.
type ScanResult[Res any] func(ctx context.Context, cmd redis.Cmder) (Res, error)

// NewRouteHandler returns a route handler that checks commands already executed by invoker and maps successful results.
func NewRouteHandler[Req any, Res any](
	invoker CommandInvoker[Req],
	scan ScanResult[Res],
) routery.BasicRouteHandler[Req, Res] {
	if invoker == nil {
		return invalidRouteHandler[Req, Res](configError("command invoker is nil"))
	}
	if scan == nil {
		return invalidRouteHandler[Req, Res](configError("scan result function is nil"))
	}

	return func(call routery.RouteCall[Req]) (routery.BasicRouteResult[Res], error) {
		cmd, err := invoker(call.Context, call.Request)
		if err != nil {
			return routery.AbortResult[routery.BasicKind, routery.BasicReason, Res](), err
		}
		if cmd == nil {
			return routery.AbortResult[routery.BasicKind, routery.BasicReason, Res](),
				configError("command invoker returned nil command")
		}

		cmdErr := cmd.Err()
		if errors.Is(cmdErr, redis.Nil) {
			return routery.AbortResult[routery.BasicKind, routery.BasicReason, Res](), redis.Nil
		}
		if cmdErr != nil {
			return routery.AbortResult[routery.BasicKind, routery.BasicReason, Res](), cmdErr
		}

		result, scanErr := scan(call.Context, cmd)
		if scanErr != nil {
			return routery.AbortResult[routery.BasicKind, routery.BasicReason, Res](), scanErr
		}

		return routery.BasicHandled(result), nil
	}
}

// NewStringRouteHandler is a convenience constructor for string results ([redis.StringCmd]).
func NewStringRouteHandler[Req any](
	invoker CommandInvoker[Req],
) routery.BasicRouteHandler[Req, string] {
	return NewRouteHandler(invoker, func(_ context.Context, cmd redis.Cmder) (string, error) {
		sc, ok := cmd.(*redis.StringCmd)
		if !ok {
			return "", fmt.Errorf("%s: expected *redis.StringCmd, got %T", errorPrefix, cmd)
		}
		return sc.Result()
	})
}

func configError(detail string) error {
	return fmt.Errorf("%w: %s", routery.ErrInvalidConfig, detail)
}

func invalidRouteHandler[Req any, Res any](err error) routery.BasicRouteHandler[Req, Res] {
	return func(routery.RouteCall[Req]) (routery.BasicRouteResult[Res], error) {
		return routery.AbortResult[routery.BasicKind, routery.BasicReason, Res](), err
	}
}
