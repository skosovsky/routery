package routery

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

func TestBulkheadRetainsOwnedPermit(t *testing.T) {
	t.Parallel()
	for _, partialError := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "partial error"}[partialError], func(t *testing.T) {
			t.Parallel()
			checkOwnedPermit(t, partialError)
		})
	}
}

func checkOwnedPermit(t *testing.T, partialError bool) {
	t.Helper()
	// Arrange.
	var calls, closes atomic.Int32
	failure := errors.New("partial result")
	handler := Bulkhead[int, BasicKind, BasicReason, int](1)(func(RouteCall[int]) (BasicRouteResult[int], error) {
		calls.Add(1)
		result := BasicHandled(1)
		result.Lifetime = NewLifetime(func() error { closes.Add(1); return nil })
		if partialError {
			return result, failure
		}
		return result, nil
	})
	// Act.
	first, firstErr := InvokeRouteHandler(context.Background(), 0, handler)
	t.Cleanup(func() { _ = first.Lifetime.Close() })
	_, blockedErr := InvokeRouteHandler(context.Background(), 0, handler)
	// Assert.
	if (partialError && !errors.Is(firstErr, failure)) || (!partialError && firstErr != nil) {
		t.Fatalf("first error: %v", firstErr)
	}
	if !errors.Is(blockedErr, ErrTooManyRequests) || calls.Load() != 1 || closes.Load() != 0 {
		t.Fatalf("early permit release: error=%v calls=%d closes=%d", blockedErr, calls.Load(), closes.Load())
	}
	if err := first.Lifetime.Close(); err != nil {
		t.Fatal(err)
	}
	second, secondErr := InvokeRouteHandler(context.Background(), 0, handler)
	t.Cleanup(func() { _ = second.Lifetime.Close() })
	if calls.Load() != 2 || errors.Is(secondErr, ErrTooManyRequests) {
		t.Fatalf("permit not released: error=%v calls=%d", secondErr, calls.Load())
	}
}

func TestBulkheadCancellationBeforeAdmission(t *testing.T) {
	t.Parallel()
	// Arrange.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls atomic.Int32
	handler := Bulkhead[int, BasicKind, BasicReason, int](1)(func(RouteCall[int]) (BasicRouteResult[int], error) {
		calls.Add(1)
		return BasicHandled(1), nil
	})
	// Act.
	_, err := InvokeRouteHandler(ctx, 0, handler)
	// Assert.
	if !errors.Is(err, context.Canceled) || calls.Load() != 0 {
		t.Fatalf("error=%v calls=%d", err, calls.Load())
	}
}
