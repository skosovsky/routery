package routery

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCircuitBreakerOpensAfterFailures(t *testing.T) {
	t.Parallel()

	fail := errors.New("fail")
	calls := atomic.Int32{}
	base := ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		calls.Add(1)
		return 0, fail
	})

	executor := Apply(base, CircuitBreaker[int, int](2, time.Hour, nil))

	_, err := executor.Execute(context.Background(), 0)
	if !errors.Is(err, fail) {
		t.Fatalf("first call: want %v, got %v", fail, err)
	}
	_, err = executor.Execute(context.Background(), 0)
	if !errors.Is(err, fail) {
		t.Fatalf("second call: want %v, got %v", fail, err)
	}
	_, err = executor.Execute(context.Background(), 0)
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("third call: want ErrCircuitOpen, got %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("unexpected base calls: got %d, want 2", calls.Load())
	}
}

func TestCircuitBreakerSuccessResetsFailures(t *testing.T) {
	t.Parallel()

	fail := errors.New("fail")
	attempt := atomic.Int32{}
	base := ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		if attempt.Add(1) == 1 {
			return 0, fail
		}
		return 1, nil
	})

	executor := Apply(base, CircuitBreaker[int, int](2, time.Hour, nil))

	_, err := executor.Execute(context.Background(), 0)
	if !errors.Is(err, fail) {
		t.Fatalf("first: %v", err)
	}
	res, err := executor.Execute(context.Background(), 0)
	if err != nil || res != 1 {
		t.Fatalf("second: res=%d err=%v", res, err)
	}
	res, err = executor.Execute(context.Background(), 0)
	if err != nil || res != 1 {
		t.Fatalf("third: res=%d err=%v", res, err)
	}
}

func TestCircuitBreakerHalfOpenCanceledProbeAllowsNextProbe(t *testing.T) {
	t.Parallel()

	fail := errors.New("fail")
	calls := atomic.Int32{}
	probeCtx, cancelProbe := context.WithCancel(context.Background())

	base := ExecutorFunc[int, int](func(ctx context.Context, _ int) (int, error) {
		n := calls.Add(1)
		switch n {
		case 1:
			return 0, fail
		case 2:
			cancelProbe()
			<-ctx.Done()
			return 0, ctx.Err()
		default:
			return 42, nil
		}
	})

	executor := Apply(base, CircuitBreaker[int, int](1, 10*time.Millisecond, nil))

	_, err := executor.Execute(context.Background(), 0)
	if !errors.Is(err, fail) {
		t.Fatalf("first: want %v, got %v", fail, err)
	}
	_, err = executor.Execute(context.Background(), 0)
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("while open: want ErrCircuitOpen, got %v", err)
	}

	time.Sleep(12 * time.Millisecond)

	_, err = executor.Execute(probeCtx, 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled probe: want context.Canceled, got %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("after canceled probe: want 2 base calls, got %d", calls.Load())
	}

	res, err := executor.Execute(context.Background(), 0)
	if err != nil || res != 42 {
		t.Fatalf("second probe after cancel: res=%d err=%v (want HalfOpen → new probe)", res, err)
	}
	if calls.Load() != 3 {
		t.Fatalf("want 3 base calls, got %d", calls.Load())
	}
}

func TestCircuitBreakerHalfOpenParallelWhileProbeInFlightErrCircuitOpen(t *testing.T) {
	t.Parallel()

	fail := errors.New("fail")
	calls := atomic.Int32{}
	probeStarted := make(chan struct{})
	probeMayFinish := make(chan struct{})

	probeCtx, cancelProbe := context.WithCancel(context.Background())

	base := ExecutorFunc[int, int](func(ctx context.Context, _ int) (int, error) {
		n := calls.Add(1)
		switch n {
		case 1:
			return 0, fail
		case 2:
			close(probeStarted)
			cancelProbe()
			<-probeMayFinish
			<-ctx.Done()
			return 0, ctx.Err()
		default:
			return 0, errors.New("unexpected base call")
		}
	})

	executor := Apply(base, CircuitBreaker[int, int](1, 10*time.Millisecond, nil))

	_, _ = executor.Execute(context.Background(), 0)
	_, err := executor.Execute(context.Background(), 0)
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("while open: %v", err)
	}
	time.Sleep(12 * time.Millisecond)

	var wg sync.WaitGroup
	wg.Go(func() {
		_, err := executor.Execute(probeCtx, 0)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("probe: want context.Canceled, got %v", err)
		}
	})

	<-probeStarted

	errCh := make(chan error, 1)
	wg.Go(func() {
		_, e := executor.Execute(context.Background(), 0)
		errCh <- e
	})

	select {
	case e := <-errCh:
		if !errors.Is(e, ErrCircuitOpen) {
			t.Fatalf("parallel during HalfOpen probe: want ErrCircuitOpen, got %v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for parallel ErrCircuitOpen")
	}

	close(probeMayFinish)

	wg.Wait()
	if calls.Load() != 2 {
		t.Fatalf("want 2 base calls, got %d", calls.Load())
	}
}

func TestCircuitBreakerHalfOpenSuccessCloses(t *testing.T) {
	t.Parallel()

	fail := errors.New("fail")
	calls := atomic.Int32{}
	base := ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		c := calls.Add(1)
		if c <= 2 {
			return 0, fail
		}
		return 42, nil
	})

	executor := Apply(base, CircuitBreaker[int, int](2, 10*time.Millisecond, nil))

	_, _ = executor.Execute(context.Background(), 0)
	_, _ = executor.Execute(context.Background(), 0)
	_, err := executor.Execute(context.Background(), 0)
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected open: %v", err)
	}

	time.Sleep(12 * time.Millisecond)

	res, err := executor.Execute(context.Background(), 0)
	if err != nil || res != 42 {
		t.Fatalf("probe: res=%d err=%v", res, err)
	}

	res, err = executor.Execute(context.Background(), 0)
	if err != nil || res != 42 {
		t.Fatalf("after close: res=%d err=%v", res, err)
	}
}

func TestCircuitBreakerHalfOpenOnlyOneProbeConcurrent(t *testing.T) {
	t.Parallel()

	fail := errors.New("fail")
	releaseProbe := make(chan struct{})
	calls := atomic.Int32{}

	base := ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		c := calls.Add(1)
		if c == 1 {
			return 0, fail
		}
		<-releaseProbe
		return 1, nil
	})

	executor := Apply(base, CircuitBreaker[int, int](1, time.Millisecond, func(error) bool { return true }))

	_, err := executor.Execute(context.Background(), 0)
	if !errors.Is(err, fail) {
		t.Fatalf("expected first failure, got %v", err)
	}

	_, err = executor.Execute(context.Background(), 0)
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected open before reset, got %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	const workers = 32
	errCh := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			_, e := executor.Execute(context.Background(), 0)
			errCh <- e
		})
	}

	for range workers - 1 {
		select {
		case e := <-errCh:
			if !errors.Is(e, ErrCircuitOpen) {
				t.Fatalf("expected ErrCircuitOpen, got %v", e)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("timeout waiting for fail-fast results")
		}
	}

	close(releaseProbe)

	select {
	case e := <-errCh:
		if e != nil {
			t.Fatalf("expected successful probe, got %v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for probe result")
	}

	wg.Wait()
	if got := calls.Load(); got != 2 {
		t.Fatalf("expected exactly 2 base calls (fail + probe), got %d", got)
	}
}

func TestCircuitBreakerConcurrentClosed(t *testing.T) {
	t.Parallel()

	const (
		workers = 2 * 2 * 2 * 2
		calls   = 2
	)

	fail := errors.New("fail")
	base := ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		return 0, fail
	})

	executor := Apply(base, CircuitBreaker[int, int](10, time.Hour, nil))

	var wg sync.WaitGroup
	errs := make(chan error, workers*calls)
	for range workers {
		wg.Go(func() {
			for range calls {
				_, err := executor.Execute(context.Background(), 0)
				if err != nil && !errors.Is(err, fail) && !errors.Is(err, ErrCircuitOpen) {
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

func TestCircuitBreakerInvalidThreshold(t *testing.T) {
	t.Parallel()

	base := ExecutorFunc[int, int](func(context.Context, int) (int, error) { return 0, nil })
	_, err := CircuitBreaker[int, int](0, time.Second, nil)(base).Execute(context.Background(), 0)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestCircuitBreakerNegativeResetTimeout(t *testing.T) {
	t.Parallel()

	base := ExecutorFunc[int, int](func(context.Context, int) (int, error) { return 0, nil })
	_, err := CircuitBreaker[int, int](1, -time.Second, nil)(base).Execute(context.Background(), 0)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestCircuitBreakerNilNext(t *testing.T) {
	t.Parallel()

	_, err := CircuitBreaker[int, int](1, time.Second, nil)(nil).Execute(context.Background(), 0)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestCircuitBreakerIsFailureSkipsWhenPredicateReturnsFalse(t *testing.T) {
	t.Parallel()

	noise := errors.New("noise")
	calls := atomic.Int32{}
	base := ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		calls.Add(1)
		return 0, noise
	})

	executor := Apply(base, CircuitBreaker[int, int](2, time.Hour, func(error) bool { return false }))

	for range 5 {
		_, err := executor.Execute(context.Background(), 0)
		if !errors.Is(err, noise) {
			t.Fatalf("expected noise error, got %v", err)
		}
	}
	_, err := executor.Execute(context.Background(), 0)
	if !errors.Is(err, noise) {
		t.Fatalf("circuit should stay closed: %v", err)
	}
	if calls.Load() != 6 {
		t.Fatalf("unexpected call count: %d", calls.Load())
	}
}

func TestCircuitBreakerIsFailurePredicate(t *testing.T) {
	t.Parallel()

	benign := errors.New("benign")
	fatal := errors.New("fatal")
	calls := atomic.Int32{}
	base := ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		c := calls.Add(1)
		if c == 1 {
			return 0, benign
		}
		return 0, fatal
	})

	executor := Apply(base, CircuitBreaker[int, int](2, time.Hour, func(e error) bool {
		return errors.Is(e, fatal)
	}))

	_, err := executor.Execute(context.Background(), 0)
	if !errors.Is(err, benign) {
		t.Fatalf("first: %v", err)
	}
	_, err = executor.Execute(context.Background(), 0)
	if !errors.Is(err, fatal) {
		t.Fatalf("second: %v", err)
	}
	_, err = executor.Execute(context.Background(), 0)
	if !errors.Is(err, fatal) {
		t.Fatalf("third: %v", err)
	}
	_, err = executor.Execute(context.Background(), 0)
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("fourth: %v", err)
	}
}

func FuzzCircuitBreakerStateMachine(f *testing.F) {
	f.Add(int64(1), int64(0), int64(2))
	f.Add(int64(3), int64(1), int64(0))

	f.Fuzz(func(t *testing.T, threshold int64, resetMillis int64, pattern int64) {
		t.Helper()

		th := max(1, min(int(threshold), 8))
		reset := time.Duration(max(0, resetMillis%20)) * time.Millisecond

		var idx atomic.Int32
		base := ExecutorFunc[int, int](func(context.Context, int) (int, error) {
			i := idx.Add(1) - 1
			bit := (pattern >> (i % 8)) & 1
			if bit == 0 {
				return 1, nil
			}
			return 0, errors.New("fail")
		})

		executor := Apply(base, CircuitBreaker[int, int](th, reset, nil))
		for range 20 {
			_, _ = executor.Execute(context.Background(), 0)
		}
	})
}
