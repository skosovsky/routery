package routery

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestApplyRouteAppliesMiddlewaresInReverseOrder(t *testing.T) {
	t.Parallel()

	callOrder := make([]string, 0, 2+2)

	base := func(_ context.Context, _ int, rec ResultRecorder[int]) error {
		callOrder = append(callOrder, "base")
		rec.Stop(1, "")
		return nil
	}

	first := func(next RouteHandler[int, int]) RouteHandler[int, int] {
		return func(ctx context.Context, req int, rec ResultRecorder[int]) error {
			callOrder = append(callOrder, "first-before")
			err := next(ctx, req, rec)
			callOrder = append(callOrder, "first-after")
			return err
		}
	}

	second := func(next RouteHandler[int, int]) RouteHandler[int, int] {
		return func(ctx context.Context, req int, rec ResultRecorder[int]) error {
			callOrder = append(callOrder, "second-before")
			err := next(ctx, req, rec)
			callOrder = append(callOrder, "second-after")
			return err
		}
	}

	handler := ApplyRoute(base, first, second)
	_, err := InvokeRouteHandler(context.Background(), 0, handler)
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}

	expected := []string{"first-before", "second-before", "base", "second-after", "first-after"}
	if !slices.Equal(callOrder, expected) {
		t.Fatalf("unexpected call order: got %v, want %v", callOrder, expected)
	}
}

func TestApplyRouteSkipsNilMiddlewares(t *testing.T) {
	t.Parallel()

	called := false

	base := FromFunc(func(context.Context, int) (int, error) {
		return 1, nil
	})

	observed := func(next RouteHandler[int, int]) RouteHandler[int, int] {
		return func(ctx context.Context, req int, rec ResultRecorder[int]) error {
			called = true
			return next(ctx, req, rec)
		}
	}

	handler := ApplyRoute(base, nil, observed, nil)
	_, err := InvokeRouteHandler(context.Background(), 0, handler)
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected non-nil middleware to be called")
	}
}

func TestApplyRouteReturnsConfigErrorForNilBase(t *testing.T) {
	t.Parallel()

	handler := ApplyRoute[int, int](nil)
	_, err := InvokeRouteHandler(context.Background(), 0, handler)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestApplyRouteReturnsConfigErrorWhenMiddlewareReturnsNil(t *testing.T) {
	t.Parallel()

	base := FromFunc(func(context.Context, int) (int, error) {
		return 1, nil
	})

	broken := func(RouteHandler[int, int]) RouteHandler[int, int] {
		return nil
	}

	handler := ApplyRoute(base, broken)
	_, err := InvokeRouteHandler(context.Background(), 0, handler)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}
