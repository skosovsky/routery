package observability

import (
	"context"
	"time"

	"github.com/skosovsky/routery"
)

// ResultMeta is a serializable summary of a route outcome for metrics callbacks.
type ResultMeta[Kind comparable, Reason comparable] struct {
	Action routery.RouteAction
	Kind   Kind
	Reason Reason
	Match  routery.RouteMatch
}

// MetricsHooks defines callbacks for metrics collection.
type MetricsHooks[Kind comparable, Reason comparable, Payload any] struct {
	OnStart     func(ctx context.Context, name string, match routery.RouteMatch)
	OnComplete  func(ctx context.Context, name string, duration time.Duration, result ResultMeta[Kind, Reason], payloadMeta PayloadMeta, err error)
	PayloadMeta PayloadMetaFunc[Kind, Reason, Payload]
}

// Metrics emits start and completion callbacks around execution.
func Metrics[Req any, Kind comparable, Reason comparable, Payload any](
	name string,
	hooks MetricsHooks[Kind, Reason, Payload],
) routery.RouteMiddleware[Req, Kind, Reason, Payload] {
	if hooks.OnStart == nil && hooks.OnComplete == nil {
		return func(
			next routery.RouteHandler[Req, Kind, Reason, Payload],
		) routery.RouteHandler[Req, Kind, Reason, Payload] {
			if next == nil {
				return invalidRouteHandler[Req, Kind, Reason, Payload](
					"metrics middleware requires non-nil next route handler",
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
				"metrics middleware requires non-nil next route handler",
			)
		}

		return func(call routery.RouteCall[Req]) (routery.RouteResult[Kind, Reason, Payload], error) {
			if hooks.OnStart != nil {
				hooks.OnStart(call.Context, name, call.Match)
			}

			start := time.Now()
			result, handleErr := next(call)
			if handleErr != nil {
				result = routery.AbortResult[Kind, Reason, Payload]().WithMatch(call.Match)
			} else if result.Match.RouteID == "" && len(result.Match.Path) == 0 {
				result = result.WithMatch(call.Match)
			}

			if hooks.OnComplete != nil {
				meta := resolvePayloadMeta(result, hooks.PayloadMeta)
				resultMeta := ResultMeta[Kind, Reason]{
					Action: result.Action,
					Kind:   result.Kind,
					Reason: result.Reason,
					Match:  result.Match,
				}
				hooks.OnComplete(call.Context, name, time.Since(start), resultMeta, meta, handleErr)
			}

			return result, handleErr
		}
	}
}
