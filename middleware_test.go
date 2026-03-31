package routery

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestApplyAppliesMiddlewaresInReverseOrder(t *testing.T) {
	t.Parallel()

	callOrder := make([]string, 0, 2+2)

	base := ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		callOrder = append(callOrder, "base")
		return 1, nil
	})

	first := func(next Executor[int, int]) Executor[int, int] {
		return ExecutorFunc[int, int](func(ctx context.Context, req int) (int, error) {
			callOrder = append(callOrder, "first-before")
			res, err := next.Execute(ctx, req)
			callOrder = append(callOrder, "first-after")
			return res, err
		})
	}

	second := func(next Executor[int, int]) Executor[int, int] {
		return ExecutorFunc[int, int](func(ctx context.Context, req int) (int, error) {
			callOrder = append(callOrder, "second-before")
			res, err := next.Execute(ctx, req)
			callOrder = append(callOrder, "second-after")
			return res, err
		})
	}

	executor := Apply(base, first, second)
	_, err := executor.Execute(context.Background(), 0)
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}

	expected := []string{"first-before", "second-before", "base", "second-after", "first-after"}
	if !slices.Equal(callOrder, expected) {
		t.Fatalf("unexpected call order: got %v, want %v", callOrder, expected)
	}
}

func TestApplySkipsNilMiddlewares(t *testing.T) {
	t.Parallel()

	called := false

	base := ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		return 1, nil
	})

	observed := func(next Executor[int, int]) Executor[int, int] {
		return ExecutorFunc[int, int](func(ctx context.Context, req int) (int, error) {
			called = true
			return next.Execute(ctx, req)
		})
	}

	executor := Apply(base, nil, observed, nil)
	_, err := executor.Execute(context.Background(), 0)
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected non-nil middleware to be called")
	}
}

func TestApplyReturnsConfigErrorForNilBase(t *testing.T) {
	t.Parallel()

	executor := Apply[int, int](nil)
	_, err := executor.Execute(context.Background(), 0)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestApplyReturnsConfigErrorWhenMiddlewareReturnsNil(t *testing.T) {
	t.Parallel()

	base := ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		return 1, nil
	})

	broken := func(Executor[int, int]) Executor[int, int] {
		return nil
	}

	executor := Apply(base, broken)
	_, err := executor.Execute(context.Background(), 0)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}
