package routery_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/skosovsky/routery"
)

func ExampleApply() {
	attempts := 0
	retryErr := errors.New("retry")

	base := routery.HandlerFunc[int, string](func(context.Context, int) (routery.RouteResult[string], error) {
		attempts++
		if attempts == 1 {
			return routery.RouteResult[string]{}, retryErr
		}
		return routery.Handled("ok"), nil
	})

	executor := routery.Apply(
		base,
		routery.RetryIf[int, string](2, 0, func(_ context.Context, _ int, err error) bool {
			return errors.Is(err, retryErr)
		}),
	)

	result, err := executor.Handle(context.Background(), 0)
	fmt.Println(result.Payload, err == nil, attempts)
	// Output: ok true 2
}

func ExampleFirstCompleted() {
	executor := routery.FirstCompleted[int, string](
		routery.HandlerFunc[int, string](func(context.Context, int) (routery.RouteResult[string], error) {
			return routery.RouteResult[string]{}, errors.New("failed")
		}),
		routery.HandlerFunc[int, string](func(context.Context, int) (routery.RouteResult[string], error) {
			return routery.Handled("fast"), nil
		}),
	)

	result, err := executor.Handle(context.Background(), 0)
	fmt.Println(result.Payload, err == nil)
	// Output: fast true
}

func ExampleWeightBasedRouter() {
	executor := routery.WeightBasedRouter[int, string](
		func(context.Context, int) (int, error) {
			return 1, nil
		},
		2,
		routery.HandlerFunc[int, string](func(context.Context, int) (routery.RouteResult[string], error) {
			return routery.Handled("light"), nil
		}),
		routery.HandlerFunc[int, string](func(context.Context, int) (routery.RouteResult[string], error) {
			return routery.Handled("heavy"), nil
		}),
	)

	result, err := executor.Handle(context.Background(), 0)
	fmt.Println(result.Payload, err == nil)
	// Output: light true
}

func ExampleApply_middlewareOrder() {
	const (
		attempts = 2 + 1
		backoff  = 30 * time.Millisecond
		timeout  = 20 * time.Millisecond
	)

	retryErr := errors.New("retry")

	perAttemptCalls := 0
	perAttemptBase := routery.HandlerFunc[int, string](func(context.Context, int) (routery.RouteResult[string], error) {
		perAttemptCalls++
		return routery.RouteResult[string]{}, retryErr
	})

	perAttempt := routery.Apply(
		perAttemptBase,
		routery.RetryIf[int, string](attempts, backoff, func(_ context.Context, _ int, err error) bool {
			return errors.Is(err, retryErr)
		}),
		routery.Timeout[int, string](timeout),
	)

	_, perAttemptErr := perAttempt.Handle(context.Background(), 0)
	fmt.Println(perAttemptCalls, errors.Is(perAttemptErr, retryErr))

	globalCalls := 0
	globalBase := routery.HandlerFunc[int, string](func(context.Context, int) (routery.RouteResult[string], error) {
		globalCalls++
		return routery.RouteResult[string]{}, retryErr
	})

	global := routery.Apply(
		globalBase,
		routery.Timeout[int, string](timeout),
		routery.RetryIf[int, string](attempts, backoff, func(_ context.Context, _ int, err error) bool {
			return errors.Is(err, retryErr)
		}),
	)

	_, globalErr := global.Handle(context.Background(), 0)
	fmt.Println(globalCalls, errors.Is(globalErr, context.DeadlineExceeded))
	// Output:
	// 3 true
	// 1 true
}

func ExampleCircuitBreaker() {
	fail := errors.New("fail")
	base := routery.HandlerFunc[int, int](func(context.Context, int) (routery.RouteResult[int], error) {
		return routery.RouteResult[int]{}, fail
	})

	executor := routery.Apply(
		base,
		routery.CircuitBreaker[int, int](1, time.Hour, nil),
	)

	_, err1 := executor.Handle(context.Background(), 0)
	_, err2 := executor.Handle(context.Background(), 0)
	fmt.Println(errors.Is(err1, fail), errors.Is(err2, routery.ErrCircuitOpen))
	// Output: true true
}

func ExampleBulkhead() {
	base := routery.HandlerFunc[string, int](func(context.Context, string) (routery.RouteResult[int], error) {
		return routery.Handled(7), nil
	})

	executor := routery.Apply(
		base,
		routery.Bulkhead[string, int](2),
	)

	res, err := executor.Handle(context.Background(), "req")
	fmt.Println(res.Payload, err == nil)
	// Output: 7 true
}
