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
	base := FromFunc(func(context.Context, int) (int, error) {
		calls.Add(1)
		return 0, fail
	})

	handler := ApplyRoute(base, CircuitBreaker[int, int](2, time.Hour, nil))

	_, err := InvokeRouteHandler(context.Background(), 0, handler)
	if !errors.Is(err, fail) {
		t.Fatalf("first call: want %v, got %v", fail, err)
	}
	_, err = InvokeRouteHandler(context.Background(), 0, handler)
	if !errors.Is(err, fail) {
		t.Fatalf("second call: want %v, got %v", fail, err)
	}
	outcome, err := InvokeRouteHandler(context.Background(), 0, handler)
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("third call: want ErrCircuitOpen, got %v", err)
	}
	if outcome.Action != ActionAbort {
		t.Fatalf("got action %q; want abort on circuit open", outcome.Action)
	}
	if calls.Load() != 2 {
		t.Fatalf("unexpected base calls: got %d, want 2", calls.Load())
	}
}

func TestCircuitBreakerSuccessResetsFailures(t *testing.T) {
	t.Parallel()

	fail := errors.New("fail")
	attempt := atomic.Int32{}
	base := FromFunc(func(context.Context, int) (int, error) {
		if attempt.Add(1) == 1 {
			return 0, fail
		}
		return 1, nil
	})

	handler := ApplyRoute(base, CircuitBreaker[int, int](2, time.Hour, nil))

	_, err := InvokeRouteHandler(context.Background(), 0, handler)
	if !errors.Is(err, fail) {
		t.Fatalf("first: %v", err)
	}
	outcome, err := InvokeRouteHandler(context.Background(), 0, handler)
	if err != nil || !outcome.HasPayload || outcome.Payload != 1 {
		t.Fatalf("second: payload=%d err=%v", outcome.Payload, err)
	}
	outcome, err = InvokeRouteHandler(context.Background(), 0, handler)
	if err != nil || !outcome.HasPayload || outcome.Payload != 1 {
		t.Fatalf("third: payload=%d err=%v", outcome.Payload, err)
	}
}

func TestCircuitBreakerHalfOpenCanceledProbeAllowsNextProbe(t *testing.T) {
	t.Parallel()

	fail := errors.New("fail")
	calls := atomic.Int32{}
	probeCtx, cancelProbe := context.WithCancel(context.Background())

	base := func(ctx context.Context, _ int, rec ResultRecorder[int]) error {
		n := calls.Add(1)
		switch n {
		case 1:
			return fail
		case 2:
			cancelProbe()
			<-ctx.Done()
			return ctx.Err()
		default:
			rec.Stop(42, "")
			return nil
		}
	}

	handler := ApplyRoute(base, CircuitBreaker[int, int](1, 10*time.Millisecond, nil))

	_, err := InvokeRouteHandler(context.Background(), 0, handler)
	if !errors.Is(err, fail) {
		t.Fatalf("first: want %v, got %v", fail, err)
	}
	_, err = InvokeRouteHandler(context.Background(), 0, handler)
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("while open: want ErrCircuitOpen, got %v", err)
	}

	time.Sleep(12 * time.Millisecond)

	_, err = InvokeRouteHandler(probeCtx, 0, handler)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled probe: want context.Canceled, got %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("after canceled probe: want 2 base calls, got %d", calls.Load())
	}

	outcome, err := InvokeRouteHandler(context.Background(), 0, handler)
	if err != nil || !outcome.HasPayload || outcome.Payload != 42 {
		t.Fatalf("second probe after cancel: payload=%d err=%v (want HalfOpen → new probe)", outcome.Payload, err)
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

	base := func(ctx context.Context, _ int, _ ResultRecorder[int]) error {
		n := calls.Add(1)
		switch n {
		case 1:
			return fail
		case 2:
			close(probeStarted)
			cancelProbe()
			<-probeMayFinish
			<-ctx.Done()
			return ctx.Err()
		default:
			return errors.New("unexpected base call")
		}
	}

	handler := ApplyRoute(base, CircuitBreaker[int, int](1, 10*time.Millisecond, nil))

	_, _ = InvokeRouteHandler(context.Background(), 0, handler)
	_, err := InvokeRouteHandler(context.Background(), 0, handler)
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("while open: %v", err)
	}
	time.Sleep(12 * time.Millisecond)

	var wg sync.WaitGroup
	wg.Go(func() {
		_, err := InvokeRouteHandler(probeCtx, 0, handler)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("probe: want context.Canceled, got %v", err)
		}
	})

	<-probeStarted

	errCh := make(chan error, 1)
	wg.Go(func() {
		_, e := InvokeRouteHandler(context.Background(), 0, handler)
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
	base := FromFunc(func(context.Context, int) (int, error) {
		c := calls.Add(1)
		if c <= 2 {
			return 0, fail
		}
		return 42, nil
	})

	handler := ApplyRoute(base, CircuitBreaker[int, int](2, 10*time.Millisecond, nil))

	_, _ = InvokeRouteHandler(context.Background(), 0, handler)
	_, _ = InvokeRouteHandler(context.Background(), 0, handler)
	_, err := InvokeRouteHandler(context.Background(), 0, handler)
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected open: %v", err)
	}

	time.Sleep(12 * time.Millisecond)

	outcome, err := InvokeRouteHandler(context.Background(), 0, handler)
	if err != nil || !outcome.HasPayload || outcome.Payload != 42 {
		t.Fatalf("probe: payload=%d err=%v", outcome.Payload, err)
	}

	outcome, err = InvokeRouteHandler(context.Background(), 0, handler)
	if err != nil || !outcome.HasPayload || outcome.Payload != 42 {
		t.Fatalf("after close: payload=%d err=%v", outcome.Payload, err)
	}
}

func TestCircuitBreakerHalfOpenOnlyOneProbeConcurrent(t *testing.T) {
	t.Parallel()

	fail := errors.New("fail")
	releaseProbe := make(chan struct{})
	calls := atomic.Int32{}

	base := func(_ context.Context, _ int, rec ResultRecorder[int]) error {
		c := calls.Add(1)
		if c == 1 {
			return fail
		}
		<-releaseProbe
		rec.Stop(1, "")
		return nil
	}

	handler := ApplyRoute(base, CircuitBreaker[int, int](1, time.Millisecond, func(error) bool { return true }))

	_, err := InvokeRouteHandler(context.Background(), 0, handler)
	if !errors.Is(err, fail) {
		t.Fatalf("expected first failure, got %v", err)
	}

	_, err = InvokeRouteHandler(context.Background(), 0, handler)
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected open before reset, got %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	const workers = 32
	errCh := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			_, e := InvokeRouteHandler(context.Background(), 0, handler)
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
	base := FromFunc(func(context.Context, int) (int, error) {
		return 0, fail
	})

	handler := ApplyRoute(base, CircuitBreaker[int, int](10, time.Hour, nil))

	var wg sync.WaitGroup
	errs := make(chan error, workers*calls)
	for range workers {
		wg.Go(func() {
			for range calls {
				_, err := InvokeRouteHandler(context.Background(), 0, handler)
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

	base := FromFunc(func(context.Context, int) (int, error) { return 0, nil })
	handler := ApplyRoute(base, CircuitBreaker[int, int](0, time.Second, nil))
	_, err := InvokeRouteHandler(context.Background(), 0, handler)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestCircuitBreakerNegativeResetTimeout(t *testing.T) {
	t.Parallel()

	base := FromFunc(func(context.Context, int) (int, error) { return 0, nil })
	handler := ApplyRoute(base, CircuitBreaker[int, int](1, -time.Second, nil))
	_, err := InvokeRouteHandler(context.Background(), 0, handler)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestCircuitBreakerNilNext(t *testing.T) {
	t.Parallel()

	handler := ApplyRoute[int, int](nil, CircuitBreaker[int, int](1, time.Second, nil))
	_, err := InvokeRouteHandler(context.Background(), 0, handler)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestCircuitBreakerIsFailureSkipsWhenPredicateReturnsFalse(t *testing.T) {
	t.Parallel()

	noise := errors.New("noise")
	calls := atomic.Int32{}
	base := FromFunc(func(context.Context, int) (int, error) {
		calls.Add(1)
		return 0, noise
	})

	handler := ApplyRoute(base, CircuitBreaker[int, int](2, time.Hour, func(error) bool { return false }))

	for range 5 {
		_, err := InvokeRouteHandler(context.Background(), 0, handler)
		if !errors.Is(err, noise) {
			t.Fatalf("expected noise error, got %v", err)
		}
	}
	_, err := InvokeRouteHandler(context.Background(), 0, handler)
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
	base := FromFunc(func(context.Context, int) (int, error) {
		c := calls.Add(1)
		if c == 1 {
			return 0, benign
		}
		return 0, fatal
	})

	handler := ApplyRoute(base, CircuitBreaker[int, int](2, time.Hour, func(e error) bool {
		return errors.Is(e, fatal)
	}))

	_, err := InvokeRouteHandler(context.Background(), 0, handler)
	if !errors.Is(err, benign) {
		t.Fatalf("first: %v", err)
	}
	_, err = InvokeRouteHandler(context.Background(), 0, handler)
	if !errors.Is(err, fatal) {
		t.Fatalf("second: %v", err)
	}
	_, err = InvokeRouteHandler(context.Background(), 0, handler)
	if !errors.Is(err, fatal) {
		t.Fatalf("third: %v", err)
	}
	_, err = InvokeRouteHandler(context.Background(), 0, handler)
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("fourth: %v", err)
	}
}

func TestCircuitBreakerIgnoresActionNextAndIgnore(t *testing.T) {
	t.Parallel()

	fail := errors.New("fail")
	calls := atomic.Int32{}
	base := func(_ context.Context, _ int, rec ResultRecorder[int]) error {
		c := calls.Add(1)
		switch {
		case c <= 2:
			rec.Next("delegate")
			return nil
		case c == 3:
			rec.Ignore("skip")
			return nil
		default:
			return fail
		}
	}

	handler := ApplyRoute(base, CircuitBreaker[int, int](1, time.Hour, nil))

	for i := range 3 {
		rec := NewResultRecorder[int]()
		if err := handler(context.Background(), 0, rec); err != nil {
			t.Fatalf("call %d: business disposition should not error: %v", i+1, err)
		}
		switch i {
		case 0, 1:
			if rec.Action() != ActionNext {
				t.Fatalf("call %d: want ActionNext, got %s", i+1, rec.Action())
			}
		case 2:
			if rec.Action() != ActionStop {
				t.Fatalf("call %d: want ActionStop (ignore), got %s", i+1, rec.Action())
			}
			if _, ok := rec.Payload(); ok {
				t.Fatalf("call %d: ignore should not set payload", i+1)
			}
		}
	}

	_, err := InvokeRouteHandler(context.Background(), 0, handler)
	if !errors.Is(err, fail) {
		t.Fatalf("fourth call: want %v, got %v", fail, err)
	}
	_, err = InvokeRouteHandler(context.Background(), 0, handler)
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("fifth call: want ErrCircuitOpen, got %v", err)
	}
	if calls.Load() != 4 {
		t.Fatalf("unexpected call count: got %d, want 4", calls.Load())
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
		base := FromFunc(func(context.Context, int) (int, error) {
			i := idx.Add(1) - 1
			bit := (pattern >> (i % 8)) & 1
			if bit == 0 {
				return 1, nil
			}
			return 0, errors.New("fail")
		})

		handler := ApplyRoute(base, CircuitBreaker[int, int](th, reset, nil))
		for range 20 {
			_, _ = InvokeRouteHandler(context.Background(), 0, handler)
		}
	})
}
