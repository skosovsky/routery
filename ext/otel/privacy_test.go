package routeryotel

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/skosovsky/routery"
)

type privacyExporter struct{ spans []sdktrace.ReadOnlySpan }

func (exporter *privacyExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	exporter.spans = append(exporter.spans, spans...)
	return nil
}

func (*privacyExporter) Shutdown(context.Context) error { return nil }

func TestTracingDefaultsDoNotExportCallerSecrets(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "error"}[failed], func(t *testing.T) {
			checkDefaultTracePrivacy(t, failed)
		})
	}
}

func checkDefaultTracePrivacy(t *testing.T, failed bool) {
	t.Helper()
	// Arrange: every caller-controlled field carries synthetic secret/high-cardinality data.
	const secret = "synthetic-credential-prompt-state-unique-987"
	exporter := &privacyExporter{}
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })
	leaf := func(routery.RouteCall[string]) (routery.BasicRouteResult[string], error) {
		result := routery.Handled(routery.BasicKind(secret), routery.BasicReason(secret), secret)
		result.Match = routery.RouteMatch{
			RouteID:           routery.RouteID(secret),
			Path:              []routery.RouteID{routery.RouteID(secret)},
			Key:               secret,
			Prefix:            secret,
			Remainder:         secret,
			Kind:              routery.MatchKind(secret),
			DecisionReason:    secret,
			HasDecisionReason: true,
		}
		if failed {
			return result, errors.New(secret)
		}
		return result, nil
	}
	call := routery.NewRouteCall(t.Context(), secret)
	call.Match.RouteID, call.Match.Path = routery.RouteID(secret), []routery.RouteID{routery.RouteID(secret)}
	// Act.
	result, err := Tracing[string, routery.BasicKind, routery.BasicReason, string](
		provider.Tracer("test"),
		"",
		nil,
	)(
		leaf,
	)(
		call,
	)
	// Assert: only a bounded canonical action is automatic; result itself is unchanged.
	if len(exporter.spans) != 1 || (err != nil) != failed || result.Payload != secret {
		t.Fatal("trace changed canonical result or did not record one span")
	}
	span := exporter.spans[0]
	attributes := span.Attributes()
	if span.Name() != defaultSpanName || len(attributes) != 1 || len(span.Events()) != 0 {
		t.Fatal("default tracing published caller fields/error or high-cardinality span name")
	}
	if string(attributes[0].Key) != actionAttribute || attributes[0].Value.AsString() != string(result.Action) {
		t.Fatal("default tracing did not publish only canonical action")
	}
	wantDescription := ""
	if failed {
		wantDescription = "route failed"
	}
	if span.Status().Description != wantDescription {
		t.Fatal("default tracing published raw error description")
	}
}

func TestTracingExplicitProjectionUsesCanonicalMetadata(t *testing.T) {
	// Arrange: explicit host projection maps private values to a bounded category.
	exporter := &privacyExporter{}
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })
	failure := errors.New("private provider detail")
	life := routery.NewLifetime(func() error { return nil })
	t.Cleanup(func() { _ = life.Close() })
	leaf := func(routery.RouteCall[int]) (routery.BasicRouteResult[string], error) {
		result := routery.Handled(
			routery.BasicKind("private-kind"),
			routery.BasicReason("private-reason"),
			"private-payload",
		)
		result.Match.RouteID, result.Lifetime = "private-route", life
		return result, failure
	}
	var calls int
	projection := func(meta TraceResult[routery.BasicKind, routery.BasicReason]) []attribute.KeyValue {
		calls++
		if meta.Action != routery.ActionAbort || !errors.Is(meta.Err, failure) ||
			meta.Match.RouteID != "private-route" {
			t.Fatal("projection received noncanonical metadata")
		}
		if meta.Kind != "private-kind" || meta.Reason != "private-reason" {
			t.Fatal("projection lost caller metadata")
		}
		return []attribute.KeyValue{attribute.String("host.failure.class", "provider"),
			attribute.String("routery.action", "private-attempted-override")}
	}
	// Act.
	result, err := Tracing[int, routery.BasicKind, routery.BasicReason, string](
		provider.Tracer("test"),
		"safe-operation",
		projection,
	)(
		leaf,
	)(
		routery.NewRouteCall(t.Context(), 1),
	)
	// Assert: explicit projection preserves result ownership and cannot replace action.
	if calls != 1 || !errors.Is(err, failure) || result.Lifetime != life || len(exporter.spans) != 1 {
		t.Fatal("projection changed result or span lifecycle")
	}
	span := exporter.spans[0]
	if span.Name() != "safe-operation" || len(span.Attributes()) != 2 || len(span.Events()) != 0 {
		t.Fatal("unexpected projection output")
	}
	values := make(map[attribute.Key]string)
	for _, attr := range span.Attributes() {
		values[attr.Key] = attr.Value.AsString()
	}
	if values["host.failure.class"] != "provider" || values["routery.action"] != string(routery.ActionAbort) {
		t.Fatal("projection failed safe allowlist or reserved action protection")
	}
}
