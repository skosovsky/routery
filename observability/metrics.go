package observability

import (
	"context"
	"time"

	"github.com/skosovsky/routery"
)

// ResultMeta is a serializable summary of a route result for metrics callbacks.
//
// It intentionally omits payload values; use [PayloadMeta] for safe payload telemetry.
type ResultMeta struct {
	Status     routery.RouteStatus
	ReasonCode string
}

// MetricsHooks defines callbacks for metrics collection.
type MetricsHooks[Res any] struct {
	OnStart     func(ctx context.Context, name string)
	OnComplete  func(ctx context.Context, name string, duration time.Duration, result ResultMeta, payloadMeta PayloadMeta, err error)
	PayloadMeta PayloadMetaFunc[Res]
}

// Metrics emits start and completion callbacks around execution.
func Metrics[Req any, Res any](name string, hooks MetricsHooks[Res]) routery.HandlerMiddleware[Req, Res] {
	if hooks.OnStart == nil && hooks.OnComplete == nil {
		return func(next routery.Handler[Req, Res]) routery.Handler[Req, Res] {
			if next == nil {
				return invalidHandler[Req, Res]("metrics middleware requires non-nil next handler")
			}
			return next
		}
	}

	return func(next routery.Handler[Req, Res]) routery.Handler[Req, Res] {
		if next == nil {
			return invalidHandler[Req, Res]("metrics middleware requires non-nil next handler")
		}

		return routery.HandlerFunc[Req, Res](func(ctx context.Context, req Req) (routery.RouteResult[Res], error) {
			if hooks.OnStart != nil {
				hooks.OnStart(ctx, name)
			}

			start := time.Now()
			result, handleErr := next.Handle(ctx, req)

			if hooks.OnComplete != nil {
				meta := resolvePayloadMeta(ctx, result, hooks.PayloadMeta)
				resultMeta := ResultMeta{
					Status:     result.Status,
					ReasonCode: result.ReasonCode,
				}
				hooks.OnComplete(ctx, name, time.Since(start), resultMeta, meta, handleErr)
			}

			return result, handleErr
		})
	}
}
