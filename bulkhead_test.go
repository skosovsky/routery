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
	base := ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		close(entered)
		<-release
		return 1, nil
	})

	executor := Apply(base, Bulkhead[int, int](1))

	var wg sync.WaitGroup
	wg.Go(func() {
		_, err := executor.Execute(context.Background(), 0)
		if err != nil {
			t.Errorf("first call: %v", err)
		}
	})

	<-entered

	_, err := executor.Execute(context.Background(), 0)
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
	base := ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		close(entered)
		<-release
		return 1, nil
	})

	executor := Apply(base, Bulkhead[int, int](1))

	var wg sync.WaitGroup
	wg.Go(func() {
		_, err := executor.Execute(context.Background(), 0)
		if err != nil {
			t.Errorf("first: %v", err)
		}
	})

	<-entered

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := executor.Execute(ctx, 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want canceled, got %v", err)
	}

	close(release)
	wg.Wait()
}

func TestBulkheadInvalidLimit(t *testing.T) {
	t.Parallel()

	base := ExecutorFunc[int, int](func(context.Context, int) (int, error) { return 0, nil })
	_, err := Bulkhead[int, int](0)(base).Execute(context.Background(), 0)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestBulkheadNilNext(t *testing.T) {
	t.Parallel()

	_, err := Bulkhead[int, int](1)(nil).Execute(context.Background(), 0)
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

	base := ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		return 1, nil
	})

	executor := Apply(base, Bulkhead[int, int](limit))

	var wg sync.WaitGroup
	errs := make(chan error, workers*calls)
	for range workers {
		wg.Go(func() {
			for range calls {
				_, err := executor.Execute(context.Background(), 0)
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

func TestBulkheadAcquireAndExecute(t *testing.T) {
	t.Parallel()

	base := ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		return 42, nil
	})
	res, err := Apply(base, Bulkhead[int, int](2)).Execute(context.Background(), 0)
	if err != nil || res != 42 {
		t.Fatalf("res=%d err=%v", res, err)
	}
}

func TestBulkheadTimeoutWaitingNotUsed(t *testing.T) {
	t.Parallel()

	// Semaphore is non-blocking on full: slow holder should not block second call.
	block := make(chan struct{})
	base := ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		<-block
		return 0, nil
	})

	executor := Apply(base, Bulkhead[int, int](1))

	done := make(chan struct{})
	go func() {
		_, _ = executor.Execute(context.Background(), 0)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := executor.Execute(ctx, 0)
	if !errors.Is(err, ErrTooManyRequests) {
		t.Fatalf("want ErrTooManyRequests, got %v", err)
	}

	close(block)
	<-done
}
