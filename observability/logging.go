package observability

import (
	"context"
	"fmt"
	"time"

	"github.com/skosovsky/routery"
)

// Outcome is a serializable summary of a route handler invocation for logging.
type Outcome[Kind comparable, Reason comparable] struct {
	Action routery.RouteAction
	Kind   Kind
	Reason Reason
	Match  routery.RouteMatch
}

// Event describes one route handler invocation.
type Event[Req any, Kind comparable, Reason comparable, Payload any] struct {
	Name        string
	StartTime   time.Time
	Duration    time.Duration
	Request     Req
	Outcome     Outcome[Kind, Reason]
	PayloadMeta PayloadMeta
	Err         error
}

// EventHandler handles events produced by Logging middleware.
type EventHandler[Req any, Kind comparable, Reason comparable, Payload any] func(
	ctx context.Context,
	event Event[Req, Kind, Reason, Payload],
)

// Logging emits one event after each wrapped route handler call.
func Logging[Req any, Kind comparable, Reason comparable, Payload any](
	name string,
	handler EventHandler[Req, Kind, Reason, Payload],
	payloadMeta PayloadMetaFunc[Kind, Reason, Payload],
) routery.RouteMiddleware[Req, Kind, Reason, Payload] {
	if handler == nil {
		return func(
			next routery.RouteHandler[Req, Kind, Reason, Payload],
		) routery.RouteHandler[Req, Kind, Reason, Payload] {
			if next == nil {
				return invalidRouteHandler[Req, Kind, Reason, Payload](
					"logging middleware requires non-nil next route handler",
				)
			}
			return next
		}
	}

	return func(
		next routery.RouteHandler[Req, Kind, Reason, Payload],
	) routery.RouteHandler[Req, Kind, Reason, Payload] {
		if next == nil {
			return invalidRouteHandler[Req, Kind, Reason, Payload](
				"logging middleware requires non-nil next route handler",
			)
		}

		return func(call routery.RouteCall[Req]) (routery.RouteResult[Kind, Reason, Payload], error) {
			start := time.Now()
			result, handleErr := next(call)
			if handleErr != nil {
				result = routery.AbortResult[Kind, Reason, Payload]().WithMatch(call.Match)
			} else if result.Match.RouteID == "" && len(result.Match.Path) == 0 {
				result = result.WithMatch(call.Match)
			}
			handler(call.Context, Event[Req, Kind, Reason, Payload]{
				Name:      name,
				StartTime: start,
				Duration:  time.Since(start),
				Request:   call.Request,
				Outcome: Outcome[Kind, Reason]{
					Action: result.Action,
					Kind:   result.Kind,
					Reason: result.Reason,
					Match:  result.Match,
				},
				PayloadMeta: resolvePayloadMeta(result, payloadMeta),
				Err:         handleErr,
			})
			return result, handleErr
		}
	}
}

func invalidRouteHandler[Req any, Kind comparable, Reason comparable, Payload any](
	detail string,
) routery.RouteHandler[Req, Kind, Reason, Payload] {
	err := fmt.Errorf("%w: %s", routery.ErrInvalidConfig, detail)

	return func(routery.RouteCall[Req]) (routery.RouteResult[Kind, Reason, Payload], error) {
		return routery.AbortResult[Kind, Reason, Payload](), err
	}
}
