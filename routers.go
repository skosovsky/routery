package routery

import (
	"context"
	"math"
	"math/rand/v2"
	"sync/atomic"
	"time"
)

// Fallback executes primary first and uses secondary when primary returns an error.
func Fallback[Req any, Res any](
	primary RouteHandler[Req, Res],
	secondary RouteHandler[Req, Res],
) RouteHandler[Req, Res] {
	if primary == nil || secondary == nil {
		return invalidRouteHandler[Req, Res](
			configError("fallback requires non-nil primary and secondary route handlers"),
		)
	}

	return func(ctx context.Context, req Req, rec ResultRecorder[Res]) error {
		localPrimary := NewResultRecorder[Res]()
		err := primary(ctx, req, localPrimary)
		if err == nil {
			CopyRecorderOutcome(rec, localPrimary)
			return nil
		}

		localSecondary := NewResultRecorder[Res]()
		if err := secondary(ctx, req, localSecondary); err != nil {
			return err
		}
		CopyRecorderOutcome(rec, localSecondary)

		return nil
	}
}

// RetryIf retries execution when predicate returns true for a system error.
func RetryIf[Req any, Res any](
	attempts int,
	backoff time.Duration,
	predicate RetryPredicate[Req],
) RouteMiddleware[Req, Res] {
	normalizedAttempts := max(attempts, 1)
	normalizedBackoff := max(backoff, time.Duration(0))
	if predicate == nil {
		return func(RouteHandler[Req, Res]) RouteHandler[Req, Res] {
			return invalidRouteHandler[Req, Res](configError("retry predicate is nil"))
		}
	}

	return func(next RouteHandler[Req, Res]) RouteHandler[Req, Res] {
		if next == nil {
			return invalidRouteHandler[Req, Res](configError("retry middleware requires non-nil next route handler"))
		}

		return func(ctx context.Context, req Req, rec ResultRecorder[Res]) error {
			return executeWithRetry(ctx, req, rec, next, normalizedAttempts, normalizedBackoff, predicate)
		}
	}
}

// RoundRobin distributes requests among route handlers sequentially.
func RoundRobin[Req any, Res any](handlers ...RouteHandler[Req, Res]) RouteHandler[Req, Res] {
	validated, err := validateRouteHandlers(handlers, "round robin")
	if err != nil {
		return invalidRouteHandler[Req, Res](err)
	}

	handler := &roundRobinHandler[Req, Res]{
		handlers: validated,
		next:     atomic.Uint64{},
	}

	return func(ctx context.Context, req Req, rec ResultRecorder[Res]) error {
		return handler.route(ctx, req, rec)
	}
}

func (handler *roundRobinHandler[Req, Res]) route(
	ctx context.Context,
	req Req,
	rec ResultRecorder[Res],
) error {
	handlerCount := uint64(len(handler.handlers))
	index := (handler.next.Add(1) - 1) % handlerCount
	return handler.handlers[index](ctx, req, rec)
}

// Timeout limits execution time for the wrapped route handler.
func Timeout[Req any, Res any](timeout time.Duration) RouteMiddleware[Req, Res] {
	if timeout <= 0 {
		return func(next RouteHandler[Req, Res]) RouteHandler[Req, Res] {
			if next == nil {
				return invalidRouteHandler[Req, Res](
					configError("timeout middleware requires non-nil next route handler"),
				)
			}
			return next
		}
	}

	return func(next RouteHandler[Req, Res]) RouteHandler[Req, Res] {
		if next == nil {
			return invalidRouteHandler[Req, Res](configError("timeout middleware requires non-nil next route handler"))
		}

		return func(ctx context.Context, req Req, rec ResultRecorder[Res]) error {
			timedCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			localRec := NewResultRecorder[Res]()
			err := next(timedCtx, req, localRec)
			if err != nil {
				return err
			}
			CopyRecorderOutcome(rec, localRec)

			return nil
		}
	}
}

type roundRobinHandler[Req any, Res any] struct {
	handlers []RouteHandler[Req, Res]
	next     atomic.Uint64
}

func executeWithRetry[Req any, Res any](
	ctx context.Context,
	req Req,
	rec ResultRecorder[Res],
	next RouteHandler[Req, Res],
	attempts int,
	backoff time.Duration,
	predicate RetryPredicate[Req],
) error {
	wait := backoff
	var lastErr error

	for attemptIndex := range attempts {
		localRec := NewResultRecorder[Res]()
		lastErr = next(ctx, req, localRec)
		if lastErr == nil {
			CopyRecorderOutcome(rec, localRec)
			return nil
		}

		isFinalAttempt := attemptIndex == attempts-1
		if isFinalAttempt || !predicate(ctx, req, lastErr) {
			return lastErr
		}

		if wait > 0 {
			if err := sleepWithContext(ctx, wait); err != nil {
				return err
			}
			wait = growBackoff(wait)
		}
	}

	return lastErr
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
