package routery_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/skosovsky/routery"
)

func ExampleApplyRoute() {
	attempts := 0
	retryErr := errors.New("retry")

	base := routery.FromFunc(func(context.Context, int) (string, error) {
		attempts++
		if attempts == 1 {
			return "", retryErr
		}
		return "ok", nil
	})

	handler := routery.ApplyRoute(
		base,
		routery.RetryIf[int, string](2, 0, func(_ context.Context, _ int, err error) bool {
			return errors.Is(err, retryErr)
		}),
	)

	outcome, err := routery.InvokeRouteHandler(context.Background(), 0, handler)
	if outcome.HasPayload {
		fmt.Println(outcome.Payload, err == nil, attempts)
	}
	// Output: ok true 2
}

func ExampleFirstCompleted() {
	handler := routery.FirstCompleted[int, string](
		routery.FromFunc(func(context.Context, int) (string, error) {
			return "", errors.New("failed")
		}),
		routery.FromFunc(func(context.Context, int) (string, error) {
			return "fast", nil
		}),
	)

	outcome, err := routery.InvokeRouteHandler(context.Background(), 0, handler)
	if outcome.HasPayload {
		fmt.Println(outcome.Payload, err == nil)
	}
	// Output: fast true
}

func ExampleWeightBasedRouter() {
	handler := routery.WeightBasedRouter[int, string](
		func(context.Context, int) (int, error) {
			return 1, nil
		},
		2,
		routery.FromFunc(func(context.Context, int) (string, error) {
			return "light", nil
		}),
		routery.FromFunc(func(context.Context, int) (string, error) {
			return "heavy", nil
		}),
	)

	outcome, err := routery.InvokeRouteHandler(context.Background(), 0, handler)
	if outcome.HasPayload {
		fmt.Println(outcome.Payload, err == nil)
	}
	// Output: light true
}

func ExampleApplyRoute_middlewareOrder() {
	const (
		attempts = 2 + 1
		backoff  = 30 * time.Millisecond
		timeout  = 20 * time.Millisecond
	)

	retryErr := errors.New("retry")

	perAttemptCalls := 0
	perAttemptBase := routery.FromFunc(func(context.Context, int) (string, error) {
		perAttemptCalls++
		return "", retryErr
	})

	perAttempt := routery.ApplyRoute(
		perAttemptBase,
		routery.RetryIf[int, string](attempts, backoff, func(_ context.Context, _ int, err error) bool {
			return errors.Is(err, retryErr)
		}),
		routery.Timeout[int, string](timeout),
	)

	_, perAttemptErr := routery.InvokeRouteHandler(context.Background(), 0, perAttempt)
	fmt.Println(perAttemptCalls, errors.Is(perAttemptErr, retryErr))

	globalCalls := 0
	globalBase := routery.FromFunc(func(context.Context, int) (string, error) {
		globalCalls++
		return "", retryErr
	})

	global := routery.ApplyRoute(
		globalBase,
		routery.Timeout[int, string](timeout),
		routery.RetryIf[int, string](attempts, backoff, func(_ context.Context, _ int, err error) bool {
			return errors.Is(err, retryErr)
		}),
	)

	_, globalErr := routery.InvokeRouteHandler(context.Background(), 0, global)
	fmt.Println(globalCalls, errors.Is(globalErr, context.DeadlineExceeded))
	// Output:
	// 3 true
	// 1 true
}

func ExampleCircuitBreaker() {
	fail := errors.New("fail")
	base := routery.FromFunc(func(context.Context, int) (int, error) {
		return 0, fail
	})

	handler := routery.ApplyRoute(
		base,
		routery.CircuitBreaker[int, int](1, time.Hour, nil),
	)

	_, err1 := routery.InvokeRouteHandler(context.Background(), 0, handler)
	_, err2 := routery.InvokeRouteHandler(context.Background(), 0, handler)
	fmt.Println(errors.Is(err1, fail), errors.Is(err2, routery.ErrCircuitOpen))
	// Output: true true
}

func ExampleBulkhead() {
	base := routery.FromFunc(func(context.Context, string) (int, error) {
		return 7, nil
	})

	handler := routery.ApplyRoute(
		base,
		routery.Bulkhead[string, int](2),
	)

	outcome, err := routery.InvokeRouteHandler(context.Background(), "req", handler)
	if outcome.HasPayload {
		fmt.Println(outcome.Payload, err == nil)
	}
	// Output: 7 true
}

func ExampleRouteTable_dispatch() {
	router, err := routery.NewRouteTable[int, string]().
		Route("primary", 10, func(int) bool { return true }, routery.FromFunc(func(context.Context, int) (string, error) {
			return "matched", nil
		})).
		Fallback(routery.FromFunc(func(context.Context, int) (string, error) {
			return "fallback", nil
		})).
		Build()
	if err != nil {
		fmt.Println("build error", err)
		return
	}

	outcome, err := router.Dispatch(context.Background(), 1)
	fmt.Println(outcome.HasPayload, outcome.Payload, err == nil, outcome.ReasonCode, outcome.Action)
	// Output: true matched true  stop
}

func ExampleRouteTable_nestedMount() {
	nested := routery.NewRouteTable[int, string]().
		Route("inner", 1, func(int) bool { return false }, routery.FromFunc(func(context.Context, int) (string, error) {
			return "inner", nil
		}))

	router, err := routery.NewRouteTable[int, string]().
		Mount("group", 10, nil, nested).
		Route("outer", 1, nil, routery.FromFunc(func(context.Context, int) (string, error) {
			return "outer", nil
		})).
		Build()
	if err != nil {
		fmt.Println("build error", err)
		return
	}

	outcome, err := router.Dispatch(context.Background(), 0)
	fmt.Println(outcome.Payload, err == nil)
	// Output: outer true
}
