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

// NewRouteHandler returns a route handler that runs commands produced by extractor.
func NewRouteHandler[Req any, Res any](
	client *redis.Client,
	extractor CommandExtractor[Req],
	scan ScanResult[Res],
) routery.RouteHandler[Req, Res] {
	if client == nil {
		return invalidRouteHandler[Req, Res](configError("redis client is nil"))
	}
	if extractor == nil {
		return invalidRouteHandler[Req, Res](configError("command extractor is nil"))
	}
	if scan == nil {
		return invalidRouteHandler[Req, Res](configError("scan result function is nil"))
	}

	return func(ctx context.Context, req Req, rec routery.ResultRecorder[Res]) error {
		cmd, err := extractor(ctx, req)
		if err != nil {
			return err
		}
		if cmd == nil {
			return configError("command extractor returned nil command")
		}

		cmdErr := cmd.Err()
		if errors.Is(cmdErr, redis.Nil) {
			return redis.Nil
		}
		if cmdErr != nil {
			return cmdErr
		}

		result, scanErr := scan(ctx, cmd)
		if scanErr != nil {
			return scanErr
		}

		rec.Stop(result, "")
		return nil
	}
}

// NewStringRouteHandler is a convenience constructor for string results ([redis.StringCmd]).
func NewStringRouteHandler[Req any](
	client *redis.Client,
	extractor CommandExtractor[Req],
) routery.RouteHandler[Req, string] {
	return NewRouteHandler(client, extractor, func(_ context.Context, cmd redis.Cmder) (string, error) {
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

func invalidRouteHandler[Req any, Res any](err error) routery.RouteHandler[Req, Res] {
	return func(context.Context, Req, routery.ResultRecorder[Res]) error {
		return err
	}
}
