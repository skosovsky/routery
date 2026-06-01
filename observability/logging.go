package observability

import (
	"context"
	"fmt"
	"time"

	"github.com/skosovsky/routery"
)

// Event describes one handler invocation.
type Event[Req any, Res any] struct {
	Name        string
	StartTime   time.Time
	Duration    time.Duration
	Request     Req
	Result      routery.RouteResult[Res]
	PayloadMeta PayloadMeta
	Err         error
}

// EventHandler handles events produced by Logging middleware.
type EventHandler[Req any, Res any] func(ctx context.Context, event Event[Req, Res])

// Logging emits one event after each wrapped handler call.
//
// Optional payloadMeta computes serializable payload metadata for telemetry.
// When nil, [DefaultPayloadMeta] is used.
func Logging[Req any, Res any](
	name string,
	handler EventHandler[Req, Res],
	payloadMeta PayloadMetaFunc[Res],
) routery.HandlerMiddleware[Req, Res] {
	if handler == nil {
		return func(next routery.Handler[Req, Res]) routery.Handler[Req, Res] {
			if next == nil {
				return invalidHandler[Req, Res]("logging middleware requires non-nil next handler")
			}
			return next
		}
	}

	return func(next routery.Handler[Req, Res]) routery.Handler[Req, Res] {
		if next == nil {
			return invalidHandler[Req, Res]("logging middleware requires non-nil next handler")
		}

		return routery.HandlerFunc[Req, Res](func(ctx context.Context, req Req) (routery.RouteResult[Res], error) {
			start := time.Now()
			result, handleErr := next.Handle(ctx, req)
			handler(ctx, Event[Req, Res]{
				Name:        name,
				StartTime:   start,
				Duration:    time.Since(start),
				Request:     req,
				Result:      result,
				PayloadMeta: resolvePayloadMeta(ctx, result, payloadMeta),
				Err:         handleErr,
			})
			return result, handleErr
		})
	}
}

func invalidHandler[Req any, Res any](detail string) routery.Handler[Req, Res] {
	err := fmt.Errorf("%w: %s", routery.ErrInvalidConfig, detail)

	return routery.HandlerFunc[Req, Res](func(context.Context, Req) (routery.RouteResult[Res], error) {
		return routery.RouteResult[Res]{}, err
	})
}
