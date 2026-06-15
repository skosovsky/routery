package routery

import (
	"context"
	"errors"
	"testing"
)

func BenchmarkApplyRouteExecute(b *testing.B) {
	base := FromFunc(func(context.Context, int) (int, error) {
		return 1, nil
	})

	handler := ApplyRoute(
		base,
		Timeout[int, int](0),
		RetryIf[int, int](1, 0, func(context.Context, int, error) bool { return false }),
	)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_, err := InvokeRouteHandler(context.Background(), 0, handler)
		if err != nil {
			b.Fatalf("execute returned error: %v", err)
		}
	}
}

func BenchmarkRetryIfNoRetry(b *testing.B) {
	base := FromFunc(func(context.Context, int) (int, error) {
		return 0, errors.New("stop")
	})
	handler := ApplyRoute(base, RetryIf[int, int](3, 0, func(context.Context, int, error) bool { return false }))

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_, err := InvokeRouteHandler(context.Background(), 0, handler)
		if err == nil {
			b.Fatal("expected error")
		}
	}
}

func BenchmarkRoundRobinExecute(b *testing.B) {
	handler := RoundRobin(
		FromFunc(func(context.Context, int) (int, error) { return 1, nil }),
		FromFunc(func(context.Context, int) (int, error) { return 2, nil }),
	)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_, err := InvokeRouteHandler(context.Background(), 0, handler)
		if err != nil {
			b.Fatalf("execute returned error: %v", err)
		}
	}
}

func BenchmarkFirstCompleted(b *testing.B) {
	handler := FirstCompleted(
		FromFunc(func(context.Context, int) (int, error) {
			return 1, nil
		}),
		FromFunc(func(context.Context, int) (int, error) {
			return 2, nil
		}),
	)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_, err := InvokeRouteHandler(context.Background(), 0, handler)
		if err != nil {
			b.Fatalf("execute returned error: %v", err)
		}
	}
}

func BenchmarkWeightBasedRouter(b *testing.B) {
	handler := WeightBasedRouter(
		func(context.Context, int) (int, error) { return 1, nil },
		2,
		FromFunc(func(context.Context, int) (int, error) { return 1, nil }),
		FromFunc(func(context.Context, int) (int, error) { return 2, nil }),
	)

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_, err := InvokeRouteHandler(context.Background(), 0, handler)
		if err != nil {
			b.Fatalf("execute returned error: %v", err)
		}
	}
}
