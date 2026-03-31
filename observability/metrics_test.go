package observability

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/skosovsky/routery"
)

func TestMetricsNoOpWhenHooksAreEmpty(t *testing.T) {
	t.Parallel()

	calls := 0
	base := routery.ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		calls++
		return 1, nil
	})

	result, err := Metrics[int, int]("operation", MetricsHooks{})(base).Execute(context.Background(), 0)
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if result != 1 {
		t.Fatalf("unexpected result: got %d, want 1", result)
	}
	if calls != 1 {
		t.Fatalf("unexpected call count: got %d, want 1", calls)
	}
}

func TestMetricsCallsHooks(t *testing.T) {
	t.Parallel()

	startCalls := atomic.Int32{}
	completeCalls := atomic.Int32{}
	var (
		gotName     string
		gotDuration time.Duration
		gotErr      error
	)

	expectedErr := errors.New("failure")
	base := routery.ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		return 0, expectedErr
	})

	executor := Metrics[int, int]("primary", MetricsHooks{
		OnStart: func(context.Context, string) {
			startCalls.Add(1)
		},
		OnComplete: func(_ context.Context, name string, duration time.Duration, err error) {
			completeCalls.Add(1)
			gotName = name
			gotDuration = duration
			gotErr = err
		},
	})(base)

	_, err := executor.Execute(context.Background(), 0)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected wrapped error, got %v", err)
	}

	if startCalls.Load() != 1 {
		t.Fatalf("unexpected start calls: got %d, want 1", startCalls.Load())
	}
	if completeCalls.Load() != 1 {
		t.Fatalf("unexpected complete calls: got %d, want 1", completeCalls.Load())
	}
	if gotName != "primary" {
		t.Fatalf("unexpected name: got %q, want %q", gotName, "primary")
	}
	if gotDuration < 0 {
		t.Fatalf("expected non-negative duration, got %v", gotDuration)
	}
	if !errors.Is(gotErr, expectedErr) {
		t.Fatalf("unexpected completion error: got %v, want %v", gotErr, expectedErr)
	}
}

func TestMetricsReturnsConfigErrorWhenNextExecutorIsNil(t *testing.T) {
	t.Parallel()

	executor := Metrics[int, int]("primary", MetricsHooks{
		OnStart: func(context.Context, string) {},
	})(nil)

	_, err := executor.Execute(context.Background(), 0)
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}
