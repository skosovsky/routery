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

// NewExecutor returns an executor that runs commands produced by extractor.
func NewExecutor[Req any, Res any](
	client *redis.Client,
	extractor CommandExtractor[Req],
	scan ScanResult[Res],
) routery.Executor[Req, Res] {
	if client == nil {
		return invalidExecutor[Req, Res](configError("redis client is nil"))
	}
	if extractor == nil {
		return invalidExecutor[Req, Res](configError("command extractor is nil"))
	}
	if scan == nil {
		return invalidExecutor[Req, Res](configError("scan result function is nil"))
	}

	return routery.ExecutorFunc[Req, Res](func(ctx context.Context, req Req) (Res, error) {
		var zero Res

		cmd, err := extractor(ctx, req)
		if err != nil {
			return zero, err
		}
		if cmd == nil {
			return zero, configError("command extractor returned nil command")
		}

		cmdErr := cmd.Err()
		if errors.Is(cmdErr, redis.Nil) {
			return zero, redis.Nil
		}
		if cmdErr != nil {
			return zero, cmdErr
		}

		return scan(ctx, cmd)
	})
}

// NewStringExecutor is a convenience constructor for string results ([redis.StringCmd]).
func NewStringExecutor[Req any](client *redis.Client, extractor CommandExtractor[Req]) routery.Executor[Req, string] {
	return NewExecutor(client, extractor, func(_ context.Context, cmd redis.Cmder) (string, error) {
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

func invalidExecutor[Req any, Res any](err error) routery.Executor[Req, Res] {
	return routery.ExecutorFunc[Req, Res](func(context.Context, Req) (Res, error) {
		var zero Res
		return zero, err
	})
}
