package routery

import (
	"context"
	"errors"
	"testing"
)

type routeRequest struct {
	Key      string
	Number   int
	Decision string
}

func TestRouteTableDispatchAnnotatesTypedResultWithMatch(t *testing.T) {
	// Arrange.
	handler := func(call RouteCall[routeRequest]) (RouteResult[testKind, testReason, string], error) {
		if call.Match.RouteID != "primary" {
			t.Fatalf("handler Match.RouteID = %q, want primary", call.Match.RouteID)
		}
		return Handled(testKindHandled, testReasonHandled, "routed"), nil
	}
	router, err := NewRouteTable[routeRequest, testKind, testReason, string]().
		Route("primary", 10, func(req routeRequest) bool { return req.Number > 0 }, handler).
		Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	// Act.
	result, err := router.Dispatch(t.Context(), routeRequest{Number: 1})

	// Assert.
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if result.Payload != "routed" || result.Kind != testKindHandled || result.Reason != testReasonHandled {
		t.Fatalf("result = %#v, want typed routed result", result)
	}
	if result.Match.RouteID != "primary" || result.Match.Kind != MatchKindPredicate || result.Match.Priority != 10 {
		t.Fatalf("match = %#v, want predicate primary priority 10", result.Match)
	}
}

func TestDispatchWithSinkObservesNestedRouteEvents(t *testing.T) {
	// Arrange.
	nested := NewRouteTable[routeRequest, testKind, testReason, string]().
		Route("inner", 1, nil, func(RouteCall[routeRequest]) (RouteResult[testKind, testReason, string], error) {
			return Handled(testKindHandled, testReasonHandled, "nested"), nil
		})
	router, err := NewRouteTable[routeRequest, testKind, testReason, string]().
		Mount("outer", 1, nil, nested).
		Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	var events []RouteEvent[testKind, testReason, string]
	sink := OutcomeSinkFunc[testKind, testReason, string](func(event RouteEvent[testKind, testReason, string]) {
		events = append(events, event)
	})

	// Act.
	result, err := router.DispatchWithSink(t.Context(), routeRequest{}, sink)

	// Assert.
	if err != nil {
		t.Fatalf("DispatchWithSink() error = %v", err)
	}
	if result.Payload != "nested" {
		t.Fatalf("Payload = %q, want nested", result.Payload)
	}
	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1", len(events))
	}
	if got := events[0].Match.Path; len(got) != 2 || got[0] != "outer" || got[1] != "inner" {
		t.Fatalf("event path = %#v, want [outer inner]", got)
	}
}

func TestKeyedRoutesSupportExactComparableKeys(t *testing.T) {
	// Arrange.
	table := NewRouteTable[routeRequest, testKind, testReason, string]()
	OnKey(table, func(req routeRequest) (int, bool) { return req.Number, true }).
		Exact("two", 1, 2, func(RouteCall[routeRequest]) (RouteResult[testKind, testReason, string], error) {
			return Handled(testKindHandled, testReasonHandled, "two"), nil
		})
	router, err := table.Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	// Act.
	result, err := router.Dispatch(t.Context(), routeRequest{Number: 2})

	// Assert.
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if result.Payload != "two" {
		t.Fatalf("Payload = %q, want two", result.Payload)
	}
	if result.Match.Kind != MatchKindExact || result.Match.Key != "2" {
		t.Fatalf("match = %#v, want exact key 2", result.Match)
	}
}

func TestRouteTableReturnsLastNextReasonWhenAllMatchesFallThrough(t *testing.T) {
	// Arrange.
	router, err := NewRouteTable[routeRequest, testKind, testReason, string]().
		Route("delegate", 1, nil, func(RouteCall[routeRequest]) (RouteResult[testKind, testReason, string], error) {
			return Next[testKind, testReason, string](testReasonDelegate), nil
		}).
		Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	// Act.
	result, err := router.Dispatch(t.Context(), routeRequest{})

	// Assert.
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if result.Action != ActionNext || result.Reason != testReasonDelegate {
		t.Fatalf("result = %#v, want ActionNext with delegate reason", result)
	}
}

func TestKeyedRoutesUseLongestPrefixForEqualPriority(t *testing.T) {
	// Arrange.
	table := NewRouteTable[routeRequest, testKind, testReason, string]()
	OnStringKey(table, func(req routeRequest) (string, bool) { return req.Key, true }).
		Prefix("short", 1, "/a", func(RouteCall[routeRequest]) (RouteResult[testKind, testReason, string], error) {
			return Handled(testKindHandled, testReasonHandled, "short"), nil
		}).
		Prefix("long", 1, "/a/b", func(RouteCall[routeRequest]) (RouteResult[testKind, testReason, string], error) {
			return Handled(testKindHandled, testReasonHandled, "long"), nil
		}).
		LongestPrefixWins()
	router, err := table.Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	// Act.
	result, err := router.Dispatch(t.Context(), routeRequest{Key: "/a/b/c"})

	// Assert.
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if result.Payload != "long" {
		t.Fatalf("Payload = %q, want long", result.Payload)
	}
	if result.Match.Kind != MatchKindPrefix || result.Match.Prefix != "/a/b" || result.Match.Remainder != "/c" {
		t.Fatalf("match = %#v, want prefix /a/b remainder /c", result.Match)
	}
}

func TestKeyedRoutesLongestPrefixModeBeatsHigherPriorityShortPrefix(t *testing.T) {
	// Arrange.
	table := NewRouteTable[routeRequest, testKind, testReason, string]()
	OnStringKey(table, func(req routeRequest) (string, bool) { return req.Key, true }).
		Prefix("short", 100, "/a", func(RouteCall[routeRequest]) (RouteResult[testKind, testReason, string], error) {
			return Handled(testKindHandled, testReasonHandled, "short"), nil
		}).
		Prefix("long", 1, "/a/b", func(RouteCall[routeRequest]) (RouteResult[testKind, testReason, string], error) {
			return Handled(testKindHandled, testReasonHandled, "long"), nil
		}).
		LongestPrefixWins()
	router, err := table.Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	// Act.
	result, err := router.Dispatch(t.Context(), routeRequest{Key: "/a/b/c"})

	// Assert.
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if result.Payload != "long" {
		t.Fatalf("Payload = %q, want long", result.Payload)
	}
	if result.Match.RouteID != "long" || result.Match.Priority != 1 {
		t.Fatalf("match = %#v, want lower-priority longest prefix route", result.Match)
	}
}

func TestDecisionRoutesUseTypedClassifierDecision(t *testing.T) {
	// Arrange.
	table := NewRouteTable[routeRequest, testKind, testReason, string]()
	OnDecision(table, func(_ context.Context, req routeRequest) (RouteDecision[string, testReason], error) {
		return RouteDecision[string, testReason]{
			Key:        req.Decision,
			Matched:    true,
			Confidence: 0.9,
			Reason:     testReasonClassified,
		}, nil
	}).Case("classified", 1, "alpha", 0.5, func(RouteCall[routeRequest]) (RouteResult[testKind, testReason, string], error) {
		return Handled(testKindHandled, testReasonClassified, "classified"), nil
	})
	router, err := table.Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	// Act.
	result, err := router.Dispatch(t.Context(), routeRequest{Decision: "alpha"})

	// Assert.
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if result.Payload != "classified" {
		t.Fatalf("Payload = %q, want classified", result.Payload)
	}
	if result.Match.Kind != MatchKindDecision || result.Match.Key != "alpha" {
		t.Fatalf("match = %#v, want decision key alpha", result.Match)
	}
	gotReason, ok := MatchDecisionReason[testReason](result.Match)
	if !ok || gotReason != testReasonClassified {
		t.Fatalf("decision reason = (%q, %v), want (%q, true)", gotReason, ok, testReasonClassified)
	}
}

func TestDecisionRoutesComputeClassifierOnceAcrossCases(t *testing.T) {
	// Arrange.
	calls := 0
	table := NewRouteTable[routeRequest, testKind, testReason, string]()
	OnDecision(table, func(_ context.Context, req routeRequest) (RouteDecision[string, testReason], error) {
		calls++
		return RouteDecision[string, testReason]{
			Key:        req.Decision,
			Matched:    true,
			Confidence: 0.9,
			Reason:     testReasonClassified,
		}, nil
	}).
		Case("beta", 2, "beta", 0.5, func(RouteCall[routeRequest]) (RouteResult[testKind, testReason, string], error) {
			return Handled(testKindHandled, testReasonHandled, "beta"), nil
		}).
		Case("alpha", 1, "alpha", 0.5, func(RouteCall[routeRequest]) (RouteResult[testKind, testReason, string], error) {
			return Handled(testKindHandled, testReasonClassified, "alpha"), nil
		})
	router, err := table.Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	// Act.
	result, err := router.Dispatch(t.Context(), routeRequest{Decision: "alpha"})

	// Assert.
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if result.Payload != "alpha" {
		t.Fatalf("Payload = %q, want alpha", result.Payload)
	}
	if calls != 1 {
		t.Fatalf("classifier calls = %d, want 1", calls)
	}
	gotReason, ok := MatchDecisionReason[testReason](result.Match)
	if !ok || gotReason != testReasonClassified {
		t.Fatalf("decision reason = (%q, %v), want (%q, true)", gotReason, ok, testReasonClassified)
	}
}

func TestDecisionRoutesPropagateClassifierErrors(t *testing.T) {
	// Arrange.
	wantErr := errors.New("classify")
	table := NewRouteTable[routeRequest, testKind, testReason, string]()
	OnDecision(table, func(context.Context, routeRequest) (RouteDecision[string, testReason], error) {
		return RouteDecision[string, testReason]{}, wantErr
	}).Case("classified", 1, "alpha", 0, func(RouteCall[routeRequest]) (RouteResult[testKind, testReason, string], error) {
		return Handled(testKindHandled, testReasonClassified, "classified"), nil
	})
	router, err := table.Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	// Act.
	result, err := router.Dispatch(t.Context(), routeRequest{Decision: "alpha"})

	// Assert.
	if !errors.Is(err, wantErr) {
		t.Fatalf("Dispatch() error = %v, want %v", err, wantErr)
	}
	if result.Action != ActionAbort {
		t.Fatalf("Action = %q, want abort", result.Action)
	}
}

func TestRouteTableRejectsAbortResultWithoutError(t *testing.T) {
	// Arrange.
	router, err := NewRouteTable[routeRequest, testKind, testReason, string]().
		Route("bad-abort", 1, nil, func(RouteCall[routeRequest]) (RouteResult[testKind, testReason, string], error) {
			return AbortResult[testKind, testReason, string](), nil
		}).
		Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	var events []RouteEvent[testKind, testReason, string]
	sink := OutcomeSinkFunc[testKind, testReason, string](func(event RouteEvent[testKind, testReason, string]) {
		events = append(events, event)
	})

	// Act.
	result, err := router.DispatchWithSink(t.Context(), routeRequest{}, sink)

	// Assert.
	if err == nil {
		t.Fatal("DispatchWithSink() error = nil, want config error")
	}
	if result.Action != ActionAbort || result.Match.RouteID != "bad-abort" {
		t.Fatalf("result = %#v, want abort result for bad-abort", result)
	}
	if len(events) != 1 || events[0].Err == nil || events[0].Result.Action != ActionAbort {
		t.Fatalf("events = %#v, want one abort event with error", events)
	}
}

func TestRouteTableFallbackRejectsUnexpectedAction(t *testing.T) {
	// Arrange.
	router, err := NewRouteTable[routeRequest, testKind, testReason, string]().
		Fallback(func(RouteCall[routeRequest]) (RouteResult[testKind, testReason, string], error) {
			return RouteResult[testKind, testReason, string]{
				Action:     RouteAction("unknown"),
				Kind:       testKindIgnored,
				Reason:     testReasonNoMatch,
				Payload:    "",
				HasPayload: false,
				Match:      zeroRouteMatch(),
			}, nil
		}).
		Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	// Act.
	result, err := router.Dispatch(t.Context(), routeRequest{})

	// Assert.
	if err == nil {
		t.Fatal("Dispatch() error = nil, want config error")
	}
	if result.Action != ActionAbort || result.Match.RouteID != "fallback" {
		t.Fatalf("result = %#v, want fallback abort result", result)
	}
}
