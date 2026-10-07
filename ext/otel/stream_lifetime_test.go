package routeryotel_test

import (
	"context"
	"iter"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/skosovsky/routery"
	routeryotel "github.com/skosovsky/routery/ext/otel"
	"github.com/skosovsky/routery/stream"
)

func TestLazyStreamSpanEndsAtHandlerReturn(t *testing.T) {
	// Arrange.
	exporter := &lifecycleExporter{}
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	events := iter.Seq2[int, error](func(yield func(int, error) bool) { yield(1, nil) })
	owner, err := stream.New(events, func() error { cancel(); return nil }, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	base := func(routery.RouteCall[int]) (routery.BasicRouteResult[*stream.Owner[int]], error) {
		result := routery.BasicHandled(owner)
		result.Lifetime = owner.Lifetime()
		return result, nil
	}
	handler := routeryotel.Tracing[int, routery.BasicKind, routery.BasicReason, *stream.Owner[int]](
		provider.Tracer("stream"),
		"invocation",
		nil,
	)(
		base,
	)
	// Act.
	result, err := handler(routery.NewRouteCall(ctx, 0))
	// Assert: span ended, resource still open, independent of late cleanup.
	if err != nil || len(exporter.snapshot()) != 1 || ctx.Err() != nil {
		t.Fatal(err)
	}
	select {
	case <-owner.Done():
		t.Fatal("owner completed on handler return")
	default:
	}
	for range result.Payload.Events() {
	}
	if err = result.Lifetime.Close(); err != nil || len(exporter.snapshot()) != 1 {
		t.Fatal("span ended twice", err)
	}
}
