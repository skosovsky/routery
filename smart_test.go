package routery

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPredicateFallbackUsesSecondaryWhenPredicateMatches(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("primary")
	executor := PredicateFallback[int, int](
		ExecutorFunc[int, int](func(context.Context, int) (int, error) {
			return 0, expectedErr
		}),
		ExecutorFunc[int, int](func(context.Context, int) (int, error) {
			return 2, nil
		}),
		func(err error) bool {
			return errors.Is(err, expectedErr)
		},
	)

	result, err := executor.Execute(context.Background(), 0)
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if result != 2 {
		t.Fatalf("unexpected result: got %d, want 2", result)
	}
}

func TestPredicateFallbackReturnsPrimaryErrorWhenPredicateMisses(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("primary")
	executor := PredicateFallback[int, int](
		ExecutorFunc[int, int](func(context.Context, int) (int, error) {
			return 1, expectedErr
		}),
		ExecutorFunc[int, int](func(context.Context, int) (int, error) {
			return 2, nil
		}),
		func(error) bool {
			return false
		},
	)

	result, err := executor.Execute(context.Background(), 0)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected primary error, got %v", err)
	}
	if result != 1 {
		t.Fatalf("unexpected result: got %d, want 1", result)
	}
}

func TestPredicateFallbackReturnsConfigErrorForInvalidConfig(t *testing.T) {
	t.Parallel()

	_, err := PredicateFallback[int, int](
		ExecutorFunc[int, int](func(context.Context, int) (int, error) {
			return 1, nil
		}),
		nil,
		nil,
	).Execute(context.Background(), 0)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestFirstCompletedReturnsFirstSuccessAndCancelsOthers(t *testing.T) {
	t.Parallel()

	const waitTimeout = 32 * time.Millisecond

	cancelled := make(chan struct{}, 1)

	slow := ExecutorFunc[int, int](func(ctx context.Context, _ int) (int, error) {
		<-ctx.Done()
		cancelled <- struct{}{}
		return 0, ctx.Err()
	})

	fast := ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		return 2, nil
	})

	result, err := FirstCompleted[int, int](slow, fast).Execute(context.Background(), 0)
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if result != 2 {
		t.Fatalf("unexpected result: got %d, want 2", result)
	}

	select {
	case <-cancelled:
	case <-time.After(waitTimeout):
		t.Fatal("expected slow executor to be cancelled")
	}
}

func TestFirstCompletedJoinsErrorsInDeclarationOrder(t *testing.T) {
	t.Parallel()

	const slowDelay = 8 * time.Millisecond

	firstErr := errors.New("first")
	secondErr := errors.New("second")

	first := ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		time.Sleep(slowDelay)
		return 0, firstErr
	})
	second := ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		return 0, secondErr
	})

	_, err := FirstCompleted[int, int](first, second).Execute(context.Background(), 0)
	if err == nil {
		t.Fatal("expected an aggregated error")
	}
	if !errors.Is(err, firstErr) || !errors.Is(err, secondErr) {
		t.Fatalf("expected joined errors to contain both errors, got %v", err)
	}

	lines := strings.Split(err.Error(), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected two joined errors, got %q", err.Error())
	}
	if lines[0] != firstErr.Error() || lines[1] != secondErr.Error() {
		t.Fatalf("unexpected join order: got %q", err.Error())
	}
}

func TestFirstCompletedReturnsSentinelErrors(t *testing.T) {
	t.Parallel()

	_, err := FirstCompleted[int, int]().Execute(context.Background(), 0)
	if !errors.Is(err, ErrNoExecutors) {
		t.Fatalf("expected ErrNoExecutors, got %v", err)
	}

	_, err = FirstCompleted[int, int](
		ExecutorFunc[int, int](func(context.Context, int) (int, error) { return 1, nil }),
		nil,
	).Execute(context.Background(), 0)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestFirstCompletedReturnsExternalContextCancellation(t *testing.T) {
	t.Parallel()

	waitOnContext := ExecutorFunc[int, int](func(ctx context.Context, _ int) (int, error) {
		<-ctx.Done()
		return 0, ctx.Err()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Millisecond)
	defer cancel()

	_, err := FirstCompleted[int, int](waitOnContext, waitOnContext).Execute(ctx, 0)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}
}

func TestWeightBasedRouterUsesConfiguredExecutors(t *testing.T) {
	t.Parallel()

	const threshold = 2 + 1

	lightCalls := atomic.Int32{}
	heavyCalls := atomic.Int32{}

	light := ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		lightCalls.Add(1)
		return 1, nil
	})
	heavy := ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		heavyCalls.Add(1)
		return 2, nil
	})

	executor := WeightBasedRouter[int, int](
		func(context.Context, int) (int, error) {
			return 2, nil
		},
		threshold,
		light,
		heavy,
	)

	lightResult, err := executor.Execute(context.Background(), 0)
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if lightResult != 1 {
		t.Fatalf("unexpected light result: got %d, want 1", lightResult)
	}

	executor = WeightBasedRouter[int, int](
		func(context.Context, int) (int, error) {
			return threshold, nil
		},
		threshold,
		light,
		heavy,
	)

	heavyResult, err := executor.Execute(context.Background(), 0)
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if heavyResult != 2 {
		t.Fatalf("unexpected heavy result: got %d, want 2", heavyResult)
	}
	if lightCalls.Load() != 1 || heavyCalls.Load() != 1 {
		t.Fatalf("unexpected call counts: light=%d heavy=%d", lightCalls.Load(), heavyCalls.Load())
	}
}

func TestWeightBasedRouterReturnsExtractorErrors(t *testing.T) {
	t.Parallel()

	extractorErr := errors.New("extractor")
	executor := WeightBasedRouter[int, int](
		func(context.Context, int) (int, error) {
			return 0, extractorErr
		},
		0,
		ExecutorFunc[int, int](func(context.Context, int) (int, error) { return 1, nil }),
		ExecutorFunc[int, int](func(context.Context, int) (int, error) { return 2, nil }),
	)

	_, err := executor.Execute(context.Background(), 0)
	if !errors.Is(err, extractorErr) {
		t.Fatalf("expected extractor error, got %v", err)
	}
}

func TestWeightBasedRouterReturnsConfigErrorForInvalidConfig(t *testing.T) {
	t.Parallel()

	_, err := WeightBasedRouter[int, int](nil, 0, nil, nil).Execute(context.Background(), 0)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}
