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

	base := routery.ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		return 42, nil
	})

	executor := routery.Apply(base, routeryotel.Tracing[int, int](tracer, "work"))
	res, err := executor.Execute(context.Background(), 0)
	fmt.Println(res, err == nil)
	// Output: 42 true
}

type discardExporter struct{}

func (discardExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error { return nil }
func (discardExporter) Shutdown(context.Context) error                             { return nil }
