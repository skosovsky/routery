package routery

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLifetimeConcurrentCloseAndLateHooks(t *testing.T) {
	// Arrange.
	var releases, callbacks atomic.Int32
	wantErr := errors.New("cleanup error")
	life := NewLifetime(func() error { releases.Add(1); return wantErr })
	var group sync.WaitGroup
	// Act.
	for range 20 {
		group.Go(func() {
			life.OnClose(func() { callbacks.Add(1) })
			if !errors.Is(life.Close(), wantErr) {
				t.Error("lost cleanup error")
			}
		})
	}
	group.Wait()
	life.OnClose(func() { callbacks.Add(1) })
	// Assert.
	if releases.Load() != 1 || callbacks.Load() != 21 {
		t.Fatalf("release=%d callbacks=%d", releases.Load(), callbacks.Load())
	}
}

func TestFirstCompletedClosesAllFailedOwnedResults(t *testing.T) {
	// Arrange: each failed branch still owns a resource that must be released.
	failure := errors.New("provider failed with resource")
	var closes atomic.Int32
	contexts := make(chan context.Context, 2)
	branch := func(call RouteCall[int]) (BasicRouteResult[int], error) {
		contexts <- call.Context
		result := BasicHandled(1)
		result.Lifetime = NewLifetime(func() error { closes.Add(1); return nil })
		return result, failure
	}
	// Act.
	result, err := InvokeRouteHandler(t.Context(), 0, FirstCompleted(branch, branch))
	// Assert: no winner leaks a failed handle or leaves its branch context alive.
	if !errors.Is(err, failure) || result.HasPayload || closes.Load() != 2 {
		t.Fatalf("err=%v payload=%v closes=%d", err, result.HasPayload, closes.Load())
	}
	for range 2 {
		if !errors.Is((<-contexts).Err(), context.Canceled) {
			t.Fatal("failed branch context was retained")
		}
	}
}

func TestFirstCompletedLiveWinnerStillObservesParentCancellation(t *testing.T) {
	// Arrange: returning an owned winner must not cancel it, but parent cancellation must.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	winnerContext := make(chan context.Context, 1)
	var closes atomic.Int32
	branch := func(call RouteCall[int]) (BasicRouteResult[int], error) {
		winnerContext <- call.Context
		result := BasicHandled(1)
		result.Lifetime = NewLifetime(func() error { closes.Add(1); return nil })
		return result, nil
	}
	result, err := InvokeRouteHandler(ctx, 0, FirstCompleted(branch))
	if err != nil {
		t.Fatal(err)
	}
	ownedContext := <-winnerContext
	t.Cleanup(func() { _ = result.Lifetime.Close() })
	if ownedContext.Err() != nil || closes.Load() != 0 {
		t.Fatal("winner cancelled or closed at handler return")
	}
	// Act: cancel parent while the winner lifetime is still open.
	cancel()
	if !errors.Is(ownedContext.Err(), context.Canceled) || closes.Load() != 0 {
		t.Fatal("parent cancellation did not reach the still-open winner")
	}
	if err = result.Lifetime.Close(); err != nil {
		t.Fatal(err)
	}
	if err = result.Lifetime.Close(); err != nil {
		t.Fatal(err)
	}
	// Assert: cancellation propagated, and explicit cleanup still happens exactly once.
	if !errors.Is(ownedContext.Err(), context.Canceled) || closes.Load() != 1 {
		t.Fatalf("ctx=%v closes=%d", ownedContext.Err(), closes.Load())
	}
}

func TestRetryAndFallbackRespectCancellation(t *testing.T) {
	for _, delay := range []time.Duration{0, time.Second} {
		for _, before := range []bool{false, true} {
			// Arrange.
			ctx, cancel := context.WithCancel(t.Context())
			calls := 0
			base := FromFunc(func(context.Context, int) (int, error) {
				calls++
				cancel()
				return 0, errors.New("failed")
			})
			retry := ApplyRoute(base, RetryIf[int, BasicKind, BasicReason, int](3, delay,
				func(context.Context, int, error) bool { return true }))
			if before {
				cancel()
			}
			// Act.
			_, err := InvokeRouteHandler(ctx, 0, retry)
			// Assert.
			want := 1
			if before {
				want = 0
			}
			if calls != want || !errors.Is(err, context.Canceled) {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
			cancel()
		}
	}
	for _, predicate := range []bool{false, true} {
		// Arrange.
		ctx, cancel := context.WithCancel(t.Context())
		calls := 0
		primary := FromFunc(
			func(context.Context, int) (int, error) { calls++; cancel(); return 0, errors.New("failed") },
		)
		secondary := FromFunc(func(context.Context, int) (int, error) { calls++; return 1, nil })
		handler := Fallback(primary, secondary)
		if predicate {
			handler = PredicateFallback(primary, secondary, func(error) bool { return true })
		}
		// Act.
		_, err := InvokeRouteHandler(ctx, 0, handler)
		// Assert.
		if calls != 1 || !errors.Is(err, context.Canceled) {
			t.Fatalf("calls=%d err=%v", calls, err)
		}
		cancel()
	}
}

func TestTimeoutTransfersOwnedLifetime(t *testing.T) {
	// Arrange.
	var captured func() error
	base := func(call RouteCall[int]) (BasicRouteResult[int], error) {
		captured = call.Context.Err
		result := BasicHandled(1)
		result.Lifetime = NewLifetime(nil)
		return result, nil
	}
	handler := ApplyRoute(base, Timeout[int, BasicKind, BasicReason, int](time.Hour))
	// Act.
	result, err := InvokeRouteHandler(t.Context(), 0, handler)
	// Assert.
	if err != nil || captured() != nil {
		t.Fatalf("premature cancellation: %v", err)
	}
	result.Lifetime.Close()
	if !errors.Is(captured(), context.Canceled) {
		t.Fatal("owned lifetime did not cancel")
	}
}
