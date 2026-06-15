package observability

import (
	"context"
	"fmt"
	"time"

	"github.com/skosovsky/routery"
)

// Outcome is a serializable summary of a route handler invocation for logging.
type Outcome struct {
	Action      routery.RouteAction
	ReasonCode  string
	PayloadMeta PayloadMeta
}

// Event describes one route handler invocation.
type Event[Req any, Res any] struct {
	Name      string
	StartTime time.Time
	Duration  time.Duration
	Request   Req
	Outcome   Outcome
	Err       error
}

// EventHandler handles events produced by Logging middleware.
type EventHandler[Req any, Res any] func(ctx context.Context, event Event[Req, Res])

// Logging emits one event after each wrapped route handler call.
func Logging[Req any, Res any](
	name string,
	handler EventHandler[Req, Res],
	payloadMeta PayloadMetaFunc[Res],
) routery.RouteMiddleware[Req, Res] {
	if handler == nil {
		return func(next routery.RouteHandler[Req, Res]) routery.RouteHandler[Req, Res] {
			if next == nil {
				return invalidRouteHandler[Req, Res]("logging middleware requires non-nil next route handler")
			}
			return next
		}
	}

	return func(next routery.RouteHandler[Req, Res]) routery.RouteHandler[Req, Res] {
		if next == nil {
			return invalidRouteHandler[Req, Res]("logging middleware requires non-nil next route handler")
		}

		return func(ctx context.Context, req Req, rec routery.ResultRecorder[Res]) error {
			start := time.Now()
			handleErr := next(ctx, req, rec)
			action := rec.Action()
			if handleErr != nil {
				action = routery.ActionAbort
			}
			handler(ctx, Event[Req, Res]{
				Name:      name,
				StartTime: start,
				Duration:  time.Since(start),
				Request:   req,
				Outcome: Outcome{
					Action:      action,
					ReasonCode:  rec.ReasonCode(),
					PayloadMeta: resolvePayloadMeta(ctx, rec, payloadMeta),
				},
				Err: handleErr,
			})
			return handleErr
		}
	}
}

func invalidRouteHandler[Req any, Res any](detail string) routery.RouteHandler[Req, Res] {
	err := fmt.Errorf("%w: %s", routery.ErrInvalidConfig, detail)

	return func(context.Context, Req, routery.ResultRecorder[Res]) error {
		return err
	}
}
