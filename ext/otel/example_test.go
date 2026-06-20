package routeryotel_test

import (
	"context"
	"fmt"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/skosovsky/routery"
	routeryotel "github.com/skosovsky/routery/ext/otel"
)

func ExampleTracing() {
	exporter := &discardExporter{}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	tracer := tp.Tracer("example")

	base := routery.FromFunc(func(context.Context, int) (int, error) {
		return 42, nil
	})

	handler := routery.ApplyRoute(
		base,
		routeryotel.Tracing[int, routery.BasicKind, routery.BasicReason, int](tracer, "work"),
	)
	outcome, err := routery.InvokeRouteHandler(context.Background(), 0, handler)
	fmt.Println(outcome.Payload, err == nil)
	// Output: 42 true
}

type discardExporter struct{}

func (discardExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error { return nil }
func (discardExporter) Shutdown(context.Context) error                             { return nil }
