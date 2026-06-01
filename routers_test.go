package routery

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestFallbackReturnsPrimaryResultOnSuccess(t *testing.T) {
	t.Parallel()

	primaryCalls := 0
	secondaryCalls := 0

	primary := HandlerFunc[int, int](func(context.Context, int) (RouteResult[int], error) {
		primaryCalls++
		return Handled(1), nil
	})
	secondary := HandlerFunc[int, int](func(context.Context, int) (RouteResult[int], error) {
		secondaryCalls++
		return Handled(2), nil
	})

	result, err := Fallback(primary, secondary).Handle(context.Background(), 0)
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if result.Payload != 1 {
		t.Fatalf("unexpected result: got %d, want 1", result.Payload)
	}
	if primaryCalls != 1 {
		t.Fatalf("unexpected primary call count: got %d, want 1", primaryCalls)
	}
	if secondaryCalls != 0 {
		t.Fatalf("unexpected secondary call count: got %d, want 0", secondaryCalls)
	}
}

func TestFallbackUsesSecondaryOnPrimaryError(t *testing.T) {
	t.Parallel()

	primaryErr := errors.New("primary failed")
	primary := HandlerFunc[int, int](func(context.Context, int) (RouteResult[int], error) {
		return zeroRouteResult[int](), primaryErr
	})
	secondary := HandlerFunc[int, int](func(context.Context, int) (RouteResult[int], error) {
		return Handled(2), nil
	})

	result, err := Fallback(primary, secondary).Handle(context.Background(), 0)
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if result.Payload != 2 {
		t.Fatalf("unexpected result: got %d, want 2", result.Payload)
	}
}

func TestFallbackReturnsConfigErrorForInvalidConfig(t *testing.T) {
	t.Parallel()

	_, err := Fallback[int, int](nil, nil).Handle(context.Background(), 0)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestRetryIfRetriesWhenPredicateMatches(t *testing.T) {
	t.Parallel()

	const attemptsCount = 3

	retryErr := errors.New("retry")
	calls := 0

	base := HandlerFunc[int, int](func(context.Context, int) (RouteResult[int], error) {
		calls++
		if calls < attemptsCount {
			return zeroRouteResult[int](), retryErr
		}
		return Handled(1), nil
	})

	executor := RetryIf[int, int](attemptsCount, 0, func(_ context.Context, _ int, err error) bool {
		return errors.Is(err, retryErr)
	})(base)

	result, err := executor.Handle(context.Background(), 0)
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if result.Payload != 1 {
		t.Fatalf("unexpected result: got %d, want 1", result.Payload)
	}
	if calls != attemptsCount {
		t.Fatalf("unexpected call count: got %d, want %d", calls, attemptsCount)
	}
}

func TestRetryIfStopsWhenPredicateMisses(t *testing.T) {
	t.Parallel()

	calls := 0
	baseErr := errors.New("stop")
	base := HandlerFunc[int, int](func(context.Context, int) (RouteResult[int], error) {
		calls++
		return zeroRouteResult[int](), baseErr
	})

	executor := RetryIf[int, int](2+1, 0, func(context.Context, int, error) bool {
		return false
	})(base)

	_, err := executor.Handle(context.Background(), 0)
	if !errors.Is(err, baseErr) {
		t.Fatalf("expected base error, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("unexpected call count: got %d, want 1", calls)
	}
}

func TestRetryIfNormalizesAttemptsToOne(t *testing.T) {
	t.Parallel()

	calls := 0
	baseErr := errors.New("single")
	base := HandlerFunc[int, int](func(context.Context, int) (RouteResult[int], error) {
		calls++
		return zeroRouteResult[int](), baseErr
	})

	executor := RetryIf[int, int](0, 0, func(context.Context, int, error) bool {
		return true
	})(base)

	_, err := executor.Handle(context.Background(), 0)
	if !errors.Is(err, baseErr) {
		t.Fatalf("expected base error, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("unexpected call count: got %d, want 1", calls)
	}
}

func TestRetryIfReturnsConfigErrorForNilPredicate(t *testing.T) {
	t.Parallel()

	base := HandlerFunc[int, int](func(context.Context, int) (RouteResult[int], error) {
		return Handled(1), nil
	})

	executor := RetryIf[int, int](1, 0, nil)(base)
	_, err := executor.Handle(context.Background(), 0)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestGrowBackoffEqualJitterWithinWindow(t *testing.T) {
	t.Parallel()

	const base = 50 * time.Millisecond
	for range 500 {
		next := growBackoff(base)
		doubled := safeDoubleBackoff(base)
		half := doubled / 2
		if half <= 0 {
			if next != doubled {
				t.Fatalf("expected next=%v for tiny half, got %v", doubled, next)
			}
			continue
		}
		if next < half || next >= doubled {
			t.Fatalf("growBackoff(%v)=%v want [%v,%v)", base, next, half, doubled)
		}
	}
}

func TestGrowBackoffNoOverflowRegression(t *testing.T) {
	t.Parallel()

	huge := time.Duration(1 << 62)
	next := growBackoff(huge)
	if next <= 0 {
		t.Fatalf("expected positive duration, got %v", next)
	}
}

func TestRetryIfHonorsContextCancellationDuringBackoff(t *testing.T) {
	t.Parallel()

	const (
		timeout = 16 * time.Millisecond
		backoff = 64 * time.Millisecond
	)

	base := HandlerFunc[int, int](func(context.Context, int) (RouteResult[int], error) {
		return zeroRouteResult[int](), errors.New("retry")
	})

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	start := time.Now()
	_, err := RetryIf[int, int](2+1, backoff, func(context.Context, int, error) bool {
		return true
	})(base).Handle(ctx, 0)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}
	if elapsed >= backoff {
		t.Fatalf("expected early return before backoff elapsed, elapsed=%v backoff=%v", elapsed, backoff)
	}
}

func TestRoundRobinDistributesSequentially(t *testing.T) {
	t.Parallel()

	const calls = 2 * (2 + 1)

	executor := RoundRobin[int, int](
		HandlerFunc[int, int](func(context.Context, int) (RouteResult[int], error) { return Handled(1), nil }),
		HandlerFunc[int, int](func(context.Context, int) (RouteResult[int], error) { return Handled(2), nil }),
		HandlerFunc[int, int](func(context.Context, int) (RouteResult[int], error) { return Handled(2 + 1), nil }),
	)

	got := make([]int, 0, calls)
	for range calls {
		res, err := executor.Handle(context.Background(), 0)
		if err != nil {
			t.Fatalf("execute returned unexpected error: %v", err)
		}
		got = append(got, res.Payload)
	}

	want := []int{1, 2, 3, 1, 2, 3}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("unexpected sequence at index %d: got %d, want %d", index, got[index], want[index])
		}
	}
}

func TestRoundRobinReturnsSentinelErrors(t *testing.T) {
	t.Parallel()

	_, err := RoundRobin[int, int]().Handle(context.Background(), 0)
	if !errors.Is(err, ErrNoHandlers) {
		t.Fatalf("expected ErrNoHandlers, got %v", err)
	}

	_, err = RoundRobin[int, int](
		HandlerFunc[int, int](func(context.Context, int) (RouteResult[int], error) { return Handled(1), nil }),
		nil,
	).Handle(context.Background(), 0)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestRoundRobinSupportsConcurrentCalls(t *testing.T) {
	t.Parallel()

	const (
		workers = 2 * 2 * 2 * 2
		calls   = 2
	)

	executor := RoundRobin[int, int](
		HandlerFunc[int, int](func(context.Context, int) (RouteResult[int], error) { return Handled(1), nil }),
		HandlerFunc[int, int](func(context.Context, int) (RouteResult[int], error) { return Handled(2), nil }),
	)

	var group sync.WaitGroup
	errs := make(chan error, workers*calls)

	for range workers {
		group.Go(func() {
			for range calls {
				_, err := executor.Handle(context.Background(), 0)
				if err != nil {
					errs <- err
				}
			}
		})
	}

	group.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("unexpected error during concurrent execution: %v", err)
	}
}

func TestTimeoutEnforcesDeadline(t *testing.T) {
	t.Parallel()

	const timeout = 16 * time.Millisecond

	base := HandlerFunc[int, int](func(ctx context.Context, _ int) (RouteResult[int], error) {
		<-ctx.Done()
		return zeroRouteResult[int](), ctx.Err()
	})

	start := time.Now()
	_, err := Timeout[int, int](timeout)(base).Handle(context.Background(), 0)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}
	if elapsed < timeout {
		t.Fatalf("expected elapsed >= timeout, elapsed=%v timeout=%v", elapsed, timeout)
	}
}

func TestTimeoutRespectsEarlierParentDeadline(t *testing.T) {
	t.Parallel()

	const (
		parentTimeout = 12 * time.Millisecond
		mwTimeout     = 80 * time.Millisecond
	)

	base := HandlerFunc[int, int](func(ctx context.Context, _ int) (RouteResult[int], error) {
		<-ctx.Done()
		return zeroRouteResult[int](), ctx.Err()
	})

	ctx, cancel := context.WithTimeout(context.Background(), parentTimeout)
	defer cancel()

	start := time.Now()
	_, err := Timeout[int, int](mwTimeout)(base).Handle(ctx, 0)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}
	if elapsed >= mwTimeout {
		t.Fatalf("expected parent deadline to win, elapsed=%v middleware-timeout=%v", elapsed, mwTimeout)
	}
}

func TestTimeoutReturnsIdentityWhenDurationIsNonPositive(t *testing.T) {
	t.Parallel()

	base := HandlerFunc[int, int](func(context.Context, int) (RouteResult[int], error) {
		return Handled(2 + 2), nil
	})

	result, err := Timeout[int, int](0)(base).Handle(context.Background(), 0)
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if result.Payload != 2+2 {
		t.Fatalf("unexpected result: got %d, want 4", result.Payload)
	}
}

func TestRetryIfDoesNotRetryOnRouteStatusNext(t *testing.T) {
	t.Parallel()

	calls := 0
	base := HandlerFunc[int, int](func(context.Context, int) (RouteResult[int], error) {
		calls++
		return Next[int]("delegate"), nil
	})

	executor := RetryIf[int, int](5, 0, func(context.Context, int, error) bool {
		return true
	})(base)

	result, err := executor.Handle(context.Background(), 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != StatusNext || calls != 1 {
		t.Fatalf("got status=%s calls=%d, want next/1", result.Status, calls)
	}
}

func TestRetryIfDoesNotRetryOnRouteStatusIgnored(t *testing.T) {
	t.Parallel()

	calls := 0
	base := HandlerFunc[int, int](func(context.Context, int) (RouteResult[int], error) {
		calls++
		return Ignored[int]("skip"), nil
	})

	executor := RetryIf[int, int](5, 0, func(context.Context, int, error) bool {
		return true
	})(base)

	result, err := executor.Handle(context.Background(), 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != StatusIgnored || calls != 1 {
		t.Fatalf("got status=%s calls=%d, want ignored/1", result.Status, calls)
	}
}

func TestTimeoutReturnsConfigErrorWhenNextIsNil(t *testing.T) {
	t.Parallel()

	_, err := Timeout[int, int](time.Second)(nil).Handle(context.Background(), 0)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}
