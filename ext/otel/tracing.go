package routeryotel

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/routery"
)

// Tracing records one span around each call to the wrapped handler.
//
// If tracer is nil, the returned middleware always fails with [routery.ErrInvalidConfig].
// If spanName is empty, "routery.handle" is used.
//
// Request attributes are not inferred from generic Req; wrap this middleware or
// start child spans in application code when you need rich attributes.
func Tracing[Req any, Res any](tracer trace.Tracer, spanName string) routery.HandlerMiddleware[Req, Res] {
	if tracer == nil {
		return func(routery.Handler[Req, Res]) routery.Handler[Req, Res] {
			return routery.HandlerFunc[Req, Res](func(context.Context, Req) (routery.RouteResult[Res], error) {
				return routery.RouteResult[Res]{}, fmt.Errorf("%w: nil OpenTelemetry tracer", routery.ErrInvalidConfig)
			})
		}
	}

	name := spanName
	if name == "" {
		name = "routery.handle"
	}

	return func(next routery.Handler[Req, Res]) routery.Handler[Req, Res] {
		if next == nil {
			return routery.HandlerFunc[Req, Res](func(context.Context, Req) (routery.RouteResult[Res], error) {
				return routery.RouteResult[Res]{}, fmt.Errorf(
					"%w: tracing middleware requires non-nil next handler",
					routery.ErrInvalidConfig,
				)
			})
		}

		return routery.HandlerFunc[Req, Res](func(ctx context.Context, req Req) (routery.RouteResult[Res], error) {
			ctx, span := tracer.Start(ctx, name)
			defer span.End()

			result, err := next.Handle(ctx, req)
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			} else {
				span.SetAttributes(
					attribute.String("routery.status", string(result.Status)),
				)
				if result.ReasonCode != "" {
					span.SetAttributes(attribute.String("routery.reason_code", result.ReasonCode))
				}
			}

			return result, err
		})
	}
}
