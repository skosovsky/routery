package routeryotel

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/skosovsky/routery"
)

func TestTracingRecordsSpanAndStatus(t *testing.T) {
	t.Parallel()

	exporter := &spyExporter{}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	tracer := tp.Tracer("test")

	base := routery.ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		return 0, errors.New("boom")
	})

	executor := routery.Apply(
		base,
		Tracing[int, int](tracer, "op"),
	)

	_, err := executor.Execute(context.Background(), 0)
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
}

func TestTracingSuccess(t *testing.T) {
	t.Parallel()

	exporter := &spyExporter{}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	tracer := tp.Tracer("test")

	base := routery.ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		return 42, nil
	})

	executor := routery.Apply(base, Tracing[int, int](tracer, "ok"))

	res, err := executor.Execute(context.Background(), 0)
	if err != nil || res != 42 {
		t.Fatalf("res=%d err=%v", res, err)
	}
	if len(exporter.spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(exporter.spans))
	}
	if exporter.spans[0].errored {
		t.Fatal("did not expect error status")
	}
}

func TestTracingNilTracer(t *testing.T) {
	t.Parallel()

	base := routery.ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		return 0, nil
	})
	_, err := Tracing[int, int](nil, "x")(base).Execute(context.Background(), 0)
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestTracingNilNext(t *testing.T) {
	t.Parallel()

	exporter := &spyExporter{}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	tracer := tp.Tracer("test")

	_, err := Tracing[int, int](tracer, "x")(nil).Execute(context.Background(), 0)
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestTracingDefaultSpanName(t *testing.T) {
	t.Parallel()

	exporter := &spyExporter{}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	tracer := tp.Tracer("test")

	base := routery.ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		return 0, nil
	})
	_, _ = routery.Apply(base, Tracing[int, int](tracer, "")).Execute(context.Background(), 0)
	if len(exporter.spans) != 1 || exporter.spans[0].name != "routery.execute" {
		t.Fatalf("unexpected span: %+v", exporter.spans)
	}
}

type spyExporter struct {
	spans []spanSnapshot
}

type spanSnapshot struct {
	name    string
	errored bool
}

func (s *spyExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	for _, sp := range spans {
		status := sp.Status()
		s.spans = append(s.spans, spanSnapshot{
			name:    sp.Name(),
			errored: status.Code == codes.Error,
		})
	}
	return nil
}

func (s *spyExporter) Shutdown(context.Context) error { return nil }
