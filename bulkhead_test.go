package routery

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestBulkheadRejectsWhenFull(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	release := make(chan struct{})
	base := func(_ context.Context, _ int, rec ResultRecorder[int]) error {
		close(entered)
		<-release
		rec.Stop(1, "")
		return nil
	}

	handler := ApplyRoute(base, Bulkhead[int, int](1))

	var wg sync.WaitGroup
	wg.Go(func() {
		_, err := InvokeRouteHandler(context.Background(), 0, handler)
		if err != nil {
			t.Errorf("first call: %v", err)
		}
	})

	<-entered

	_, err := InvokeRouteHandler(context.Background(), 0, handler)
	if !errors.Is(err, ErrTooManyRequests) {
		t.Fatalf("want ErrTooManyRequests, got %v", err)
	}

	close(release)
	wg.Wait()
}

func TestBulkheadContextWinsRace(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	release := make(chan struct{})
	base := func(_ context.Context, _ int, rec ResultRecorder[int]) error {
		close(entered)
		<-release
		rec.Stop(1, "")
		return nil
	}

	handler := ApplyRoute(base, Bulkhead[int, int](1))

	var wg sync.WaitGroup
	wg.Go(func() {
		_, err := InvokeRouteHandler(context.Background(), 0, handler)
		if err != nil {
			t.Errorf("first: %v", err)
		}
	})

	<-entered

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := InvokeRouteHandler(ctx, 0, handler)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want canceled, got %v", err)
	}

	close(release)
	wg.Wait()
}

func TestBulkheadInvalidLimit(t *testing.T) {
	t.Parallel()

	base := FromFunc(func(context.Context, int) (int, error) { return 0, nil })
	_, err := InvokeRouteHandler(context.Background(), 0, ApplyRoute(base, Bulkhead[int, int](0)))
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestBulkheadNilNext(t *testing.T) {
	t.Parallel()

	_, err := InvokeRouteHandler(context.Background(), 0, ApplyRoute[int, int](nil, Bulkhead[int, int](1)))
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestBulkheadSupportsConcurrentCalls(t *testing.T) {
	t.Parallel()

	const (
		workers = 2 * 2 * 2 * 2
		calls   = 2
		limit   = 4
	)

	base := FromFunc(func(context.Context, int) (int, error) {
		return 1, nil
	})

	handler := ApplyRoute(base, Bulkhead[int, int](limit))

	var wg sync.WaitGroup
	errs := make(chan error, workers*calls)
	for range workers {
		wg.Go(func() {
			for range calls {
				_, err := InvokeRouteHandler(context.Background(), 0, handler)
				if err != nil && !errors.Is(err, ErrTooManyRequests) {
					errs <- err
				}
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBulkheadPartialRecorderDiscardedOnError(t *testing.T) {
	t.Parallel()

	handlerErr := errors.New("handler failed")
	base := func(_ context.Context, _ int, rec ResultRecorder[int]) error {
		rec.Stop(42, "handled")
		return handlerErr
	}

	handler := ApplyRoute(base, Bulkhead[int, int](2))
	outcome, err := InvokeRouteHandler(context.Background(), 0, handler)
	if !errors.Is(err, handlerErr) {
		t.Fatalf("got err %v; want %v", err, handlerErr)
	}
	if outcome.HasPayload {
		t.Fatal("expected no payload on error")
	}
	if outcome.Action != ActionAbort {
		t.Fatalf("got action %q; want abort", outcome.Action)
	}
}

func TestBulkheadAcquireAndExecute(t *testing.T) {
	t.Parallel()

	base := FromFunc(func(context.Context, int) (int, error) {
		return 42, nil
	})
	outcome, err := InvokeRouteHandler(context.Background(), 0, ApplyRoute(base, Bulkhead[int, int](2)))
	if err != nil || !outcome.HasPayload || outcome.Payload != 42 {
		t.Fatalf("payload=%d err=%v", outcome.Payload, err)
	}
}

func TestBulkheadTimeoutWaitingNotUsed(t *testing.T) {
	t.Parallel()

	// Semaphore is non-blocking on full: slow holder should not block second call.
	block := make(chan struct{})
	base := func(_ context.Context, _ int, _ ResultRecorder[int]) error {
		<-block
		return nil
	}

	handler := ApplyRoute(base, Bulkhead[int, int](1))

	done := make(chan struct{})
	go func() {
		_, _ = InvokeRouteHandler(context.Background(), 0, handler)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := InvokeRouteHandler(ctx, 0, handler)
	if !errors.Is(err, ErrTooManyRequests) {
		t.Fatalf("want ErrTooManyRequests, got %v", err)
	}

	close(block)
	<-done
}
