package routeryotel

import (
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/routery"
)

// Tracing records one span around each call to the wrapped route handler.
func Tracing[Req any, Kind comparable, Reason comparable, Payload any](
	tracer trace.Tracer,
	spanName string,
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
				name = spanNameFromMatch(call.Match)
			}

			ctx, span := tracer.Start(call.Context, name)
			defer span.End()

			result, err := next(call.WithContext(ctx))
			if err != nil {
				result = routery.AbortResult[Kind, Reason, Payload]().WithMatch(call.Match)
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			} else if result.Match.RouteID == "" && len(result.Match.Path) == 0 {
				result = result.WithMatch(call.Match)
			}

			setResultAttributes(span, result)
			return result, err
		}
	}
}

func spanNameFromMatch(match routery.RouteMatch) string {
	if len(match.Path) == 0 && match.RouteID == "" {
		return "routery.handle"
	}
	if len(match.Path) == 0 {
		return "routery.route." + string(match.RouteID)
	}

	return "routery.route." + matchPath(match.Path)
}

func matchPath(path []routery.RouteID) string {
	if len(path) == 0 {
		return ""
	}

	parts := make([]string, len(path))
	for index, routeID := range path {
		parts[index] = string(routeID)
	}

	return strings.Join(parts, ".")
}

func setResultAttributes[Kind comparable, Reason comparable, Payload any](
	span trace.Span,
	result routery.RouteResult[Kind, Reason, Payload],
) {
	span.SetAttributes(
		attribute.String("routery.action", string(result.Action)),
		attribute.String("routery.kind", fmt.Sprint(result.Kind)),
		attribute.String("routery.reason", fmt.Sprint(result.Reason)),
		attribute.String("routery.route.id", string(result.Match.RouteID)),
		attribute.String("routery.route.path", matchPath(result.Match.Path)),
		attribute.String("routery.match.kind", string(result.Match.Kind)),
		attribute.Int("routery.route.priority", result.Match.Priority),
		attribute.Int("routery.route.depth", result.Match.Depth),
	)
	if result.Match.Key != "" {
		span.SetAttributes(attribute.String("routery.match.key", result.Match.Key))
	}
	if result.Match.Prefix != "" {
		span.SetAttributes(attribute.String("routery.match.prefix", result.Match.Prefix))
	}
	if result.Match.Remainder != "" {
		span.SetAttributes(attribute.String("routery.match.remainder", result.Match.Remainder))
	}
}
