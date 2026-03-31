package observability

import (
	"context"
	"fmt"
	"time"

	"github.com/skosovsky/routery"
)

// Event describes one executor invocation.
type Event[Req any, Res any] struct {
	Name      string
	StartTime time.Time
	Duration  time.Duration
	Request   Req
	Response  Res
	Err       error
}

// EventHandler handles events produced by Logging middleware.
type EventHandler[Req any, Res any] func(ctx context.Context, event Event[Req, Res])

// Logging emits one event after each wrapped executor call.
func Logging[Req any, Res any](name string, handler EventHandler[Req, Res]) routery.Middleware[Req, Res] {
	if handler == nil {
		return func(next routery.Executor[Req, Res]) routery.Executor[Req, Res] {
			if next == nil {
				return invalidExecutor[Req, Res]("logging middleware requires non-nil next executor")
			}
			return next
		}
	}

	return func(next routery.Executor[Req, Res]) routery.Executor[Req, Res] {
		if next == nil {
			return invalidExecutor[Req, Res]("logging middleware requires non-nil next executor")
		}

		return routery.ExecutorFunc[Req, Res](func(ctx context.Context, req Req) (Res, error) {
			start := time.Now()
			response, executeErr := next.Execute(ctx, req)
			handler(ctx, Event[Req, Res]{
				Name:      name,
				StartTime: start,
				Duration:  time.Since(start),
				Request:   req,
				Response:  response,
				Err:       executeErr,
			})
			return response, executeErr
		})
	}
}

func invalidExecutor[Req any, Res any](detail string) routery.Executor[Req, Res] {
	err := fmt.Errorf("%w: %s", routery.ErrInvalidConfig, detail)

	return routery.ExecutorFunc[Req, Res](func(context.Context, Req) (Res, error) {
		var zero Res
		return zero, err
	})
}
