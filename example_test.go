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

	base := routery.ExecutorFunc[int, string](func(context.Context, int) (string, error) {
		attempts++
		if attempts == 1 {
			return "", retryErr
		}
		return "ok", nil
	})

	executor := routery.Apply(
		base,
		routery.RetryIf[int, string](2, 0, func(err error) bool {
			return errors.Is(err, retryErr)
		}),
	)

	response, err := executor.Execute(context.Background(), 0)
	fmt.Println(response, err == nil, attempts)
	// Output: ok true 2
}

func ExampleFirstCompleted() {
	executor := routery.FirstCompleted[int, string](
		routery.ExecutorFunc[int, string](func(context.Context, int) (string, error) {
			return "", errors.New("failed")
		}),
		routery.ExecutorFunc[int, string](func(context.Context, int) (string, error) {
			return "fast", nil
		}),
	)

	response, err := executor.Execute(context.Background(), 0)
	fmt.Println(response, err == nil)
	// Output: fast true
}

func ExampleWeightBasedRouter() {
	executor := routery.WeightBasedRouter[int, string](
		func(context.Context, int) (int, error) {
			return 1, nil
		},
		2,
		routery.ExecutorFunc[int, string](func(context.Context, int) (string, error) {
			return "light", nil
		}),
		routery.ExecutorFunc[int, string](func(context.Context, int) (string, error) {
			return "heavy", nil
		}),
	)

	response, err := executor.Execute(context.Background(), 0)
	fmt.Println(response, err == nil)
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
	perAttemptBase := routery.ExecutorFunc[int, string](func(context.Context, int) (string, error) {
		perAttemptCalls++
		return "", retryErr
	})

	perAttempt := routery.Apply(
		perAttemptBase,
		routery.RetryIf[int, string](attempts, backoff, func(err error) bool {
			return errors.Is(err, retryErr)
		}),
		routery.Timeout[int, string](timeout),
	)

	_, perAttemptErr := perAttempt.Execute(context.Background(), 0)
	fmt.Println(perAttemptCalls, errors.Is(perAttemptErr, retryErr))

	globalCalls := 0
	globalBase := routery.ExecutorFunc[int, string](func(context.Context, int) (string, error) {
		globalCalls++
		return "", retryErr
	})

	global := routery.Apply(
		globalBase,
		routery.Timeout[int, string](timeout),
		routery.RetryIf[int, string](attempts, backoff, func(err error) bool {
			return errors.Is(err, retryErr)
		}),
	)

	_, globalErr := global.Execute(context.Background(), 0)
	fmt.Println(globalCalls, errors.Is(globalErr, context.DeadlineExceeded))
	// Output:
	// 3 true
	// 1 true
}
