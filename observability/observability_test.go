package observability

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/skosovsky/routery"
)

type testKind string

const testKindHandled testKind = "handled"

type testReason string

const testReasonOK testReason = "ok"

func TestDefaultPayloadMetaUsesRouteResultPayload(t *testing.T) {
	// Arrange.
	result := routery.Handled(testKindHandled, testReasonOK, 10)

	// Act.
	meta := DefaultPayloadMeta(result)

	// Assert.
	if meta.Shape != "int" {
		t.Fatalf("Shape = %q, want int", meta.Shape)
	}
}

func TestLoggingEmitsTypedOutcomeAndMatch(t *testing.T) {
	// Arrange.
	var got Event[int, testKind, testReason, string]
	middleware := Logging("route", func(_ context.Context, event Event[int, testKind, testReason, string]) {
		got = event
	}, nil)
	handler := middleware(func(routery.RouteCall[int]) (routery.RouteResult[testKind, testReason, string], error) {
		return routery.Handled(testKindHandled, testReasonOK, "ok"), nil
	})
	call := routery.NewRouteCall(context.Background(), 1)
	call.Match = routery.RouteMatch{
		RouteID:           "typed",
		Path:              nil,
		Priority:          0,
		Depth:             0,
		Kind:              routery.MatchKindExact,
		Key:               "",
		Prefix:            "",
		Remainder:         "",
		DecisionReason:    nil,
		HasDecisionReason: false,
	}

	// Act.
	result, err := handler(call)

	// Assert.
	if err != nil {
		t.Fatalf("handler() error = %v", err)
	}
	if result.Payload != "ok" {
		t.Fatalf("Payload = %q, want ok", result.Payload)
	}
	if got.Outcome.Kind != testKindHandled || got.Outcome.Reason != testReasonOK {
		t.Fatalf("Outcome = %#v, want typed kind/reason", got.Outcome)
	}
	if got.Outcome.Match.RouteID != "typed" {
		t.Fatalf("Match.RouteID = %q, want typed", got.Outcome.Match.RouteID)
	}
}

func TestObservabilityMiddlewareReceivesEngineProducedRouteMatch(t *testing.T) {
	// Arrange.
	var logged routery.RouteMatch
	var metricStart routery.RouteMatch
	var metricComplete routery.RouteMatch
	handler := routery.ApplyRoute(
		func(routery.RouteCall[int]) (routery.RouteResult[testKind, testReason, string], error) {
			return routery.Handled(testKindHandled, testReasonOK, "ok"), nil
		},
		Logging("route", func(_ context.Context, event Event[int, testKind, testReason, string]) {
			logged = event.Outcome.Match
		}, nil),
		Metrics[int, testKind, testReason, string]("route", MetricsHooks[testKind, testReason, string]{
			OnStart: func(_ context.Context, _ string, match routery.RouteMatch) {
				metricStart = match
			},
			OnComplete: func(
				_ context.Context,
				_ string,
				_ time.Duration,
				result ResultMeta[testKind, testReason],
				_ PayloadMeta,
				_ error,
			) {
				metricComplete = result.Match
			},
		}),
	)
	router, err := routery.NewRouteTable[int, testKind, testReason, string]().
		Route("observed", 7, nil, handler).
		Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	// Act.
	result, err := router.Dispatch(t.Context(), 10)

	// Assert.
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if result.Payload != "ok" {
		t.Fatalf("Payload = %q, want ok", result.Payload)
	}
	for name, match := range map[string]routery.RouteMatch{
		"logging":          logged,
		"metrics start":    metricStart,
		"metrics complete": metricComplete,
	} {
		if match.RouteID != "observed" || match.Priority != 7 ||
			match.Depth != 0 || match.Kind != routery.MatchKindPredicate {
			t.Fatalf("%s match = %#v, want observed predicate priority 7 depth 0", name, match)
		}
	}
}

func TestMetricsEmitsAbortResultOnError(t *testing.T) {
	// Arrange.
	wantErr := errors.New("fail")
	var got ResultMeta[testKind, testReason]
	middleware := Metrics[int, testKind, testReason, string]("route", MetricsHooks[testKind, testReason, string]{
		OnComplete: func(_ context.Context, _ string, _ time.Duration, result ResultMeta[testKind, testReason], _ PayloadMeta, err error) {
			if !errors.Is(err, wantErr) {
				t.Fatalf("err = %v, want %v", err, wantErr)
			}
			got = result
		},
	})
	handler := middleware(func(routery.RouteCall[int]) (routery.RouteResult[testKind, testReason, string], error) {
		return routery.AbortResult[testKind, testReason, string](), wantErr
	})

	// Act.
	_, err := handler(routery.NewRouteCall(context.Background(), 1))

	// Assert.
	if !errors.Is(err, wantErr) {
		t.Fatalf("handler() error = %v, want %v", err, wantErr)
	}
	if got.Action != routery.ActionAbort {
		t.Fatalf("Action = %q, want abort", got.Action)
	}
}
