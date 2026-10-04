package routeryotel

import (
	"errors"
	"sync/atomic"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/skosovsky/routery"
)

func TestTracingRetainsOwnedPartialAndValidatesActions(t *testing.T) {
	for _, scenario := range []string{"provider error", "abort without error", "unknown action"} {
		t.Run(scenario, func(t *testing.T) { checkTracingPartial(t, scenario) })
	}
}

func checkTracingPartial(t *testing.T, scenario string) {
	t.Helper()
	// Arrange.
	exporter := &spyExporter{}
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })
	failure, cleanupErr := errors.New("provider failure"), errors.New("cleanup failure")
	var closes, hooks atomic.Int32
	life := routery.NewLifetime(func() error { closes.Add(1); return cleanupErr })
	life.OnClose(func() { hooks.Add(1) })
	t.Cleanup(func() { _ = life.Close() })
	leaf := func(routery.RouteCall[int]) (routery.BasicRouteResult[string], error) {
		result := routery.BasicHandled("private partial")
		result.Lifetime, result.Match.RouteID = life, "leaf-route"
		switch scenario {
		case "abort without error":
			result.Action = routery.ActionAbort
			return result, nil
		case "unknown action":
			result.Action = "unknown"
			return result, nil
		default:
			return result, failure
		}
	}
	call := routery.NewRouteCall(t.Context(), 42)
	call.Match.RouteID = "caller-route"
	// Act.
	result, err := Tracing[int, routery.BasicKind, routery.BasicReason, string](
		provider.Tracer("test"),
		"owned",
		nil,
	)(
		leaf,
	)(
		call,
	)
	// Assert.
	wantErr := failure
	if scenario != "provider error" {
		wantErr = routery.ErrInvalidConfig
	}
	if !errors.Is(err, wantErr) || result.Lifetime != life || !result.HasPayload {
		t.Fatal("tracing lost canonical error or partial owner")
	}
	if result.Payload != "private partial" || result.Action != routery.ActionAbort ||
		result.Match.RouteID != "leaf-route" || closes.Load() != 0 {
		t.Fatal("tracing discarded or prematurely closed canonical owned partial")
	}
	if len(exporter.spans) != 1 || !exporter.spans[0].errored ||
		exporter.spans[0].action != string(routery.ActionAbort) ||
		exporter.spans[0].routeID != "" {
		t.Fatal("tracing did not record bounded canonical error/action")
	}
	firstErr, secondErr := result.Lifetime.Close(), result.Lifetime.Close()
	if !errors.Is(firstErr, cleanupErr) || !errors.Is(secondErr, cleanupErr) || closes.Load() != 1 ||
		hooks.Load() != 1 {
		t.Fatal("tracing lost exactly-once cleanup/hooks/error")
	}
}
