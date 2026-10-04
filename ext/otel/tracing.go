package routeryotel

import (
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/routery"
)

const (
	defaultSpanName = "routery.handle"
	actionAttribute = "routery.action"
)

// TraceResult borrows canonical metadata for an explicit host-owned projection.
// It deliberately excludes the request, payload and resource ownership.
type TraceResult[Kind comparable, Reason comparable] struct {
	Action routery.RouteAction
	Kind   Kind
	Reason Reason
	Match  routery.RouteMatch
	Err    error
}

// AttributeProjection returns host-allowlisted, bounded attributes. It must not
// publish secrets or arbitrary caller values. routery.action is reserved.
type AttributeProjection[Kind comparable, Reason comparable] func(TraceResult[Kind, Reason]) []attribute.KeyValue

// Tracing records one span around each call, not the returned resource lifetime.
// A nil projection publishes only canonical action and bounded error status.
// An empty spanName uses a fixed name; explicit names must be safe host labels.
func Tracing[Req any, Kind comparable, Reason comparable, Payload any](
	tracer trace.Tracer,
	spanName string,
	projection AttributeProjection[Kind, Reason],
) routery.RouteMiddleware[Req, Kind, Reason, Payload] {
	if tracer == nil {
		return func(routery.RouteHandler[Req, Kind, Reason, Payload]) routery.RouteHandler[Req, Kind, Reason, Payload] {
			return func(routery.RouteCall[Req]) (routery.RouteResult[Kind, Reason, Payload], error) {
				return routery.AbortResult[Kind, Reason, Payload](),
					fmt.Errorf("%w: nil tracer", routery.ErrInvalidConfig)
			}
		}
	}

	return func(next routery.RouteHandler[Req, Kind, Reason, Payload]) routery.RouteHandler[Req, Kind, Reason, Payload] {
		if next == nil {
			return func(routery.RouteCall[Req]) (routery.RouteResult[Kind, Reason, Payload], error) {
				return routery.AbortResult[Kind, Reason, Payload](),
					fmt.Errorf("%w: tracing middleware requires non-nil next route handler", routery.ErrInvalidConfig)
			}
		}

		return func(call routery.RouteCall[Req]) (routery.RouteResult[Kind, Reason, Payload], error) {
			name := spanName
			if name == "" {
				name = defaultSpanName
			}

			ctx, span := tracer.Start(call.Context, name)
			defer span.End()

			result, err := next(call.WithContext(ctx))
			result, err = routery.ValidateRouteResult(result, err)
			if result.Match.RouteID == "" && len(result.Match.Path) == 0 {
				result = result.WithMatch(call.Match)
			}
			if err != nil {
				span.SetStatus(codes.Error, "route failed")
			}

			setProjectedAttributes(span, projection, TraceResult[Kind, Reason]{
				Action: result.Action, Kind: result.Kind, Reason: result.Reason,
				Match: result.Match, Err: err,
			})
			span.SetAttributes(attribute.String(actionAttribute, string(result.Action)))
			return result, err
		}
	}
}

func setProjectedAttributes[Kind comparable, Reason comparable](
	span trace.Span,
	projection AttributeProjection[Kind, Reason],
	result TraceResult[Kind, Reason],
) {
	if projection == nil {
		return
	}
	for _, attr := range projection(result) {
		if attr.Key != actionAttribute {
			span.SetAttributes(attr)
		}
	}
}
