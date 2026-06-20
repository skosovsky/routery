package routery

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestApplyRouteMiddlewareReceivesRouteMatch(t *testing.T) {
	// Arrange.
	var got RouteMatch
	observed := func(next RouteHandler[routeRequest, testKind, testReason, string]) RouteHandler[routeRequest, testKind, testReason, string] {
		return func(call RouteCall[routeRequest]) (RouteResult[testKind, testReason, string], error) {
			got = call.Match
			return next(call)
		}
	}
	handler := ApplyRoute(
		func(RouteCall[routeRequest]) (RouteResult[testKind, testReason, string], error) {
			return Handled(testKindHandled, testReasonHandled, "ok"), nil
		},
		observed,
	)
	router, err := NewRouteTable[routeRequest, testKind, testReason, string]().
		Route("observed", 7, nil, handler).
		Build()
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	// Act.
	_, err = router.Dispatch(t.Context(), routeRequest{})

	// Assert.
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if got.RouteID != "observed" || got.Priority != 7 {
		t.Fatalf("middleware match = %#v, want observed priority 7", got)
	}
}

func TestRetryIfRetriesOnlySystemErrors(t *testing.T) {
	// Arrange.
	attempts := 0
	handler := ApplyRoute(
		func(RouteCall[int]) (RouteResult[testKind, testReason, string], error) {
			attempts++
			if attempts == 1 {
				return AbortResult[testKind, testReason, string](), errors.New("temporary")
			}
			return Handled(testKindHandled, testReasonHandled, "ok"), nil
		},
		RetryIf[int, testKind, testReason, string](2, 0, func(context.Context, int, error) bool {
			return true
		}),
	)

	// Act.
	result, err := InvokeRouteHandler(t.Context(), 0, handler)

	// Assert.
	if err != nil {
		t.Fatalf("InvokeRouteHandler() error = %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if result.Payload != "ok" {
		t.Fatalf("Payload = %q, want ok", result.Payload)
	}
}

func TestBulkheadRejectsWhenFull(t *testing.T) {
	// Arrange.
	started := make(chan struct{})
	release := make(chan struct{})
	handler := ApplyRoute(
		func(RouteCall[int]) (RouteResult[testKind, testReason, string], error) {
			close(started)
			<-release
			return Handled(testKindHandled, testReasonHandled, "ok"), nil
		},
		Bulkhead[int, testKind, testReason, string](1),
	)
	var group sync.WaitGroup
	group.Go(func() {
		_, _ = InvokeRouteHandler(t.Context(), 0, handler)
	})
	<-started

	// Act.
	_, err := InvokeRouteHandler(t.Context(), 0, handler)
	close(release)
	group.Wait()

	// Assert.
	if !errors.Is(err, ErrTooManyRequests) {
		t.Fatalf("err = %v, want ErrTooManyRequests", err)
	}
}

func TestCircuitBreakerOpensAfterFailures(t *testing.T) {
	// Arrange.
	handler := ApplyRoute(
		func(RouteCall[int]) (RouteResult[testKind, testReason, string], error) {
			return AbortResult[testKind, testReason, string](), errors.New("down")
		},
		CircuitBreaker[int, testKind, testReason, string](1, time.Minute, nil),
	)

	// Act.
	_, firstErr := InvokeRouteHandler(t.Context(), 0, handler)
	_, secondErr := InvokeRouteHandler(t.Context(), 0, handler)

	// Assert.
	if firstErr == nil {
		t.Fatal("first err = nil, want failure")
	}
	if !errors.Is(secondErr, ErrCircuitOpen) {
		t.Fatalf("second err = %v, want ErrCircuitOpen", secondErr)
	}
}

func TestFirstCompletedReturnsFirstPayloadResult(t *testing.T) {
	// Arrange.
	slow := func(call RouteCall[int]) (RouteResult[testKind, testReason, string], error) {
		select {
		case <-time.After(50 * time.Millisecond):
			return Handled(testKindHandled, testReasonHandled, "slow"), nil
		case <-call.Context.Done():
			return AbortResult[testKind, testReason, string](), call.Context.Err()
		}
	}
	fast := func(RouteCall[int]) (RouteResult[testKind, testReason, string], error) {
		return Handled(testKindHandled, testReasonHandled, "fast"), nil
	}
	handler := FirstCompleted(slow, fast)

	// Act.
	result, err := InvokeRouteHandler(t.Context(), 0, handler)

	// Assert.
	if err != nil {
		t.Fatalf("InvokeRouteHandler() error = %v", err)
	}
	if result.Payload != "fast" {
		t.Fatalf("Payload = %q, want fast", result.Payload)
	}
}

func TestFirstCompletedIgnoresActionNextWithPayload(t *testing.T) {
	// Arrange.
	nextWithPayload := func(RouteCall[int]) (RouteResult[testKind, testReason, string], error) {
		return RouteResult[testKind, testReason, string]{
			Action:     ActionNext,
			Kind:       testKindIgnored,
			Reason:     testReasonDelegate,
			Payload:    "delegate",
			HasPayload: true,
			Match:      zeroRouteMatch(),
		}, nil
	}
	terminal := func(call RouteCall[int]) (RouteResult[testKind, testReason, string], error) {
		select {
		case <-time.After(10 * time.Millisecond):
			return Handled(testKindHandled, testReasonHandled, "terminal"), nil
		case <-call.Context.Done():
			return AbortResult[testKind, testReason, string](), call.Context.Err()
		}
	}
	handler := FirstCompleted(nextWithPayload, terminal)

	// Act.
	result, err := InvokeRouteHandler(t.Context(), 0, handler)

	// Assert.
	if err != nil {
		t.Fatalf("InvokeRouteHandler() error = %v", err)
	}
	if result.Payload != "terminal" {
		t.Fatalf("Payload = %q, want terminal", result.Payload)
	}
}
