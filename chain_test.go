package routery

import (
	"errors"
	"testing"
)

func TestChainStopsAtFirstTerminalResult(t *testing.T) {
	// Arrange.
	calls := 0
	first := func(RouteCall[int]) (RouteResult[testKind, testReason, string], error) {
		calls++
		return Handled(testKindHandled, testReasonHandled, "first"), nil
	}
	second := func(RouteCall[int]) (RouteResult[testKind, testReason, string], error) {
		calls++
		return Handled(testKindHandled, testReasonHandled, "second"), nil
	}
	handler := Chain(first, second)

	// Act.
	result, err := InvokeRouteHandler(t.Context(), 0, handler)

	// Assert.
	if err != nil {
		t.Fatalf("InvokeRouteHandler() error = %v", err)
	}
	if result.Payload != "first" {
		t.Fatalf("Payload = %q, want first", result.Payload)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestChainFallsThroughActionNextWithTypedReason(t *testing.T) {
	// Arrange.
	first := func(RouteCall[int]) (RouteResult[testKind, testReason, string], error) {
		return Next[testKind, testReason, string](testReasonDelegate), nil
	}
	second := func(RouteCall[int]) (RouteResult[testKind, testReason, string], error) {
		return Handled(testKindHandled, testReasonHandled, "second"), nil
	}
	handler := Chain(first, second)

	// Act.
	result, err := InvokeRouteHandler(t.Context(), 0, handler)

	// Assert.
	if err != nil {
		t.Fatalf("InvokeRouteHandler() error = %v", err)
	}
	if result.Payload != "second" {
		t.Fatalf("Payload = %q, want second", result.Payload)
	}
}

func TestChainReturnsLastNextWhenAllHandlersFallThrough(t *testing.T) {
	// Arrange.
	handler := Chain(
		func(RouteCall[int]) (RouteResult[testKind, testReason, string], error) {
			return Next[testKind, testReason, string](testReasonDelegate), nil
		},
		func(RouteCall[int]) (RouteResult[testKind, testReason, string], error) {
			return Next[testKind, testReason, string](testReasonNoMatch), nil
		},
	)

	// Act.
	result, err := InvokeRouteHandler(t.Context(), 0, handler)

	// Assert.
	if err != nil {
		t.Fatalf("InvokeRouteHandler() error = %v", err)
	}
	if result.Action != ActionNext || result.Reason != testReasonNoMatch {
		t.Fatalf("result = %#v, want ActionNext with no-match reason", result)
	}
}

func TestChainAbortsOnHandlerError(t *testing.T) {
	// Arrange.
	wantErr := errors.New("chain")
	handler := Chain(func(RouteCall[int]) (RouteResult[testKind, testReason, string], error) {
		return Next[testKind, testReason, string](testReasonDelegate), wantErr
	})

	// Act.
	result, err := InvokeRouteHandler(t.Context(), 0, handler)

	// Assert.
	if !errors.Is(err, wantErr) {
		t.Fatalf("InvokeRouteHandler() error = %v, want %v", err, wantErr)
	}
	if result.Action != ActionAbort {
		t.Fatalf("Action = %q, want abort", result.Action)
	}
}

func TestChainRejectsAbortResultWithoutError(t *testing.T) {
	// Arrange.
	handler := Chain(func(RouteCall[int]) (RouteResult[testKind, testReason, string], error) {
		return AbortResult[testKind, testReason, string](), nil
	})

	// Act.
	result, err := InvokeRouteHandler(t.Context(), 0, handler)

	// Assert.
	if err == nil {
		t.Fatal("InvokeRouteHandler() error = nil, want config error")
	}
	if result.Action != ActionAbort {
		t.Fatalf("Action = %q, want abort", result.Action)
	}
}
