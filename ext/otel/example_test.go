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

	base := routery.HandlerFunc[int, int](func(context.Context, int) (routery.RouteResult[int], error) {
		return routery.Handled(42), nil
	})

	executor := routery.Apply(base, routeryotel.Tracing[int, int](tracer, "work"))
	res, err := executor.Handle(context.Background(), 0)
	fmt.Println(res.Payload, err == nil)
	// Output: 42 true
}

type discardExporter struct{}

func (discardExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error { return nil }
func (discardExporter) Shutdown(context.Context) error                             { return nil }
