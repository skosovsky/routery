package routery

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

func TestRouterClosesDiscardedNextResources(t *testing.T) {
	for _, name := range []string{"handled", "next", "fallback", "cancel", "nested", "cleanup error"} {
		t.Run(name, func(t *testing.T) { checkRouterNextOwnership(t, name) })
	}
}

func checkRouterNextOwnership(t *testing.T, name string) {
	t.Helper()
	// Arrange.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var closes atomic.Int32
	cleanupErr := errors.New("resource cleanup failed")
	next := func(RouteCall[int]) (BasicRouteResult[int], error) {
		result := BasicNext[int](BasicReasonNoMatch)
		result.Lifetime = NewLifetime(func() error {
			closes.Add(1)
			if name == "cleanup error" {
				return cleanupErr
			}
			return nil
		})
		if name == "cancel" {
			cancel()
		}
		return result, nil
	}
	secondCalls := 0
	handled := func(RouteCall[int]) (BasicRouteResult[int], error) {
		secondCalls++
		if closes.Load() != 1 {
			t.Error("next handler invoked before discarded resource cleanup")
		}
		return BasicHandled(42), nil
	}
	table := NewBasicRouteTable[int, int]()
	if name == "nested" {
		table.Mount("nested", 10, nil, NewBasicRouteTable[int, int]().Route("next", 1, nil, next))
	} else {
		table.Route("next", 10, nil, next)
	}
	switch name {
	case "next":
		table.Route("another-next", 1, nil, next)
	case "fallback":
		table.Fallback(handled)
	default:
		table.Route("handled", 1, nil, handled)
	}
	router, err := table.Build()
	if err != nil {
		t.Fatal(err)
	}
	// Act.
	result, err := router.Dispatch(ctx, 0)
	// Assert.
	assertDiscardedNext(t, name, result, err, closes.Load(), secondCalls, cleanupErr)
}

func assertDiscardedNext(t *testing.T, name string, result BasicRouteResult[int], err error,
	closes int32, secondCalls int, cleanupErr error,
) {
	t.Helper()
	switch name {
	case "cancel":
		if !errors.Is(err, context.Canceled) || closes != 1 || secondCalls != 0 {
			t.Fatal("cancelled fallthrough lost resource or started another handler")
		}
	case "cleanup error":
		if !errors.Is(err, cleanupErr) || closes != 1 || secondCalls != 0 {
			t.Fatal("cleanup failure silently continued routing")
		}
	case "next":
		if err != nil || closes != 2 || result.Action != ActionNext {
			t.Fatal("overwritten/final next resource was not closed")
		}
	default:
		if err != nil || closes != 1 || result.Payload != 42 || secondCalls != 1 {
			t.Fatalf("err=%v closes=%d payload=%d calls=%d", err, closes, result.Payload, secondCalls)
		}
	}
}

func TestInvalidResultRetainsPartialPayloadAndOwnership(t *testing.T) {
	for _, action := range []RouteAction{ActionAbort, RouteAction("unknown")} {
		for _, boundary := range []string{"invoke", "chain", "router", "fallback", "first successful payload"} {
			t.Run(string(action)+"/"+boundary, func(t *testing.T) { checkInvalidOwnedResult(t, action, boundary) })
		}
	}
}

func checkInvalidOwnedResult(t *testing.T, action RouteAction, boundary string) {
	t.Helper()
	// Arrange: validation failure does not release the caller from owning a partial resource.
	var closes atomic.Int32
	cleanupErr := errors.New("close failed")
	life := NewLifetime(func() error { closes.Add(1); return cleanupErr })
	leaf := func(RouteCall[int]) (BasicRouteResult[int], error) {
		result := BasicHandled(42)
		result.Action, result.Lifetime = action, life
		return result, nil
	}
	// Act.
	result, err := invokeOwnedValidationBoundary(t, boundary, leaf)
	// Assert: invalid action becomes Abort while payload, kind/reason and lifetime survive.
	if !errors.Is(err, ErrInvalidConfig) || result.Action != ActionAbort || result.Lifetime != life ||
		!result.HasPayload || result.Payload != 42 || result.Kind != BasicKindHandled {
		t.Fatalf("boundary=%s err=%v result=%+v", boundary, err, result)
	}
	if !errors.Is(result.Lifetime.Close(), cleanupErr) || closes.Load() != 1 {
		t.Fatal("partial ownership or stored cleanup error lost")
	}
}

func invokeOwnedValidationBoundary(
	t *testing.T,
	boundary string,
	leaf BasicRouteHandler[int, int],
) (BasicRouteResult[int], error) {
	t.Helper()
	switch boundary {
	case "invoke":
		return InvokeRouteHandler(t.Context(), 0, leaf)
	case "chain":
		return InvokeRouteHandler(t.Context(), 0, Chain(leaf))
	case "first successful payload":
		return InvokeRouteHandler(t.Context(), 0, FirstSuccessfulPayload(leaf))
	default:
		table := NewBasicRouteTable[int, int]()
		if boundary == "fallback" {
			table.Fallback(leaf)
		} else {
			table.Route("leaf", 1, nil, leaf)
		}
		router, err := table.Build()
		if err != nil {
			t.Fatal(err)
		}
		return router.Dispatch(t.Context(), 0)
	}
}
