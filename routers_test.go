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

	primary := ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		primaryCalls++
		return 1, nil
	})
	secondary := ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		secondaryCalls++
		return 2, nil
	})

	result, err := Fallback(primary, secondary).Execute(context.Background(), 0)
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if result != 1 {
		t.Fatalf("unexpected result: got %d, want 1", result)
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
	primary := ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		return 0, primaryErr
	})
	secondary := ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		return 2, nil
	})

	result, err := Fallback(primary, secondary).Execute(context.Background(), 0)
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if result != 2 {
		t.Fatalf("unexpected result: got %d, want 2", result)
	}
}

func TestFallbackReturnsConfigErrorForInvalidConfig(t *testing.T) {
	t.Parallel()

	_, err := Fallback[int, int](nil, nil).Execute(context.Background(), 0)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestRetryIfRetriesWhenPredicateMatches(t *testing.T) {
	t.Parallel()

	const attemptsCount = 3

	retryErr := errors.New("retry")
	calls := 0

	base := ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		calls++
		if calls < attemptsCount {
			return 0, retryErr
		}
		return 1, nil
	})

	executor := RetryIf[int, int](attemptsCount, 0, func(err error) bool {
		return errors.Is(err, retryErr)
	})(base)

	result, err := executor.Execute(context.Background(), 0)
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if result != 1 {
		t.Fatalf("unexpected result: got %d, want 1", result)
	}
	if calls != attemptsCount {
		t.Fatalf("unexpected call count: got %d, want %d", calls, attemptsCount)
	}
}

func TestRetryIfStopsWhenPredicateMisses(t *testing.T) {
	t.Parallel()

	calls := 0
	baseErr := errors.New("stop")
	base := ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		calls++
		return 0, baseErr
	})

	executor := RetryIf[int, int](2+1, 0, func(error) bool {
		return false
	})(base)

	_, err := executor.Execute(context.Background(), 0)
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
	base := ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		calls++
		return 0, baseErr
	})

	executor := RetryIf[int, int](0, 0, func(error) bool {
		return true
	})(base)

	_, err := executor.Execute(context.Background(), 0)
	if !errors.Is(err, baseErr) {
		t.Fatalf("expected base error, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("unexpected call count: got %d, want 1", calls)
	}
}

func TestRetryIfReturnsConfigErrorForNilPredicate(t *testing.T) {
	t.Parallel()

	base := ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		return 1, nil
	})

	executor := RetryIf[int, int](1, 0, nil)(base)
	_, err := executor.Execute(context.Background(), 0)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestRetryIfHonorsContextCancellationDuringBackoff(t *testing.T) {
	t.Parallel()

	const (
		timeout = 16 * time.Millisecond
		backoff = 64 * time.Millisecond
	)

	base := ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		return 0, errors.New("retry")
	})

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	start := time.Now()
	_, err := RetryIf[int, int](2+1, backoff, func(error) bool {
		return true
	})(base).Execute(ctx, 0)
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
		ExecutorFunc[int, int](func(context.Context, int) (int, error) { return 1, nil }),
		ExecutorFunc[int, int](func(context.Context, int) (int, error) { return 2, nil }),
		ExecutorFunc[int, int](func(context.Context, int) (int, error) { return 2 + 1, nil }),
	)

	got := make([]int, 0, calls)
	for range calls {
		res, err := executor.Execute(context.Background(), 0)
		if err != nil {
			t.Fatalf("execute returned unexpected error: %v", err)
		}
		got = append(got, res)
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

	_, err := RoundRobin[int, int]().Execute(context.Background(), 0)
	if !errors.Is(err, ErrNoExecutors) {
		t.Fatalf("expected ErrNoExecutors, got %v", err)
	}

	_, err = RoundRobin[int, int](
		ExecutorFunc[int, int](func(context.Context, int) (int, error) { return 1, nil }),
		nil,
	).Execute(context.Background(), 0)
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
		ExecutorFunc[int, int](func(context.Context, int) (int, error) { return 1, nil }),
		ExecutorFunc[int, int](func(context.Context, int) (int, error) { return 2, nil }),
	)

	var group sync.WaitGroup
	errs := make(chan error, workers*calls)

	for range workers {
		group.Go(func() {
			for range calls {
				_, err := executor.Execute(context.Background(), 0)
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

	base := ExecutorFunc[int, int](func(ctx context.Context, _ int) (int, error) {
		<-ctx.Done()
		return 0, ctx.Err()
	})

	start := time.Now()
	_, err := Timeout[int, int](timeout)(base).Execute(context.Background(), 0)
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

	base := ExecutorFunc[int, int](func(ctx context.Context, _ int) (int, error) {
		<-ctx.Done()
		return 0, ctx.Err()
	})

	ctx, cancel := context.WithTimeout(context.Background(), parentTimeout)
	defer cancel()

	start := time.Now()
	_, err := Timeout[int, int](mwTimeout)(base).Execute(ctx, 0)
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

	base := ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		return 2 + 2, nil
	})

	result, err := Timeout[int, int](0)(base).Execute(context.Background(), 0)
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if result != 2+2 {
		t.Fatalf("unexpected result: got %d, want 4", result)
	}
}

func TestTimeoutReturnsConfigErrorWhenNextIsNil(t *testing.T) {
	t.Parallel()

	_, err := Timeout[int, int](time.Second)(nil).Execute(context.Background(), 0)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}
