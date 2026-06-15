package routeryotel

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/routery"
)

// Tracing records one span around each call to the wrapped route handler.
func Tracing[Req any, Res any](tracer trace.Tracer, spanName string) routery.RouteMiddleware[Req, Res] {
	if tracer == nil {
		return func(routery.RouteHandler[Req, Res]) routery.RouteHandler[Req, Res] {
			return func(context.Context, Req, routery.ResultRecorder[Res]) error {
				return fmt.Errorf("%w: nil OpenTelemetry tracer", routery.ErrInvalidConfig)
			}
		}
	}

	name := spanName
	if name == "" {
		name = "routery.handle"
	}

	return func(next routery.RouteHandler[Req, Res]) routery.RouteHandler[Req, Res] {
		if next == nil {
			return func(context.Context, Req, routery.ResultRecorder[Res]) error {
				return fmt.Errorf(
					"%w: tracing middleware requires non-nil next route handler",
					routery.ErrInvalidConfig,
				)
			}
		}

		return func(ctx context.Context, req Req, rec routery.ResultRecorder[Res]) error {
			ctx, span := tracer.Start(ctx, name)
			defer span.End()

			err := next(ctx, req, rec)
			action := rec.Action()
			if err != nil {
				action = routery.ActionAbort
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			}
			span.SetAttributes(attribute.String("routery.action", string(action)))
			if err == nil && rec.ReasonCode() != "" {
				span.SetAttributes(attribute.String("routery.reason_code", rec.ReasonCode()))
			}

			return err
		}
	}
}
