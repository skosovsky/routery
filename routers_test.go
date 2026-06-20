package routery

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFallbackDoesNotTreatActionNextAsError(t *testing.T) {
	// Arrange.
	secondaryCalled := false
	handler := Fallback(
		func(RouteCall[int]) (RouteResult[testKind, testReason, string], error) {
			return Next[testKind, testReason, string](testReasonDelegate), nil
		},
		func(RouteCall[int]) (RouteResult[testKind, testReason, string], error) {
			secondaryCalled = true
			return Handled(testKindHandled, testReasonHandled, "secondary"), nil
		},
	)

	// Act.
	result, err := InvokeRouteHandler(t.Context(), 1, handler)

	// Assert.
	if err != nil {
		t.Fatalf("InvokeRouteHandler() error = %v", err)
	}
	if result.Action != ActionNext || result.Reason != testReasonDelegate {
		t.Fatalf("result = %#v, want ActionNext with delegate reason", result)
	}
	if secondaryCalled {
		t.Fatal("secondaryCalled = true, want false")
	}
}

func TestFallbackUsesSecondaryOnError(t *testing.T) {
	// Arrange.
	wantErr := errors.New("primary")
	handler := Fallback(
		func(RouteCall[int]) (RouteResult[testKind, testReason, string], error) {
			return AbortResult[testKind, testReason, string](), wantErr
		},
		func(RouteCall[int]) (RouteResult[testKind, testReason, string], error) {
			return Handled(testKindHandled, testReasonHandled, "secondary"), nil
		},
	)

	// Act.
	result, err := InvokeRouteHandler(t.Context(), 1, handler)

	// Assert.
	if err != nil {
		t.Fatalf("InvokeRouteHandler() error = %v", err)
	}
	if result.Payload != "secondary" {
		t.Fatalf("Payload = %q, want secondary", result.Payload)
	}
}

func TestRoundRobinCyclesTypedResults(t *testing.T) {
	// Arrange.
	handler := RoundRobin(
		func(RouteCall[int]) (RouteResult[testKind, testReason, string], error) {
			return Handled(testKindHandled, testReasonHandled, "a"), nil
		},
		func(RouteCall[int]) (RouteResult[testKind, testReason, string], error) {
			return Handled(testKindHandled, testReasonHandled, "b"), nil
		},
	)

	// Act.
	first, firstErr := InvokeRouteHandler(t.Context(), 0, handler)
	second, secondErr := InvokeRouteHandler(t.Context(), 0, handler)
	third, thirdErr := InvokeRouteHandler(t.Context(), 0, handler)

	// Assert.
	if joined := errors.Join(firstErr, secondErr, thirdErr); joined != nil {
		t.Fatalf("InvokeRouteHandler() error = %v", joined)
	}
	if got := []string{first.Payload, second.Payload, third.Payload}; got[0] != "a" || got[1] != "b" || got[2] != "a" {
		t.Fatalf("payloads = %#v, want [a b a]", got)
	}
}

func TestTimeoutPassesDeadlineToWrappedHandler(t *testing.T) {
	// Arrange.
	handler := ApplyRoute(
		func(call RouteCall[int]) (RouteResult[testKind, testReason, string], error) {
			if _, ok := call.Context.Deadline(); !ok {
				return AbortResult[testKind, testReason, string](), errors.New("missing deadline")
			}
			return Handled(testKindHandled, testReasonHandled, "deadline"), nil
		},
		Timeout[int, testKind, testReason, string](time.Second),
	)

	// Act.
	result, err := InvokeRouteHandler(t.Context(), 0, handler)

	// Assert.
	if err != nil {
		t.Fatalf("InvokeRouteHandler() error = %v", err)
	}
	if result.Payload != "deadline" {
		t.Fatalf("Payload = %q, want deadline", result.Payload)
	}
}

func TestPredicateFallbackKeepsErrorWhenPredicateRejectsFallback(t *testing.T) {
	// Arrange.
	wantErr := errors.New("primary")
	secondaryCalled := false
	handler := PredicateFallback(
		func(RouteCall[int]) (RouteResult[testKind, testReason, string], error) {
			return AbortResult[testKind, testReason, string](), wantErr
		},
		func(RouteCall[int]) (RouteResult[testKind, testReason, string], error) {
			secondaryCalled = true
			return Handled(testKindHandled, testReasonHandled, "secondary"), nil
		},
		func(error) bool { return false },
	)

	// Act.
	result, err := InvokeRouteHandler(t.Context(), 0, handler)

	// Assert.
	if !errors.Is(err, wantErr) {
		t.Fatalf("InvokeRouteHandler() error = %v, want %v", err, wantErr)
	}
	if result.Action != ActionAbort {
		t.Fatalf("Action = %q, want abort", result.Action)
	}
	if secondaryCalled {
		t.Fatal("secondaryCalled = true, want false")
	}
}

func TestWeightBasedRouterChoosesTypedHandlerByThreshold(t *testing.T) {
	// Arrange.
	handler := WeightBasedRouter(
		func(_ context.Context, req routeRequest) (int, error) {
			return req.Number, nil
		},
		10,
		func(RouteCall[routeRequest]) (RouteResult[testKind, testReason, string], error) {
			return Handled(testKindHandled, testReasonHandled, "light"), nil
		},
		func(RouteCall[routeRequest]) (RouteResult[testKind, testReason, string], error) {
			return Handled(testKindHandled, testReasonHandled, "heavy"), nil
		},
	)

	// Act.
	light, lightErr := InvokeRouteHandler(t.Context(), routeRequest{Number: 3}, handler)
	heavy, heavyErr := InvokeRouteHandler(t.Context(), routeRequest{Number: 10}, handler)

	// Assert.
	if joined := errors.Join(lightErr, heavyErr); joined != nil {
		t.Fatalf("InvokeRouteHandler() error = %v", joined)
	}
	if light.Payload != "light" || heavy.Payload != "heavy" {
		t.Fatalf("payloads = (%q, %q), want (light, heavy)", light.Payload, heavy.Payload)
	}
}
