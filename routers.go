package routery

import (
	"context"
	"math"
	"math/rand/v2"
	"sync/atomic"
	"time"
)

// Fallback executes primary first and uses secondary when primary returns an error.
//
// Business route statuses from primary are returned as-is when err is nil.
func Fallback[TReq any, TRes any](primary Handler[TReq, TRes], secondary Handler[TReq, TRes]) Handler[TReq, TRes] {
	if primary == nil || secondary == nil {
		return invalidHandler[TReq, TRes](configError("fallback requires non-nil primary and secondary handlers"))
	}

	return HandlerFunc[TReq, TRes](func(ctx context.Context, req TReq) (RouteResult[TRes], error) {
		result, err := primary.Handle(ctx, req)
		if err == nil {
			return result, nil
		}

		return secondary.Handle(ctx, req)
	})
}

// RetryIf retries execution when predicate returns true for a system error.
//
// StatusNext, StatusIgnored, StatusHandled, and StatusAsync never trigger retries.
func RetryIf[TReq any, TRes any](
	attempts int,
	backoff time.Duration,
	predicate RetryPredicate[TReq],
) HandlerMiddleware[TReq, TRes] {
	normalizedAttempts := max(attempts, 1)
	normalizedBackoff := max(backoff, time.Duration(0))
	if predicate == nil {
		return func(Handler[TReq, TRes]) Handler[TReq, TRes] {
			return invalidHandler[TReq, TRes](configError("retry predicate is nil"))
		}
	}

	return func(next Handler[TReq, TRes]) Handler[TReq, TRes] {
		if next == nil {
			return invalidHandler[TReq, TRes](configError("retry middleware requires non-nil next handler"))
		}

		return HandlerFunc[TReq, TRes](func(ctx context.Context, req TReq) (RouteResult[TRes], error) {
			return executeWithRetry(ctx, req, next, normalizedAttempts, normalizedBackoff, predicate)
		})
	}
}

// RoundRobin distributes requests among handlers sequentially.
func RoundRobin[TReq any, TRes any](handlers ...Handler[TReq, TRes]) Handler[TReq, TRes] {
	validated, err := validateHandlers(handlers, "round robin")
	if err != nil {
		return invalidHandler[TReq, TRes](err)
	}

	return &roundRobinHandler[TReq, TRes]{
		handlers: validated,
		next:     atomic.Uint64{},
	}
}

// Timeout limits execution time for the wrapped handler.
func Timeout[TReq any, TRes any](timeout time.Duration) HandlerMiddleware[TReq, TRes] {
	if timeout <= 0 {
		return func(next Handler[TReq, TRes]) Handler[TReq, TRes] {
			if next == nil {
				return invalidHandler[TReq, TRes](configError("timeout middleware requires non-nil next handler"))
			}
			return next
		}
	}

	return func(next Handler[TReq, TRes]) Handler[TReq, TRes] {
		if next == nil {
			return invalidHandler[TReq, TRes](configError("timeout middleware requires non-nil next handler"))
		}

		return HandlerFunc[TReq, TRes](func(ctx context.Context, req TReq) (RouteResult[TRes], error) {
			timedCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			return next.Handle(timedCtx, req)
		})
	}
}

type roundRobinHandler[TReq any, TRes any] struct {
	handlers []Handler[TReq, TRes]
	next     atomic.Uint64
}

func (handler *roundRobinHandler[TReq, TRes]) Handle(ctx context.Context, req TReq) (RouteResult[TRes], error) {
	handlerCount := uint64(len(handler.handlers))
	index := (handler.next.Add(1) - 1) % handlerCount
	return handler.handlers[index].Handle(ctx, req)
}

func executeWithRetry[TReq any, TRes any](
	ctx context.Context,
	req TReq,
	next Handler[TReq, TRes],
	attempts int,
	backoff time.Duration,
	predicate RetryPredicate[TReq],
) (RouteResult[TRes], error) {
	wait := backoff
	var (
		lastErr error
		result  RouteResult[TRes]
	)

	for attemptIndex := range attempts {
		result, lastErr = next.Handle(ctx, req)
		if lastErr == nil {
			return result, nil
		}

		isFinalAttempt := attemptIndex == attempts-1
		if isFinalAttempt || !predicate(ctx, req, lastErr) {
			return result, lastErr
		}

		if wait > 0 {
			if err := sleepWithContext(ctx, wait); err != nil {
				return zeroRouteResult[TRes](), err
			}
			wait = growBackoff(wait)
		}
	}

	return result, lastErr
}

func growBackoff(current time.Duration) time.Duration {
	doubled := safeDoubleBackoff(current)
	if doubled <= 0 {
		return doubled
	}

	capHalf := doubled / 2
	if capHalf <= 0 {
		return doubled
	}

	jitter := rand.Int64N(int64(capHalf)) //nolint:gosec // G404: backoff jitter; crypto/rand is unnecessary.
	return capHalf + time.Duration(jitter)
}

func safeDoubleBackoff(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	maxHalf := time.Duration(math.MaxInt64 / 2)
	if d > maxHalf {
		return time.Duration(math.MaxInt64)
	}
	doubled := d * 2
	if doubled < d {
		return time.Duration(math.MaxInt64)
	}
	return doubled
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}

	timer := time.NewTimer(delay)
	defer func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
