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

	base := HandlerFunc[int, int](func(context.Context, int) (RouteResult[int], error) {
		callOrder = append(callOrder, "base")
		return Handled(1), nil
	})

	first := func(next Handler[int, int]) Handler[int, int] {
		return HandlerFunc[int, int](func(ctx context.Context, req int) (RouteResult[int], error) {
			callOrder = append(callOrder, "first-before")
			res, err := next.Handle(ctx, req)
			callOrder = append(callOrder, "first-after")
			return res, err
		})
	}

	second := func(next Handler[int, int]) Handler[int, int] {
		return HandlerFunc[int, int](func(ctx context.Context, req int) (RouteResult[int], error) {
			callOrder = append(callOrder, "second-before")
			res, err := next.Handle(ctx, req)
			callOrder = append(callOrder, "second-after")
			return res, err
		})
	}

	executor := Apply(base, first, second)
	_, err := executor.Handle(context.Background(), 0)
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

	base := HandlerFunc[int, int](func(context.Context, int) (RouteResult[int], error) {
		return Handled(1), nil
	})

	observed := func(next Handler[int, int]) Handler[int, int] {
		return HandlerFunc[int, int](func(ctx context.Context, req int) (RouteResult[int], error) {
			called = true
			return next.Handle(ctx, req)
		})
	}

	executor := Apply(base, nil, observed, nil)
	_, err := executor.Handle(context.Background(), 0)
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
	_, err := executor.Handle(context.Background(), 0)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestApplyReturnsConfigErrorWhenMiddlewareReturnsNil(t *testing.T) {
	t.Parallel()

	base := HandlerFunc[int, int](func(context.Context, int) (RouteResult[int], error) {
		return Handled(1), nil
	})

	broken := func(Handler[int, int]) Handler[int, int] {
		return nil
	}

	executor := Apply(base, broken)
	_, err := executor.Handle(context.Background(), 0)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}
