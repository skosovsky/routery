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

	primary := FromFunc(func(context.Context, int) (int, error) {
		primaryCalls++
		return 1, nil
	})
	secondary := FromFunc(func(context.Context, int) (int, error) {
		secondaryCalls++
		return 2, nil
	})

	outcome, err := InvokeRouteHandler(context.Background(), 0, Fallback(primary, secondary))
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if !outcome.HasPayload || outcome.Payload != 1 {
		t.Fatalf("unexpected result: got %d, want 1", outcome.Payload)
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
	primary := FromFunc(func(context.Context, int) (int, error) {
		return 0, primaryErr
	})
	secondary := FromFunc(func(context.Context, int) (int, error) {
		return 2, nil
	})

	outcome, err := InvokeRouteHandler(context.Background(), 0, Fallback(primary, secondary))
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if !outcome.HasPayload || outcome.Payload != 2 {
		t.Fatalf("unexpected result: got %d, want 2", outcome.Payload)
	}
}

func TestFallbackReturnsConfigErrorForInvalidConfig(t *testing.T) {
	t.Parallel()

	_, err := InvokeRouteHandler(context.Background(), 0, Fallback[int, int](nil, nil))
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestRetryIfRetriesWhenPredicateMatches(t *testing.T) {
	t.Parallel()

	const attemptsCount = 3

	retryErr := errors.New("retry")
	calls := 0

	base := FromFunc(func(context.Context, int) (int, error) {
		calls++
		if calls < attemptsCount {
			return 0, retryErr
		}
		return 1, nil
	})

	handler := ApplyRoute(base, RetryIf[int, int](attemptsCount, 0, func(_ context.Context, _ int, err error) bool {
		return errors.Is(err, retryErr)
	}))

	outcome, err := InvokeRouteHandler(context.Background(), 0, handler)
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if !outcome.HasPayload || outcome.Payload != 1 {
		t.Fatalf("unexpected result: got %d, want 1", outcome.Payload)
	}
	if calls != attemptsCount {
		t.Fatalf("unexpected call count: got %d, want %d", calls, attemptsCount)
	}
}

func TestRetryIfStopsWhenPredicateMisses(t *testing.T) {
	t.Parallel()

	calls := 0
	baseErr := errors.New("stop")
	base := FromFunc(func(context.Context, int) (int, error) {
		calls++
		return 0, baseErr
	})

	handler := ApplyRoute(base, RetryIf[int, int](2+1, 0, func(context.Context, int, error) bool {
		return false
	}))

	_, err := InvokeRouteHandler(context.Background(), 0, handler)
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
	base := FromFunc(func(context.Context, int) (int, error) {
		calls++
		return 0, baseErr
	})

	handler := ApplyRoute(base, RetryIf[int, int](0, 0, func(context.Context, int, error) bool {
		return true
	}))

	_, err := InvokeRouteHandler(context.Background(), 0, handler)
	if !errors.Is(err, baseErr) {
		t.Fatalf("expected base error, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("unexpected call count: got %d, want 1", calls)
	}
}

func TestRetryIfReturnsConfigErrorForNilPredicate(t *testing.T) {
	t.Parallel()

	base := FromFunc(func(context.Context, int) (int, error) {
		return 1, nil
	})

	handler := ApplyRoute(base, RetryIf[int, int](1, 0, nil))
	_, err := InvokeRouteHandler(context.Background(), 0, handler)
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

	base := FromFunc(func(context.Context, int) (int, error) {
		return 0, errors.New("retry")
	})

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	handler := ApplyRoute(base, RetryIf[int, int](2+1, backoff, func(context.Context, int, error) bool {
		return true
	}))

	start := time.Now()
	_, err := InvokeRouteHandler(ctx, 0, handler)
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

	handler := RoundRobin[int, int](
		FromFunc(func(context.Context, int) (int, error) { return 1, nil }),
		FromFunc(func(context.Context, int) (int, error) { return 2, nil }),
		FromFunc(func(context.Context, int) (int, error) { return 2 + 1, nil }),
	)

	got := make([]int, 0, calls)
	for range calls {
		outcome, err := InvokeRouteHandler(context.Background(), 0, handler)
		if err != nil {
			t.Fatalf("execute returned unexpected error: %v", err)
		}
		if !outcome.HasPayload {
			t.Fatal("expected payload")
		}
		got = append(got, outcome.Payload)
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

	_, err := InvokeRouteHandler(context.Background(), 0, RoundRobin[int, int]())
	if !errors.Is(err, ErrNoHandlers) {
		t.Fatalf("expected ErrNoHandlers, got %v", err)
	}

	_, err = InvokeRouteHandler(context.Background(), 0, RoundRobin[int, int](
		FromFunc(func(context.Context, int) (int, error) { return 1, nil }),
		nil,
	))
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

	handler := RoundRobin[int, int](
		FromFunc(func(context.Context, int) (int, error) { return 1, nil }),
		FromFunc(func(context.Context, int) (int, error) { return 2, nil }),
	)

	var group sync.WaitGroup
	errs := make(chan error, workers*calls)

	for range workers {
		group.Go(func() {
			for range calls {
				_, err := InvokeRouteHandler(context.Background(), 0, handler)
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

	base := func(ctx context.Context, _ int, _ ResultRecorder[int]) error {
		<-ctx.Done()
		return ctx.Err()
	}

	start := time.Now()
	_, err := InvokeRouteHandler(context.Background(), 0, ApplyRoute(base, Timeout[int, int](timeout)))
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

	base := func(ctx context.Context, _ int, _ ResultRecorder[int]) error {
		<-ctx.Done()
		return ctx.Err()
	}

	ctx, cancel := context.WithTimeout(context.Background(), parentTimeout)
	defer cancel()

	start := time.Now()
	_, err := InvokeRouteHandler(ctx, 0, ApplyRoute(base, Timeout[int, int](mwTimeout)))
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

	base := FromFunc(func(context.Context, int) (int, error) {
		return 2 + 2, nil
	})

	outcome, err := InvokeRouteHandler(context.Background(), 0, ApplyRoute(base, Timeout[int, int](0)))
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if !outcome.HasPayload || outcome.Payload != 2+2 {
		t.Fatalf("unexpected result: got %d, want 4", outcome.Payload)
	}
}

func TestRetryIfDoesNotRetryOnActionNext(t *testing.T) {
	t.Parallel()

	calls := 0
	base := func(_ context.Context, _ int, rec ResultRecorder[int]) error {
		calls++
		rec.Next("delegate")
		return nil
	}

	handler := ApplyRoute(base, RetryIf[int, int](5, 0, func(context.Context, int, error) bool {
		return true
	}))

	rec := NewResultRecorder[int]()
	if err := handler(context.Background(), 0, rec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Action() != ActionNext || calls != 1 {
		t.Fatalf("got action=%s calls=%d, want next/1", rec.Action(), calls)
	}
}

func TestRetryIfDoesNotRetryOnIgnore(t *testing.T) {
	t.Parallel()

	calls := 0
	base := func(_ context.Context, _ int, rec ResultRecorder[int]) error {
		calls++
		rec.Ignore("skip")
		return nil
	}

	handler := ApplyRoute(base, RetryIf[int, int](5, 0, func(context.Context, int, error) bool {
		return true
	}))

	rec := NewResultRecorder[int]()
	if err := handler(context.Background(), 0, rec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Action() != ActionStop || calls != 1 {
		t.Fatalf("got action=%s calls=%d, want stop/1", rec.Action(), calls)
	}
	if _, ok := rec.Payload(); ok {
		t.Fatal("expected no payload after Ignore")
	}
}

func TestFallbackPrimaryNextThenErrorUsesCleanSecondary(t *testing.T) {
	t.Parallel()

	secondaryCalled := false
	primary := func(_ context.Context, _ int, rec ResultRecorder[int]) error {
		rec.Next("dirty")
		return errors.New("primary failed")
	}
	secondary := func(_ context.Context, _ int, rec ResultRecorder[int]) error {
		secondaryCalled = true
		rec.Stop(42, "")
		return nil
	}

	outcome, err := InvokeRouteHandler(context.Background(), 0, Fallback(primary, secondary))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !secondaryCalled {
		t.Fatal("expected secondary to run")
	}
	if !outcome.HasPayload || outcome.Payload != 42 {
		t.Fatalf("got %+v; want payload 42", outcome)
	}
}

func TestRetryIfRetriesAfterPrimaryNextWithoutTerminalState(t *testing.T) {
	t.Parallel()

	attempts := 0
	base := func(_ context.Context, _ int, rec ResultRecorder[int]) error {
		attempts++
		if attempts == 1 {
			rec.Next("partial")
			return errors.New("retry me")
		}
		rec.Stop(7, "")
		return nil
	}

	handler := ApplyRoute(base, RetryIf[int, int](3, 0, func(context.Context, int, error) bool {
		return true
	}))

	outcome, err := InvokeRouteHandler(context.Background(), 0, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("got attempts=%d; want 2", attempts)
	}
	if !outcome.HasPayload || outcome.Payload != 7 {
		t.Fatalf("got %+v; want payload 7", outcome)
	}
}

func TestFallbackPrimaryIgnoreSkipsSecondary(t *testing.T) {
	t.Parallel()

	secondaryCalled := false
	primary := func(_ context.Context, _ int, rec ResultRecorder[int]) error {
		rec.Ignore("skip")
		return nil
	}
	secondary := func(_ context.Context, _ int, rec ResultRecorder[int]) error {
		secondaryCalled = true
		rec.Stop(99, "")
		return nil
	}

	outcome, err := InvokeRouteHandler(context.Background(), 0, Fallback(primary, secondary))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if secondaryCalled {
		t.Fatal("expected secondary to be skipped")
	}
	if outcome.HasPayload {
		t.Fatal("expected no payload after Ignore")
	}
	if outcome.ReasonCode != "skip" {
		t.Fatalf("got reason %q; want skip", outcome.ReasonCode)
	}
}

func TestFallbackPrimaryNextSkipsSecondary(t *testing.T) {
	t.Parallel()

	secondaryCalled := false
	primary := func(_ context.Context, _ int, rec ResultRecorder[int]) error {
		rec.Next("delegate")
		return nil
	}
	secondary := func(_ context.Context, _ int, rec ResultRecorder[int]) error {
		secondaryCalled = true
		rec.Stop(99, "")
		return nil
	}

	outcome, err := InvokeRouteHandler(context.Background(), 0, Fallback(primary, secondary))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if secondaryCalled {
		t.Fatal("expected secondary to be skipped")
	}
	if outcome.Action != ActionNext {
		t.Fatalf("got action %q; want next", outcome.Action)
	}
	if outcome.ReasonCode != "delegate" {
		t.Fatalf("got reason %q; want delegate", outcome.ReasonCode)
	}
}

func TestPredicateFallbackPrimaryNextSkipsSecondary(t *testing.T) {
	t.Parallel()

	secondaryCalled := false
	primary := func(_ context.Context, _ int, rec ResultRecorder[int]) error {
		rec.Next("delegate")
		return nil
	}
	secondary := func(_ context.Context, _ int, rec ResultRecorder[int]) error {
		secondaryCalled = true
		rec.Stop(99, "")
		return nil
	}

	outcome, err := InvokeRouteHandler(context.Background(), 0, PredicateFallback(primary, secondary, func(error) bool {
		return true
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if secondaryCalled {
		t.Fatal("expected secondary to be skipped")
	}
	if outcome.Action != ActionNext {
		t.Fatalf("got action %q; want next", outcome.Action)
	}
}

func TestCoreTimeoutCopiesNextAndIgnoreOnSuccess(t *testing.T) {
	t.Parallel()

	t.Run("next", func(t *testing.T) {
		t.Parallel()

		rec := NewResultRecorder[int]()
		base := func(_ context.Context, _ int, localRec ResultRecorder[int]) error {
			localRec.Next("delegate")
			return nil
		}
		err := ApplyRoute(base, Timeout[int, int](time.Second))(context.Background(), 0, rec)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Action() != ActionNext {
			t.Fatalf("got action %q; want next", rec.Action())
		}
		if rec.ReasonCode() != "delegate" {
			t.Fatalf("got reason %q; want delegate", rec.ReasonCode())
		}
	})

	t.Run("ignore", func(t *testing.T) {
		t.Parallel()

		rec := NewResultRecorder[int]()
		base := func(_ context.Context, _ int, localRec ResultRecorder[int]) error {
			localRec.Ignore("skip")
			return nil
		}
		err := ApplyRoute(base, Timeout[int, int](time.Second))(context.Background(), 0, rec)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rec.Action() != ActionStop {
			t.Fatalf("got action %q; want stop", rec.Action())
		}
		if rec.ReasonCode() != "skip" {
			t.Fatalf("got reason %q; want skip", rec.ReasonCode())
		}
		if _, ok := rec.Payload(); ok {
			t.Fatal("expected no payload after Ignore")
		}
	})
}

func TestInvokeRouteHandlerReturnsAbortOutcomeOnError(t *testing.T) {
	t.Parallel()

	handlerErr := errors.New("handler failed")
	handler := func(_ context.Context, _ int, rec ResultRecorder[int]) error {
		rec.Stop(42, "handled")
		return handlerErr
	}

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

func TestTimeoutDiscardsPartialRecorderOnContextError(t *testing.T) {
	t.Parallel()

	base := func(ctx context.Context, _ int, rec ResultRecorder[int]) error {
		rec.Next("partial")
		<-ctx.Done()
		return ctx.Err()
	}

	rec := NewResultRecorder[int]()
	err := ApplyRoute(base, Timeout[int, int](20*time.Millisecond))(context.Background(), 0, rec)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got err %v; want deadline exceeded", err)
	}
	if rec.Action() != ActionNext {
		t.Fatalf("got action %s; want next (partial state discarded)", rec.Action())
	}
}

func TestInvokeRouteHandlerIgnoreReturnsOutcome(t *testing.T) {
	t.Parallel()

	handler := func(_ context.Context, _ int, rec ResultRecorder[int]) error {
		rec.Ignore("ignored")
		return nil
	}

	outcome, err := InvokeRouteHandler(context.Background(), 0, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.HasPayload {
		t.Fatal("expected no payload")
	}
	if outcome.ReasonCode != "ignored" {
		t.Fatalf("got reason %q; want ignored", outcome.ReasonCode)
	}
	if outcome.Action != ActionStop {
		t.Fatalf("got action %q; want stop", outcome.Action)
	}
}

func TestTimeoutReturnsConfigErrorWhenNextIsNil(t *testing.T) {
	t.Parallel()

	_, err := InvokeRouteHandler(context.Background(), 0, ApplyRoute[int, int](nil, Timeout[int, int](time.Second)))
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}
