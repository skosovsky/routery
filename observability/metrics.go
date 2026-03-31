package observability

import (
	"context"
	"time"

	"github.com/skosovsky/routery"
)

// MetricsHooks defines callbacks for metrics collection.
type MetricsHooks struct {
	OnStart    func(ctx context.Context, name string)
	OnComplete func(ctx context.Context, name string, duration time.Duration, err error)
}

// Metrics emits start and completion callbacks around execution.
func Metrics[Req any, Res any](name string, hooks MetricsHooks) routery.Middleware[Req, Res] {
	if hooks.OnStart == nil && hooks.OnComplete == nil {
		return func(next routery.Executor[Req, Res]) routery.Executor[Req, Res] {
			if next == nil {
				return invalidExecutor[Req, Res]("metrics middleware requires non-nil next executor")
			}
			return next
		}
	}

	return func(next routery.Executor[Req, Res]) routery.Executor[Req, Res] {
		if next == nil {
			return invalidExecutor[Req, Res]("metrics middleware requires non-nil next executor")
		}

		return routery.ExecutorFunc[Req, Res](func(ctx context.Context, req Req) (Res, error) {
			if hooks.OnStart != nil {
				hooks.OnStart(ctx, name)
			}

			start := time.Now()
			response, executeErr := next.Execute(ctx, req)

			if hooks.OnComplete != nil {
				hooks.OnComplete(ctx, name, time.Since(start), executeErr)
			}

			return response, executeErr
		})
	}
}
