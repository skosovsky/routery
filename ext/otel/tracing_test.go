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

	exporter := &spyExporter{}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	tracer := tp.Tracer("test")

	base := routery.FromFunc(func(context.Context, int) (int, error) {
		return 0, errors.New("boom")
	})

	handler := routery.ApplyRoute(base, Tracing[int, int](tracer, "op"))

	_, err := routery.InvokeRouteHandler(context.Background(), 0, handler)
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

func TestTracingSuccess(t *testing.T) {
	t.Parallel()

	exporter := &spyExporter{}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	tracer := tp.Tracer("test")

	base := routery.FromFunc(func(context.Context, int) (int, error) {
		return 42, nil
	})

	handler := routery.ApplyRoute(base, Tracing[int, int](tracer, "ok"))

	outcome, err := routery.InvokeRouteHandler(context.Background(), 0, handler)
	if err != nil || !outcome.HasPayload || outcome.Payload != 42 {
		t.Fatalf("payload=%d err=%v", outcome.Payload, err)
	}
	if len(exporter.spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(exporter.spans))
	}
	if exporter.spans[0].errored {
		t.Fatal("did not expect error status")
	}
	if exporter.spans[0].action != string(routery.ActionStop) {
		t.Fatalf("got action attr %q; want stop", exporter.spans[0].action)
	}
}

func TestTracingRecordsActionNext(t *testing.T) {
	t.Parallel()

	exporter := &spyExporter{}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	tracer := tp.Tracer("test")

	base := func(_ context.Context, _ int, rec routery.ResultRecorder[int]) error {
		rec.Next("delegate")
		return nil
	}

	handler := routery.ApplyRoute(base, Tracing[int, int](tracer, "next"))

	outcome, err := routery.InvokeRouteHandler(context.Background(), 0, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Action != routery.ActionNext {
		t.Fatalf("got outcome action %q; want next", outcome.Action)
	}
	if len(exporter.spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(exporter.spans))
	}
	if exporter.spans[0].action != string(routery.ActionNext) {
		t.Fatalf("got action attr %q; want next", exporter.spans[0].action)
	}
	if exporter.spans[0].reasonCode != "delegate" {
		t.Fatalf("got reason attr %q; want delegate", exporter.spans[0].reasonCode)
	}
}

func TestTracingNilTracer(t *testing.T) {
	t.Parallel()

	base := routery.FromFunc(func(context.Context, int) (int, error) {
		return 0, nil
	})
	handler := routery.ApplyRoute(base, Tracing[int, int](nil, "x"))
	_, err := routery.InvokeRouteHandler(context.Background(), 0, handler)
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestTracingNilNext(t *testing.T) {
	t.Parallel()

	exporter := &spyExporter{}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	tracer := tp.Tracer("test")

	handler := Tracing[int, int](tracer, "x")(nil)
	_, err := routery.InvokeRouteHandler(context.Background(), 0, handler)
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestTracingDefaultSpanName(t *testing.T) {
	t.Parallel()

	exporter := &spyExporter{}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	tracer := tp.Tracer("test")

	base := routery.FromFunc(func(context.Context, int) (int, error) {
		return 0, nil
	})
	handler := routery.ApplyRoute(base, Tracing[int, int](tracer, ""))
	_, _ = routery.InvokeRouteHandler(context.Background(), 0, handler)
	if len(exporter.spans) != 1 || exporter.spans[0].name != "routery.handle" {
		t.Fatalf("unexpected span: %+v", exporter.spans)
	}
}

type spyExporter struct {
	spans []spanSnapshot
}

type spanSnapshot struct {
	name       string
	errored    bool
	action     string
	reasonCode string
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
			case "routery.reason_code":
				snapshot.reasonCode = attrValueString(attr)
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
