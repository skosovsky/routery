package routeryotel

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/skosovsky/routery"
)

func TestTracingRecordsSpanAndStatus(t *testing.T) {
	t.Parallel()

	// Arrange.
	exporter := &spyExporter{}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	tracer := tp.Tracer("test")
	base := routery.FromFunc(func(context.Context, int) (int, error) {
		return 0, errors.New("boom")
	})
	handler := routery.ApplyRoute(
		base,
		Tracing[int, routery.BasicKind, routery.BasicReason, int](tracer, "op"),
	)

	// Act.
	_, err := routery.InvokeRouteHandler(context.Background(), 0, handler)

	// Assert.
	if err == nil {
		t.Fatal("expected error")
	}
	if len(exporter.spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(exporter.spans))
	}
	if exporter.spans[0].name != "op" {
		t.Fatalf("unexpected span name: %q", exporter.spans[0].name)
	}
	if !exporter.spans[0].errored {
		t.Fatal("expected span marked as error")
	}
	if exporter.spans[0].action != string(routery.ActionAbort) {
		t.Fatalf("got action attr %q; want abort", exporter.spans[0].action)
	}
}

func TestTracingSuccessRecordsTypedAttributes(t *testing.T) {
	t.Parallel()

	// Arrange.
	exporter := &spyExporter{}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	tracer := tp.Tracer("test")
	base := func(routery.RouteCall[int]) (routery.BasicRouteResult[int], error) {
		return routery.BasicHandled(42), nil
	}
	handler := routery.ApplyRoute(
		base,
		Tracing[int, routery.BasicKind, routery.BasicReason, int](tracer, ""),
	)
	call := routery.NewRouteCall(context.Background(), 0)
	call.Match = routery.RouteMatch{
		RouteID:           "primary",
		Path:              []routery.RouteID{"primary"},
		Priority:          5,
		Depth:             0,
		Kind:              routery.MatchKindExact,
		Key:               "k",
		Prefix:            "",
		Remainder:         "",
		DecisionReason:    nil,
		HasDecisionReason: false,
	}

	// Act.
	outcome, err := handler(call)

	// Assert.
	if err != nil || !outcome.HasPayload || outcome.Payload != 42 {
		t.Fatalf("payload=%d err=%v", outcome.Payload, err)
	}
	if len(exporter.spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(exporter.spans))
	}
	if exporter.spans[0].errored {
		t.Fatal("did not expect error status")
	}
	if exporter.spans[0].name != "routery.route.primary" {
		t.Fatalf("span name = %q, want routery.route.primary", exporter.spans[0].name)
	}
	if exporter.spans[0].action != string(routery.ActionStop) {
		t.Fatalf("got action attr %q; want stop", exporter.spans[0].action)
	}
	if exporter.spans[0].reason != string(routery.BasicReasonNone) {
		t.Fatalf("got reason attr %q; want empty", exporter.spans[0].reason)
	}
	if exporter.spans[0].routeID != "primary" || exporter.spans[0].matchKind != string(routery.MatchKindExact) {
		t.Fatalf("span route attrs = %#v", exporter.spans[0])
	}
}

func TestTracingRecordsActionNext(t *testing.T) {
	t.Parallel()

	// Arrange.
	exporter := &spyExporter{}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	tracer := tp.Tracer("test")
	base := func(routery.RouteCall[int]) (routery.BasicRouteResult[int], error) {
		return routery.BasicNext[int](routery.BasicReason("delegate")), nil
	}
	handler := routery.ApplyRoute(
		base,
		Tracing[int, routery.BasicKind, routery.BasicReason, int](tracer, "next"),
	)

	// Act.
	outcome, err := routery.InvokeRouteHandler(context.Background(), 0, handler)

	// Assert.
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Action != routery.ActionNext {
		t.Fatalf("got outcome action %q; want next", outcome.Action)
	}
	if exporter.spans[0].action != string(routery.ActionNext) {
		t.Fatalf("got action attr %q; want next", exporter.spans[0].action)
	}
	if exporter.spans[0].reason != "delegate" {
		t.Fatalf("got reason attr %q; want delegate", exporter.spans[0].reason)
	}
}

func TestTracingNilTracer(t *testing.T) {
	t.Parallel()

	// Arrange.
	base := routery.FromFunc(func(context.Context, int) (int, error) {
		return 0, nil
	})
	handler := routery.ApplyRoute(
		base,
		Tracing[int, routery.BasicKind, routery.BasicReason, int](nil, "x"),
	)

	// Act.
	_, err := routery.InvokeRouteHandler(context.Background(), 0, handler)

	// Assert.
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestTracingNilNext(t *testing.T) {
	t.Parallel()

	// Arrange.
	exporter := &spyExporter{}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	tracer := tp.Tracer("test")
	handler := Tracing[int, routery.BasicKind, routery.BasicReason, int](tracer, "x")(nil)

	// Act.
	_, err := routery.InvokeRouteHandler(context.Background(), 0, handler)

	// Assert.
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

type spyExporter struct {
	spans []spanSnapshot
}

type spanSnapshot struct {
	name      string
	errored   bool
	action    string
	reason    string
	routeID   string
	matchKind string
}

func (s *spyExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	for _, sp := range spans {
		status := sp.Status()
		snapshot := spanSnapshot{
			name:    sp.Name(),
			errored: status.Code == codes.Error,
		}
		for _, attr := range sp.Attributes() {
			switch string(attr.Key) {
			case "routery.action":
				snapshot.action = attrValueString(attr)
			case "routery.reason":
				snapshot.reason = attrValueString(attr)
			case "routery.route.id":
				snapshot.routeID = attrValueString(attr)
			case "routery.match.kind":
				snapshot.matchKind = attrValueString(attr)
			}
		}
		s.spans = append(s.spans, snapshot)
	}
	return nil
}

func attrValueString(attr attribute.KeyValue) string {
	switch attr.Value.Type() {
	case attribute.STRING:
		return attr.Value.AsString()
	default:
		return attr.Value.AsString()
	}
}

func (s *spyExporter) Shutdown(context.Context) error { return nil }
