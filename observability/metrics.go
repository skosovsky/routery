package observability

import (
	"context"
	"time"

	"github.com/skosovsky/routery"
)

// ResultMeta is a serializable summary of a route outcome for metrics callbacks.
type ResultMeta struct {
	Action     routery.RouteAction
	ReasonCode string
}

// MetricsHooks defines callbacks for metrics collection.
type MetricsHooks[Res any] struct {
	OnStart     func(ctx context.Context, name string)
	OnComplete  func(ctx context.Context, name string, duration time.Duration, result ResultMeta, payloadMeta PayloadMeta, err error)
	PayloadMeta PayloadMetaFunc[Res]
}

// Metrics emits start and completion callbacks around execution.
func Metrics[Req any, Res any](name string, hooks MetricsHooks[Res]) routery.RouteMiddleware[Req, Res] {
	if hooks.OnStart == nil && hooks.OnComplete == nil {
		return func(next routery.RouteHandler[Req, Res]) routery.RouteHandler[Req, Res] {
			if next == nil {
				return invalidRouteHandler[Req, Res]("metrics middleware requires non-nil next route handler")
			}
			return next
		}
	}

	return func(next routery.RouteHandler[Req, Res]) routery.RouteHandler[Req, Res] {
		if next == nil {
			return invalidRouteHandler[Req, Res]("metrics middleware requires non-nil next route handler")
		}

		return func(ctx context.Context, req Req, rec routery.ResultRecorder[Res]) error {
			if hooks.OnStart != nil {
				hooks.OnStart(ctx, name)
			}

			start := time.Now()
			handleErr := next(ctx, req, rec)

			if hooks.OnComplete != nil {
				meta := resolvePayloadMeta(ctx, rec, hooks.PayloadMeta)
				action := rec.Action()
				if handleErr != nil {
					action = routery.ActionAbort
				}
				resultMeta := ResultMeta{
					Action:     action,
					ReasonCode: rec.ReasonCode(),
				}
				hooks.OnComplete(ctx, name, time.Since(start), resultMeta, meta, handleErr)
			}

			return handleErr
		}
	}
}
