package routery

import (
	"context"
	"errors"
	"testing"
	"time"
)

func FuzzRetryIfNoPanics(f *testing.F) {
	f.Add(1, int64(0), true)
	f.Add(0, int64(-1), false)
	f.Add(-1, int64(3), true)

	f.Fuzz(func(t *testing.T, attempts int, backoffMillis int64, useNilPredicate bool) {
		t.Helper()

		base := ExecutorFunc[int, int](func(context.Context, int) (int, error) {
			return 0, errors.New("retry")
		})

		backoff := time.Duration(backoffMillis%100) * time.Millisecond
		predicate := func(error) bool { return true }
		if useNilPredicate {
			predicate = nil
		}

		executor := RetryIf[int, int](attempts, backoff, predicate)(base)
		_, _ = executor.Execute(context.Background(), 0)
	})
}

func FuzzConstructorsNoPanics(f *testing.F) {
	f.Add(true, true, true, 0)
	f.Add(false, false, false, 1)

	f.Fuzz(func(t *testing.T, primaryNil bool, secondaryNil bool, predicateNil bool, threshold int) {
		t.Helper()

		var primary Executor[int, int]
		var secondary Executor[int, int]

		if !primaryNil {
			primary = ExecutorFunc[int, int](func(context.Context, int) (int, error) { return 1, nil })
		}
		if !secondaryNil {
			secondary = ExecutorFunc[int, int](func(context.Context, int) (int, error) { return 2, nil })
		}

		predicate := ErrorPredicate(func(error) bool { return true })
		if predicateNil {
			predicate = nil
		}

		_, _ = Fallback(primary, secondary).Execute(context.Background(), 0)
		_, _ = PredicateFallback(primary, secondary, predicate).Execute(context.Background(), 0)
		_, _ = RoundRobin(primary, secondary).Execute(context.Background(), 0)
		_, _ = FirstCompleted(primary, secondary).Execute(context.Background(), 0)
		_, _ = WeightBasedRouter(
			func(context.Context, int) (int, error) { return 1, nil },
			threshold,
			primary,
			secondary,
		).Execute(context.Background(), 0)
	})
}
