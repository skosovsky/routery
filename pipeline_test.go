package routery

import (
	"context"
	"errors"
	"testing"
)

func TestPipelineBuildReturnsIgnoredWhenNoRuleMatches(t *testing.T) {
	t.Parallel()

	router := NewPipeline[int, int]().
		Match(func(_ context.Context, req int) bool { return req < 0 }, HandlerFunc[int, int](
			func(context.Context, int) (RouteResult[int], error) {
				return Handled(1), nil
			},
		)).
		Build()

	result, err := router.Route(context.Background(), 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != StatusIgnored || result.ReasonCode != "no_match" {
		t.Fatalf("got status=%q reason=%q, want ignored/no_match", result.Status, result.ReasonCode)
	}
}

func TestPipelineMatchRunsFirstMatchingHandler(t *testing.T) {
	t.Parallel()

	calls := 0
	router := NewPipeline[int, int]().
		Match(func(_ context.Context, req int) bool { return req == 1 }, HandlerFunc[int, int](
			func(context.Context, int) (RouteResult[int], error) {
				calls++
				return Handled(10), nil
			},
		)).
		Match(func(_ context.Context, _ int) bool { return true }, HandlerFunc[int, int](
			func(context.Context, int) (RouteResult[int], error) {
				calls++
				return Handled(20), nil
			},
		)).
		Build()

	result, err := router.Route(context.Background(), 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Payload != 10 || calls != 1 {
		t.Fatalf("got payload=%d calls=%d, want payload=10 calls=1", result.Payload, calls)
	}
}

func TestPipelineStatusNextContinuesToNextRule(t *testing.T) {
	t.Parallel()

	router := NewPipeline[int, int]().
		Match(func(_ context.Context, _ int) bool { return true }, HandlerFunc[int, int](
			func(context.Context, int) (RouteResult[int], error) {
				return Next[int]("skip"), nil
			},
		)).
		Match(func(_ context.Context, _ int) bool { return true }, HandlerFunc[int, int](
			func(context.Context, int) (RouteResult[int], error) {
				return Handled(99), nil
			},
		)).
		Build()

	result, err := router.Route(context.Background(), 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Payload != 99 {
		t.Fatalf("got payload=%d, want 99", result.Payload)
	}
}

func TestPipelineWithFallback(t *testing.T) {
	t.Parallel()

	router := NewPipeline[int, int]().
		Match(func(_ context.Context, req int) bool { return req > 0 }, HandlerFunc[int, int](
			func(context.Context, int) (RouteResult[int], error) {
				return Handled(1), nil
			},
		)).
		WithFallback(HandlerFunc[int, int](func(context.Context, int) (RouteResult[int], error) {
			return Handled(2), nil
		}))

	result, err := router.Route(context.Background(), -1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Payload != 2 {
		t.Fatalf("got payload=%d, want 2", result.Payload)
	}
}

func TestPipelineReturnsHandlerError(t *testing.T) {
	t.Parallel()

	want := errors.New("boom")
	router := NewPipeline[int, int]().
		Match(func(_ context.Context, _ int) bool { return true }, HandlerFunc[int, int](
			func(context.Context, int) (RouteResult[int], error) {
				return zeroRouteResult[int](), want
			},
		)).
		Build()

	_, err := router.Route(context.Background(), 0)
	if !errors.Is(err, want) {
		t.Fatalf("got %v, want %v", err, want)
	}
}

func TestPipelineNilHandlerReturnsConfigError(t *testing.T) {
	t.Parallel()

	router := NewPipeline[int, int]().
		Match(func(_ context.Context, _ int) bool { return true }, nil).
		Build()

	_, err := router.Route(context.Background(), 0)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("got %v, want ErrInvalidConfig", err)
	}
}

func TestPipelineWithFallbackNilReturnsConfigError(t *testing.T) {
	t.Parallel()

	router := NewPipeline[int, int]().
		Match(func(_ context.Context, _ int) bool { return false }, HandlerFunc[int, int](
			func(context.Context, int) (RouteResult[int], error) {
				return Handled(1), nil
			},
		)).
		WithFallback(nil)

	_, err := router.Route(context.Background(), 0)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("got %v, want ErrInvalidConfig", err)
	}
}
