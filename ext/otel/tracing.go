package routeryotel

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/routery"
)

// Tracing records one span around each call to the wrapped executor.
//
// If tracer is nil, the returned middleware always fails with [routery.ErrInvalidConfig].
// If spanName is empty, "routery.execute" is used.
//
// Request attributes are not inferred from generic Req; wrap this middleware or
// start child spans in application code when you need rich attributes.
func Tracing[Req any, Res any](tracer trace.Tracer, spanName string) routery.Middleware[Req, Res] {
	if tracer == nil {
		return func(routery.Executor[Req, Res]) routery.Executor[Req, Res] {
			return routery.ExecutorFunc[Req, Res](func(context.Context, Req) (Res, error) {
				var zero Res
				return zero, fmt.Errorf("%w: nil OpenTelemetry tracer", routery.ErrInvalidConfig)
			})
		}
	}

	name := spanName
	if name == "" {
		name = "routery.execute"
	}

	return func(next routery.Executor[Req, Res]) routery.Executor[Req, Res] {
		if next == nil {
			return routery.ExecutorFunc[Req, Res](func(context.Context, Req) (Res, error) {
				var zero Res
				return zero, fmt.Errorf(
					"%w: tracing middleware requires non-nil next executor",
					routery.ErrInvalidConfig,
				)
			})
		}

		return routery.ExecutorFunc[Req, Res](func(ctx context.Context, req Req) (Res, error) {
			ctx, span := tracer.Start(ctx, name)
			defer span.End()

			res, err := next.Execute(ctx, req)
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			}
			return res, err
		})
	}
}
