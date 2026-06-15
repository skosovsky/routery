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
	handler := PredicateFallback[int, int](
		FromFunc(func(context.Context, int) (int, error) {
			return 0, expectedErr
		}),
		FromFunc(func(context.Context, int) (int, error) {
			return 2, nil
		}),
		func(err error) bool {
			return errors.Is(err, expectedErr)
		},
	)

	outcome, err := InvokeRouteHandler(context.Background(), 0, handler)
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if !outcome.HasPayload || outcome.Payload != 2 {
		t.Fatalf("unexpected result: got %d, want 2", outcome.Payload)
	}
}

func TestPredicateFallbackReturnsPrimaryErrorWhenPredicateMisses(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("primary")
	handler := PredicateFallback[int, int](
		FromFunc(func(context.Context, int) (int, error) {
			return 0, expectedErr
		}),
		FromFunc(func(context.Context, int) (int, error) {
			return 2, nil
		}),
		func(error) bool {
			return false
		},
	)

	outcome, err := InvokeRouteHandler(context.Background(), 0, handler)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected primary error, got %v", err)
	}
	if outcome.HasPayload {
		t.Fatalf("expected no payload on error, got %d", outcome.Payload)
	}
}

func TestPredicateFallbackReturnsConfigErrorForInvalidConfig(t *testing.T) {
	t.Parallel()

	_, err := InvokeRouteHandler(context.Background(), 0, PredicateFallback[int, int](
		FromFunc(func(context.Context, int) (int, error) {
			return 1, nil
		}),
		nil,
		nil,
	))
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestFirstCompletedReturnsFirstSuccessAndCancelsOthers(t *testing.T) {
	t.Parallel()

	const waitTimeout = 32 * time.Millisecond

	cancelled := make(chan struct{}, 1)

	slow := func(ctx context.Context, _ int, _ ResultRecorder[int]) error {
		<-ctx.Done()
		cancelled <- struct{}{}
		return ctx.Err()
	}

	fast := FromFunc(func(context.Context, int) (int, error) {
		return 2, nil
	})

	outcome, err := InvokeRouteHandler(context.Background(), 0, FirstCompleted[int, int](slow, fast))
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if !outcome.HasPayload || outcome.Payload != 2 {
		t.Fatalf("unexpected result: got %d, want 2", outcome.Payload)
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

	first := FromFunc(func(context.Context, int) (int, error) {
		time.Sleep(slowDelay)
		return 0, firstErr
	})
	second := FromFunc(func(context.Context, int) (int, error) {
		return 0, secondErr
	})

	_, err := InvokeRouteHandler(context.Background(), 0, FirstCompleted[int, int](first, second))
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

	_, err := InvokeRouteHandler(context.Background(), 0, FirstCompleted[int, int]())
	if !errors.Is(err, ErrNoHandlers) {
		t.Fatalf("expected ErrNoHandlers, got %v", err)
	}

	_, err = InvokeRouteHandler(context.Background(), 0, FirstCompleted[int, int](
		FromFunc(func(context.Context, int) (int, error) { return 1, nil }),
		nil,
	))
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestFirstCompletedIgnoreDoesNotWinOverStop(t *testing.T) {
	t.Parallel()

	const slowDelay = 40 * time.Millisecond

	slow := func(_ context.Context, _ int, rec ResultRecorder[string]) error {
		time.Sleep(slowDelay)
		rec.Stop("data", "")
		return nil
	}
	fast := func(_ context.Context, _ int, rec ResultRecorder[string]) error {
		rec.Ignore("skip")
		return nil
	}

	outcome, err := InvokeRouteHandler(context.Background(), 0, FirstCompleted[int, string](slow, fast))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !outcome.HasPayload || outcome.Payload != "data" {
		t.Fatalf("got %+v; want payload data", outcome)
	}
}

func TestFirstCompletedAsyncWithPayloadWins(t *testing.T) {
	t.Parallel()

	handler := func(_ context.Context, _ int, rec ResultRecorder[string]) error {
		rec.Async("accepted", "async")
		return nil
	}

	outcome, err := InvokeRouteHandler(context.Background(), 0, FirstCompleted[int, string](handler))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !outcome.HasPayload || outcome.Payload != "accepted" {
		t.Fatalf("got %+v; want accepted payload", outcome)
	}
}

func TestFirstCompletedAllNextReturnsErrNoSuccessfulOutcome(t *testing.T) {
	t.Parallel()

	nextOnly := func(_ context.Context, _ int, rec ResultRecorder[int]) error {
		rec.Next("delegate")
		return nil
	}

	_, err := InvokeRouteHandler(context.Background(), 0, FirstCompleted[int, int](nextOnly, nextOnly))
	if !errors.Is(err, ErrNoSuccessfulOutcome) {
		t.Fatalf("expected ErrNoSuccessfulOutcome, got %v", err)
	}
}

func TestFirstCompletedReturnsExternalContextCancellation(t *testing.T) {
	t.Parallel()

	waitOnContext := func(ctx context.Context, _ int, _ ResultRecorder[int]) error {
		<-ctx.Done()
		return ctx.Err()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Millisecond)
	defer cancel()

	_, err := InvokeRouteHandler(ctx, 0, FirstCompleted[int, int](waitOnContext, waitOnContext))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}
}

func TestWeightBasedRouterUsesConfiguredRouteHandlers(t *testing.T) {
	t.Parallel()

	const threshold = 2 + 1

	lightCalls := atomic.Int32{}
	heavyCalls := atomic.Int32{}

	light := FromFunc(func(context.Context, int) (int, error) {
		lightCalls.Add(1)
		return 1, nil
	})
	heavy := FromFunc(func(context.Context, int) (int, error) {
		heavyCalls.Add(1)
		return 2, nil
	})

	handler := WeightBasedRouter[int, int](
		func(context.Context, int) (int, error) {
			return 2, nil
		},
		threshold,
		light,
		heavy,
	)

	lightOutcome, err := InvokeRouteHandler(context.Background(), 0, handler)
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if !lightOutcome.HasPayload || lightOutcome.Payload != 1 {
		t.Fatalf("unexpected light result: got %d, want 1", lightOutcome.Payload)
	}

	handler = WeightBasedRouter[int, int](
		func(context.Context, int) (int, error) {
			return threshold, nil
		},
		threshold,
		light,
		heavy,
	)

	heavyOutcome, err := InvokeRouteHandler(context.Background(), 0, handler)
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if !heavyOutcome.HasPayload || heavyOutcome.Payload != 2 {
		t.Fatalf("unexpected heavy result: got %d, want 2", heavyOutcome.Payload)
	}
	if lightCalls.Load() != 1 || heavyCalls.Load() != 1 {
		t.Fatalf("unexpected call counts: light=%d heavy=%d", lightCalls.Load(), heavyCalls.Load())
	}
}

func TestWeightBasedRouterReturnsExtractorErrors(t *testing.T) {
	t.Parallel()

	extractorErr := errors.New("extractor")
	handler := WeightBasedRouter[int, int](
		func(context.Context, int) (int, error) {
			return 0, extractorErr
		},
		0,
		FromFunc(func(context.Context, int) (int, error) { return 1, nil }),
		FromFunc(func(context.Context, int) (int, error) { return 2, nil }),
	)

	_, err := InvokeRouteHandler(context.Background(), 0, handler)
	if !errors.Is(err, extractorErr) {
		t.Fatalf("expected extractor error, got %v", err)
	}
}

func TestPredicateFallbackPrimaryNextThenErrorUsesCleanSecondary(t *testing.T) {
	t.Parallel()

	secondaryCalled := false
	primary := func(_ context.Context, _ int, rec ResultRecorder[int]) error {
		rec.Next("dirty")
		return errors.New("primary failed")
	}
	secondary := func(_ context.Context, _ int, rec ResultRecorder[int]) error {
		secondaryCalled = true
		rec.Stop(11, "")
		return nil
	}

	outcome, err := InvokeRouteHandler(context.Background(), 0, PredicateFallback(primary, secondary, func(error) bool {
		return true
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !secondaryCalled {
		t.Fatal("expected secondary to run")
	}
	if !outcome.HasPayload || outcome.Payload != 11 {
		t.Fatalf("got %+v; want payload 11", outcome)
	}
}

func TestWeightBasedRouterReturnsConfigErrorForInvalidConfig(t *testing.T) {
	t.Parallel()

	_, err := InvokeRouteHandler(context.Background(), 0, WeightBasedRouter[int, int](nil, 0, nil, nil))
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}
