package routery

import (
	"context"
	"math"
	"math/rand/v2"
	"sync/atomic"
	"time"
)

// Fallback executes primary first and uses secondary when primary returns an error.
func Fallback[Req any, Kind comparable, Reason comparable, Payload any](
	primary RouteHandler[Req, Kind, Reason, Payload],
	secondary RouteHandler[Req, Kind, Reason, Payload],
) RouteHandler[Req, Kind, Reason, Payload] {
	if primary == nil || secondary == nil {
		return invalidRouteHandler[Req, Kind, Reason, Payload](
			configError("fallback requires non-nil primary and secondary route handlers"),
		)
	}

	return func(call RouteCall[Req]) (RouteResult[Kind, Reason, Payload], error) {
		result, err := primary(call)
		if err == nil {
			return result, nil
		}

		return secondary(call)
	}
}

// RetryIf retries execution when predicate returns true for a system error.
func RetryIf[Req any, Kind comparable, Reason comparable, Payload any](
	attempts int,
	backoff time.Duration,
	predicate RetryPredicate[Req],
) RouteMiddleware[Req, Kind, Reason, Payload] {
	normalizedAttempts := max(attempts, 1)
	normalizedBackoff := max(backoff, time.Duration(0))
	if predicate == nil {
		return func(RouteHandler[Req, Kind, Reason, Payload]) RouteHandler[Req, Kind, Reason, Payload] {
			return invalidRouteHandler[Req, Kind, Reason, Payload](configError("retry predicate is nil"))
		}
	}

	return func(next RouteHandler[Req, Kind, Reason, Payload]) RouteHandler[Req, Kind, Reason, Payload] {
		if next == nil {
			return invalidRouteHandler[Req, Kind, Reason, Payload](
				configError("retry middleware requires non-nil next route handler"),
			)
		}

		return func(call RouteCall[Req]) (RouteResult[Kind, Reason, Payload], error) {
			return executeWithRetry(call, next, normalizedAttempts, normalizedBackoff, predicate)
		}
	}
}

// RoundRobin distributes requests among route handlers sequentially.
func RoundRobin[Req any, Kind comparable, Reason comparable, Payload any](
	handlers ...RouteHandler[Req, Kind, Reason, Payload],
) RouteHandler[Req, Kind, Reason, Payload] {
	validated, err := validateRouteHandlers(handlers, "round robin")
	if err != nil {
		return invalidRouteHandler[Req, Kind, Reason, Payload](err)
	}

	handler := &roundRobinHandler[Req, Kind, Reason, Payload]{
		handlers: validated,
		next:     atomic.Uint64{},
	}

	return handler.route
}

func (handler *roundRobinHandler[Req, Kind, Reason, Payload]) route(
	call RouteCall[Req],
) (RouteResult[Kind, Reason, Payload], error) {
	handlerCount := uint64(len(handler.handlers))
	index := (handler.next.Add(1) - 1) % handlerCount
	return handler.handlers[index](call)
}

// Timeout limits execution time for the wrapped route handler.
func Timeout[Req any, Kind comparable, Reason comparable, Payload any](
	timeout time.Duration,
) RouteMiddleware[Req, Kind, Reason, Payload] {
	if timeout <= 0 {
		return func(next RouteHandler[Req, Kind, Reason, Payload]) RouteHandler[Req, Kind, Reason, Payload] {
			if next == nil {
				return invalidRouteHandler[Req, Kind, Reason, Payload](
					configError("timeout middleware requires non-nil next route handler"),
				)
			}
			return next
		}
	}

	return func(next RouteHandler[Req, Kind, Reason, Payload]) RouteHandler[Req, Kind, Reason, Payload] {
		if next == nil {
			return invalidRouteHandler[Req, Kind, Reason, Payload](
				configError("timeout middleware requires non-nil next route handler"),
			)
		}

		return func(call RouteCall[Req]) (RouteResult[Kind, Reason, Payload], error) {
			timedCtx, cancel := context.WithTimeout(call.Context, timeout)
			defer cancel()

			return next(call.withContext(timedCtx))
		}
	}
}

type roundRobinHandler[Req any, Kind comparable, Reason comparable, Payload any] struct {
	handlers []RouteHandler[Req, Kind, Reason, Payload]
	next     atomic.Uint64
}

func executeWithRetry[Req any, Kind comparable, Reason comparable, Payload any](
	call RouteCall[Req],
	next RouteHandler[Req, Kind, Reason, Payload],
	attempts int,
	backoff time.Duration,
	predicate RetryPredicate[Req],
) (RouteResult[Kind, Reason, Payload], error) {
	wait := backoff
	var lastErr error

	for attemptIndex := range attempts {
		result, err := next(call)
		if err == nil {
			return result, nil
		}

		lastErr = err
		isFinalAttempt := attemptIndex == attempts-1
		if isFinalAttempt || !predicate(call.Context, call.Request, lastErr) {
			return AbortResult[Kind, Reason, Payload]().WithMatch(result.Match), lastErr
		}

		if wait > 0 {
			if err := sleepWithContext(call.Context, wait); err != nil {
				return AbortResult[Kind, Reason, Payload](), err
			}
			wait = growBackoff(wait)
		}
	}

	return AbortResult[Kind, Reason, Payload](), lastErr
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
