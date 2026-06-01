package routery

import (
	"context"
	"errors"
	"testing"
)

func BenchmarkApplyExecute(b *testing.B) {
	base := HandlerFunc[int, int](func(context.Context, int) (RouteResult[int], error) {
		return Handled(1), nil
	})

	executor := Apply(
		base,
		Timeout[int, int](0),
		RetryIf[int, int](1, 0, func(context.Context, int, error) bool { return false }),
	)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_, err := executor.Handle(context.Background(), 0)
		if err != nil {
			b.Fatalf("execute returned error: %v", err)
		}
	}
}

func BenchmarkRetryIfNoRetry(b *testing.B) {
	base := HandlerFunc[int, int](func(context.Context, int) (RouteResult[int], error) {
		return zeroRouteResult[int](), errors.New("stop")
	})
	executor := RetryIf[int, int](3, 0, func(context.Context, int, error) bool { return false })(base)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_, err := executor.Handle(context.Background(), 0)
		if err == nil {
			b.Fatal("expected error")
		}
	}
}

func BenchmarkRoundRobinExecute(b *testing.B) {
	executor := RoundRobin(
		HandlerFunc[int, int](func(context.Context, int) (RouteResult[int], error) { return Handled(1), nil }),
		HandlerFunc[int, int](func(context.Context, int) (RouteResult[int], error) { return Handled(2), nil }),
	)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_, err := executor.Handle(context.Background(), 0)
		if err != nil {
			b.Fatalf("execute returned error: %v", err)
		}
	}
}

func BenchmarkFirstCompleted(b *testing.B) {
	executor := FirstCompleted(
		HandlerFunc[int, int](func(context.Context, int) (RouteResult[int], error) {
			return Handled(1), nil
		}),
		HandlerFunc[int, int](func(context.Context, int) (RouteResult[int], error) {
			return Handled(2), nil
		}),
	)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_, err := executor.Handle(context.Background(), 0)
		if err != nil {
			b.Fatalf("execute returned error: %v", err)
		}
	}
}

func BenchmarkWeightBasedRouter(b *testing.B) {
	executor := WeightBasedRouter(
		func(context.Context, int) (int, error) { return 1, nil },
		2,
		HandlerFunc[int, int](func(context.Context, int) (RouteResult[int], error) { return Handled(1), nil }),
		HandlerFunc[int, int](func(context.Context, int) (RouteResult[int], error) { return Handled(2), nil }),
	)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_, err := executor.Handle(context.Background(), 0)
		if err != nil {
			b.Fatalf("execute returned error: %v", err)
		}
	}
}
