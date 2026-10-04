package routeryotel_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/skosovsky/routery"
	routeryotel "github.com/skosovsky/routery/ext/otel"
	"github.com/skosovsky/routery/policy/attempt"
	"github.com/skosovsky/routery/policy/execution"
	"github.com/skosovsky/routery/policy/quota"
)

type lifecycleExporter struct {
	mu    sync.Mutex
	spans []sdktrace.ReadOnlySpan
}

func (exporter *lifecycleExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	exporter.mu.Lock()
	defer exporter.mu.Unlock()
	exporter.spans = append(exporter.spans, spans...)
	return nil
}
func (*lifecycleExporter) Shutdown(context.Context) error { return nil }
func (exporter *lifecycleExporter) snapshot() []sdktrace.ReadOnlySpan {
	exporter.mu.Lock()
	defer exporter.mu.Unlock()
	return slices.Clone(exporter.spans)
}

type correlation struct {
	operation, attempt, selection string
	allowed                       map[string]bool
}

func (ids correlation) attributes() []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, 3)
	for _, field := range []struct{ name, value string }{{"operation", ids.operation}, {"attempt", ids.attempt}, {"selection", ids.selection}} {
		if ids.allowed[field.value] {
			attrs = append(attrs, attribute.String("host."+field.name, field.value))
		}
	}
	return attrs
}

type lifecycleEvidence struct {
	invocationEnded, ownerOpen, permitHeld, cleanupBounded, completed bool
	closes, settlements                                               int
	spans                                                             []sdktrace.ReadOnlySpan
}

// observeLifecycle is application composition. The library does not extend invocation spans.
func observeLifecycle(ids correlation) (lifecycleEvidence, error) {
	return observeLifecycleWithSettlementFailure(ids, nil)
}

func observeLifecycleWithSettlementFailure(ids correlation, firstFailure error) (lifecycleEvidence, error) {
	exporter := &lifecycleExporter{}
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	tracer := provider.Tracer("host-example")
	ctx, lifecycle := tracer.Start(context.Background(), "host.lifecycle", trace.WithAttributes(ids.attributes()...))
	evidence := lifecycleEvidence{}
	base := routery.BasicRouteHandler[string, io.ReadCloser](
		func(routery.RouteCall[string]) (routery.BasicRouteResult[io.ReadCloser], error) {
			body := io.NopCloser(strings.NewReader("synthetic-private-body"))
			result := routery.BasicHandled[io.ReadCloser](body)
			result.Lifetime = routery.NewLifetime(func() error { evidence.closes++; return body.Close() })
			return result, errors.New("synthetic-private-provider-error")
		},
	)
	// No IDs are exported unless the host explicitly allowlists their exact values.
	projection := routeryotel.AttributeProjection[routery.BasicKind, routery.BasicReason](
		func(routeryotel.TraceResult[routery.BasicKind, routery.BasicReason]) []attribute.KeyValue {
			return ids.attributes()
		},
	)
	limited := routery.ApplyRoute(
		base,
		routery.Bulkhead[string, routery.BasicKind, routery.BasicReason, io.ReadCloser](1),
		routeryotel.Tracing[string, routery.BasicKind, routery.BasicReason, io.ReadCloser](
			tracer,
			"provider.invocation",
			projection,
		),
	)
	boundary := execution.Boundary[string, routery.BasicKind, routery.BasicReason, io.ReadCloser]{
		Fresh: func(context.Context, string) error { return nil },
		Admit: func(context.Context, string, attempt.Identity) (execution.Admission, error) {
			return execution.Admission{
				Status: quota.Admitted,
				Finish: func(ctx context.Context, event attempt.Event) error {
					_, bounded := ctx.Deadline()
					evidence.cleanupBounded = bounded && ctx.Err() == nil
					evidence.settlements++
					evidence.completed = event.Outcome == attempt.Completed
					if evidence.settlements == 1 {
						return errors.Join(ctx.Err(), firstFailure)
					}
					return ctx.Err()
				},
			}, nil
		},
		CleanupContext: func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), time.Second)
		},
		Dispatch: func(call routery.RouteCall[string], receipt *execution.Receipt) (routery.BasicRouteResult[io.ReadCloser], error) {
			result, err := limited(call)
			event, _, snapshotErr := receipt.Snapshot()
			if snapshotErr != nil {
				return result, errors.Join(err, snapshotErr)
			}
			event.Phase = attempt.StreamOpened
			return result, errors.Join(err, receipt.Record(event))
		},
	}
	coordinator, coordinatorErr := attempt.NewCoordinator(ids.operation, 1)
	if coordinatorErr != nil {
		lifecycle.End()
		return evidence, errors.Join(coordinatorErr, provider.Shutdown(context.Background()))
	}
	result, providerErr := boundary.Run(
		routery.NewRouteCall(ctx, "synthetic-private-request"),
		coordinator,
		attempt.Identity{Operation: ids.operation, Attempt: ids.attempt},
	)
	if result.Receipt == nil || result.Route.Lifetime == nil {
		lifecycle.End()
		return evidence, errors.Join(
			providerErr,
			result.Route.Lifetime.Close(),
			provider.Shutdown(context.Background()),
		)
	}
	evidence.invocationEnded = len(exporter.snapshot()) == 1
	evidence.ownerOpen = evidence.closes == 0 && evidence.settlements == 0
	_, blockedErr := routery.InvokeRouteHandler(ctx, "synthetic-private-request", limited)
	evidence.permitHeld = errors.Is(blockedErr, routery.ErrTooManyRequests)
	lifecycle.AddEvent("stream.open", trace.WithAttributes(attribute.String("host.outcome", "unknown")))
	closeErr := result.Route.Lifetime.Close()        // Application owns cleanup on partial+error.
	_, _, settlementErr := result.Receipt.Snapshot() // Preserve failure before a late Finish replaces it.
	lifecycle.AddEvent(
		"owner.closed",
		trace.WithAttributes(attribute.String("host.settlement", settlementStatus(settlementErr, "pending"))),
	)
	// Late authoritative report; Close alone never proves Completed.
	event := attempt.Event{
		Identity: attempt.Identity{Operation: ids.operation, Attempt: ids.attempt},
		Phase:    attempt.Terminal,
		Outcome:  attempt.Completed,
	}
	recordErr := result.Receipt.Record(event)
	lifecycle.AddEvent(
		"provider.terminal",
		trace.WithAttributes(attribute.String("host.settlement", settlementStatus(recordErr, "completed"))),
	)
	lifecycle.End() // Explicit host lifecycle span, separate from ended invocation.
	duplicateCloseErr := result.Route.Lifetime.Close()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	shutdownErr := provider.Shutdown(shutdownCtx)
	cancel()
	evidence.spans = exporter.snapshot()
	// The fixture's provider error is expected and never exported raw.
	if providerErr == nil {
		return evidence, errors.New("example: partial provider error missing")
	}
	return evidence, errors.Join(closeErr, settlementErr, recordErr, duplicateCloseErr, shutdownErr)
}

func ExampleTracing_lifecycleCorrelation() {
	// Arrange: trusted host IDs explicitly allowlisted for traces, never metric labels.
	ids := correlation{
		operation: "op-safe",
		attempt:   "attempt-safe",
		selection: "selection-safe",
		allowed:   map[string]bool{"op-safe": true, "attempt-safe": true, "selection-safe": true},
	}
	// Act.
	evidence, err := observeLifecycle(ids)
	// Assert: handle return and invocation End precede owner Close and late completion.
	fmt.Println(
		"invocation-ended",
		evidence.invocationEnded,
		"owner-open",
		evidence.ownerOpen,
		"permit-held",
		evidence.permitHeld,
	)
	fmt.Println(
		"closes",
		evidence.closes,
		"settlements",
		evidence.settlements,
		"late-completion",
		evidence.completed,
		"cleanup-bounded",
		evidence.cleanupBounded,
		"spans",
		len(evidence.spans),
		"error",
		err,
	)
	// Output:
	// invocation-ended true owner-open true permit-held true
	// closes 1 settlements 2 late-completion true cleanup-bounded true spans 2 error <nil>
}

func TestLifecycleCorrelationRequiresHostAllowlistAndKeepsOwner(t *testing.T) {
	t.Parallel()
	// Arrange: secret values in request, payload, raw error and every ID. No allowlist.
	ids := correlation{
		operation: "synthetic-private-operation",
		attempt:   "synthetic-private-attempt",
		selection: "synthetic-private-selection",
	}
	// Act.
	evidence, err := observeLifecycle(ids)
	// Assert: telemetry has no ownership side effect or raw/private exported fields.
	if err != nil || !evidence.invocationEnded || !evidence.ownerOpen || !evidence.permitHeld || !evidence.completed ||
		evidence.closes != 1 ||
		evidence.settlements != 2 ||
		len(evidence.spans) != 2 {
		t.Fatal(evidence, err)
	}
	assertNoPrivateTelemetry(t, evidence.spans)
	invocation, lifecycle := evidence.spans[0], evidence.spans[1]
	if invocation.Name() != "provider.invocation" || len(invocation.Events()) != 0 ||
		lifecycle.Name() != "host.lifecycle" ||
		len(lifecycle.Events()) != 3 ||
		lifecycle.EndTime().Before(invocation.EndTime()) {
		t.Fatal("invocation/lifecycle scopes merged")
	}
	if len(invocation.Attributes()) != 1 || len(lifecycle.Attributes()) != 0 {
		t.Fatal("unallowlisted IDs exported")
	}
}

func settlementStatus(err error, success string) string {
	if err != nil {
		return "failed"
	}
	return success
}

func TestLifecycleRetainsFirstSettlementFailure(t *testing.T) {
	t.Parallel()
	// Arrange.
	firstFailure := errors.New("synthetic-private-settlement-failure")
	// Act: late completion succeeds after the first Finish fails.
	evidence, err := observeLifecycleWithSettlementFailure(
		correlation{operation: "op", attempt: "attempt"},
		firstFailure,
	)
	// Assert: later success never hides the earlier failure or repeats cleanup.
	if !errors.Is(err, firstFailure) || !evidence.completed || evidence.settlements != 2 || evidence.closes != 1 {
		t.Fatal(evidence, err)
	}
	for _, span := range evidence.spans {
		for _, event := range span.Events() {
			for _, attr := range event.Attributes {
				if strings.Contains(attr.Value.AsString(), "synthetic-private") {
					t.Fatal(attr)
				}
			}
		}
	}
}

func assertNoPrivateTelemetry(t *testing.T, spans []sdktrace.ReadOnlySpan) {
	t.Helper()
	for _, span := range spans {
		for _, field := range append(span.Attributes(), attribute.String("span.name", span.Name()), attribute.String("span.status", span.Status().Description)) {
			if strings.Contains(field.Value.AsString(), "synthetic-private") {
				t.Fatalf("secret attribute %v", field)
			}
		}
		for _, event := range span.Events() {
			if strings.Contains(event.Name, "synthetic-private") {
				t.Fatal(event)
			}
			for _, field := range event.Attributes {
				if strings.Contains(field.Value.AsString(), "synthetic-private") {
					t.Fatal(field)
				}
			}
		}
	}
}
