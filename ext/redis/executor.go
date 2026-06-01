package routeryredis

import (
	"context"
	"errors"
	"fmt"

	"github.com/redis/go-redis/v9"

	"github.com/skosovsky/routery"
)

const errorPrefix = "routery/ext/redis"

// CommandExtractor maps a request to a Redis command handle.
type CommandExtractor[Req any] func(ctx context.Context, req Req) (redis.Cmder, error)

// ScanResult maps a successful command to the response type after [redis.Cmder.Err] is nil.
type ScanResult[Res any] func(ctx context.Context, cmd redis.Cmder) (Res, error)

// NewHandler returns a handler that runs commands produced by extractor.
func NewHandler[Req any, Res any](
	client *redis.Client,
	extractor CommandExtractor[Req],
	scan ScanResult[Res],
) routery.Handler[Req, Res] {
	if client == nil {
		return invalidHandler[Req, Res](configError("redis client is nil"))
	}
	if extractor == nil {
		return invalidHandler[Req, Res](configError("command extractor is nil"))
	}
	if scan == nil {
		return invalidHandler[Req, Res](configError("scan result function is nil"))
	}

	return routery.HandlerFunc[Req, Res](func(ctx context.Context, req Req) (routery.RouteResult[Res], error) {
		cmd, err := extractor(ctx, req)
		if err != nil {
			return routery.RouteResult[Res]{}, err
		}
		if cmd == nil {
			return routery.RouteResult[Res]{}, configError("command extractor returned nil command")
		}

		cmdErr := cmd.Err()
		if errors.Is(cmdErr, redis.Nil) {
			return routery.RouteResult[Res]{}, redis.Nil
		}
		if cmdErr != nil {
			return routery.RouteResult[Res]{}, cmdErr
		}

		result, scanErr := scan(ctx, cmd)
		if scanErr != nil {
			return routery.RouteResult[Res]{}, scanErr
		}

		return routery.Handled(result), nil
	})
}

// NewStringHandler is a convenience constructor for string results ([redis.StringCmd]).
func NewStringHandler[Req any](client *redis.Client, extractor CommandExtractor[Req]) routery.Handler[Req, string] {
	return NewHandler(client, extractor, func(_ context.Context, cmd redis.Cmder) (string, error) {
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

func invalidHandler[Req any, Res any](err error) routery.Handler[Req, Res] {
	return routery.HandlerFunc[Req, Res](func(context.Context, Req) (routery.RouteResult[Res], error) {
		return routery.RouteResult[Res]{}, err
	})
}
