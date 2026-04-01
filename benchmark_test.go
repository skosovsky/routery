package routery

import (
	"context"
	"errors"
	"testing"
)

func BenchmarkApplyExecute(b *testing.B) {
	base := ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		return 1, nil
	})

	executor := Apply(
		base,
		Timeout[int, int](0),
		RetryIf[int, int](1, 0, func(context.Context, int, error) bool { return false }),
	)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_, err := executor.Execute(context.Background(), 0)
		if err != nil {
			b.Fatalf("execute returned error: %v", err)
		}
	}
}

func BenchmarkRetryIfNoRetry(b *testing.B) {
	base := ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		return 0, errors.New("stop")
	})
	executor := RetryIf[int, int](3, 0, func(context.Context, int, error) bool { return false })(base)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_, err := executor.Execute(context.Background(), 0)
		if err == nil {
			b.Fatal("expected error")
		}
	}
}

func BenchmarkRoundRobinExecute(b *testing.B) {
	executor := RoundRobin(
		ExecutorFunc[int, int](func(context.Context, int) (int, error) { return 1, nil }),
		ExecutorFunc[int, int](func(context.Context, int) (int, error) { return 2, nil }),
	)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_, err := executor.Execute(context.Background(), 0)
		if err != nil {
			b.Fatalf("execute returned error: %v", err)
		}
	}
}

func BenchmarkFirstCompleted(b *testing.B) {
	executor := FirstCompleted(
		ExecutorFunc[int, int](func(context.Context, int) (int, error) {
			return 1, nil
		}),
		ExecutorFunc[int, int](func(context.Context, int) (int, error) {
			return 2, nil
		}),
	)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_, err := executor.Execute(context.Background(), 0)
		if err != nil {
			b.Fatalf("execute returned error: %v", err)
		}
	}
}

func BenchmarkWeightBasedRouter(b *testing.B) {
	executor := WeightBasedRouter(
		func(context.Context, int) (int, error) { return 1, nil },
		2,
		ExecutorFunc[int, int](func(context.Context, int) (int, error) { return 1, nil }),
		ExecutorFunc[int, int](func(context.Context, int) (int, error) { return 2, nil }),
	)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_, err := executor.Execute(context.Background(), 0)
		if err != nil {
			b.Fatalf("execute returned error: %v", err)
		}
	}
}
