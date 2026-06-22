package routery

import (
	"errors"
	"testing"
)

type testAction string

const (
	testActionRecover  testAction = "recover"
	testActionSuppress testAction = "suppress"
	testActionRetry    testAction = "retry"
)

func TestDecisionTableUsesOrderedCases(t *testing.T) {
	// Arrange.
	table, err := NewDecisionTable[routeRequest, testAction, testReason]().
		Case(func(routeRequest) (bool, error) { return true, nil }, testActionSuppress, testReasonHandled).
		Case(func(routeRequest) (bool, error) { return true, nil }, testActionRecover, testReasonDelegate).
		Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	// Act.
	result, err := table.Decide(routeRequest{})

	// Assert.
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if !result.Matched || result.Action != testActionSuppress || result.Reason != testReasonHandled {
		t.Fatalf("result = %#v, want first matching suppress case", result)
	}
}

func TestDecisionTableReturnsExplicitNoMatch(t *testing.T) {
	// Arrange.
	table, err := NewDecisionTable[routeRequest, testAction, testReason]().
		Case(func(routeRequest) (bool, error) { return false, nil }, testActionRecover, testReasonHandled).
		Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	// Act.
	result, err := table.Decide(routeRequest{})

	// Assert.
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if result.Matched || result.Terminal {
		t.Fatalf("result = %#v, want explicit no-match", result)
	}
}

func TestDecisionTablePropagatesMatcherError(t *testing.T) {
	// Arrange.
	wantErr := errors.New("match")
	table, err := NewDecisionTable[routeRequest, testAction, testReason]().
		Case(func(routeRequest) (bool, error) { return false, wantErr }, testActionRecover, testReasonHandled).
		Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	// Act.
	result, err := table.Decide(routeRequest{})

	// Assert.
	if !errors.Is(err, wantErr) {
		t.Fatalf("Decide() error = %v, want %v", err, wantErr)
	}
	if result.Matched {
		t.Fatalf("result = %#v, want no matched result on error", result)
	}
}

func TestDecisionTableHandlerMapsActionsToRouteResults(t *testing.T) {
	// Arrange.
	table, err := NewDecisionTable[routeRequest, testAction, testReason]().
		Case(func(req routeRequest) (bool, error) { return req.Number > 0, nil }, testActionRecover, testReasonHandled).
		Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	handler := DecisionTableHandler(
		table,
		func(
			_ RouteCall[routeRequest],
			decision DecisionResult[testAction, testReason],
		) (RouteResult[testKind, testReason, string], error) {
			if !decision.Matched {
				return Next[testKind, testReason, string](testReasonNoMatch), nil
			}

			return Handled(testKindHandled, decision.Reason, string(decision.Action)), nil
		},
	)

	// Act.
	result, err := InvokeRouteHandler(t.Context(), routeRequest{Number: 1}, handler)

	// Assert.
	if err != nil {
		t.Fatalf("InvokeRouteHandler() error = %v", err)
	}
	if result.Payload != "recover" || result.Reason != testReasonHandled {
		t.Fatalf("result = %#v, want mapped recover action", result)
	}
}

func TestDecisionTableRoutesUseTypedActionAndReason(t *testing.T) {
	// Arrange.
	calls := 0
	decisions, err := NewDecisionTable[routeRequest, testAction, testReason]().
		Case(func(req routeRequest) (bool, error) {
			calls++
			return req.Key == "recover", nil
		}, testActionRecover, testReasonClassified).
		Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	table := NewRouteTable[routeRequest, testKind, testReason, string]()
	OnDecisionTable(table, decisions).
		Case("suppress", 2, testActionSuppress, func(RouteCall[routeRequest]) (
			RouteResult[testKind, testReason, string],
			error,
		) {
			return Handled(testKindHandled, testReasonHandled, "suppress"), nil
		}).
		Case("recover", 1, testActionRecover, func(RouteCall[routeRequest]) (
			RouteResult[testKind, testReason, string],
			error,
		) {
			return Handled(testKindHandled, testReasonClassified, "recover"), nil
		})
	router, err := table.Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	// Act.
	result, err := router.Dispatch(t.Context(), routeRequest{Key: "recover"})

	// Assert.
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if result.Payload != "recover" {
		t.Fatalf("Payload = %q, want recover", result.Payload)
	}
	if result.Match.Kind != MatchKindTable || result.Match.Key != "recover" {
		t.Fatalf("match = %#v, want decision-table recover match", result.Match)
	}
	gotReason, ok := MatchDecisionReason[testReason](result.Match)
	if !ok || gotReason != testReasonClassified {
		t.Fatalf("decision reason = (%q, %v), want (%q, true)", gotReason, ok, testReasonClassified)
	}
	if calls != 1 {
		t.Fatalf("decision table calls = %d, want 1", calls)
	}
}

func TestDecisionTableRouteNoMatchFallsThroughToFallback(t *testing.T) {
	// Arrange.
	decisions, err := NewDecisionTable[routeRequest, testAction, testReason]().
		Case(func(routeRequest) (bool, error) { return false, nil }, testActionRecover, testReasonClassified).
		Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	table := NewRouteTable[routeRequest, testKind, testReason, string]()
	OnDecisionTable(table, decisions).
		Case("recover", 1, testActionRecover, func(RouteCall[routeRequest]) (
			RouteResult[testKind, testReason, string],
			error,
		) {
			return Handled(testKindHandled, testReasonClassified, "recover"), nil
		})
	router, err := table.
		Fallback(func(RouteCall[routeRequest]) (RouteResult[testKind, testReason, string], error) {
			return Handled(testKindHandled, testReasonNoMatch, "fallback"), nil
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
	if result.Payload != "fallback" || result.Match.Kind != MatchKindFallback {
		t.Fatalf("result = %#v, want fallback after decision-table no-match", result)
	}
}

func TestDecisionTableRoutePropagatesMatcherError(t *testing.T) {
	// Arrange.
	wantErr := errors.New("decision table")
	decisions, err := NewDecisionTable[routeRequest, testAction, testReason]().
		Case(func(routeRequest) (bool, error) { return false, wantErr }, testActionRecover, testReasonClassified).
		Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	table := NewRouteTable[routeRequest, testKind, testReason, string]()
	OnDecisionTable(table, decisions).
		Case("recover", 1, testActionRecover, func(RouteCall[routeRequest]) (
			RouteResult[testKind, testReason, string],
			error,
		) {
			return Handled(testKindHandled, testReasonClassified, "recover"), nil
		})
	router, err := table.Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	// Act.
	result, err := router.Dispatch(t.Context(), routeRequest{})

	// Assert.
	if !errors.Is(err, wantErr) {
		t.Fatalf("Dispatch() error = %v, want %v", err, wantErr)
	}
	if result.Action != ActionAbort || result.Match.Kind != MatchKindTable {
		t.Fatalf("result = %#v, want decision-table abort", result)
	}
}
