package routeryredis

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"

	"github.com/skosovsky/routery"
	"github.com/skosovsky/routery/policy/attempt"

	"github.com/redis/go-redis/v9"
)

// IsTransientError classifies provider errors without authorizing repetition.
// Replay safety must be provided separately through RetryPolicy or execution.Sequence.
func IsTransientError(err error) bool {
	if err == nil || errors.Is(err, routery.ErrInvalidConfig) {
		return false
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	if errors.Is(err, redis.Nil) {
		return false
	}

	if errors.Is(err, redis.TxFailedErr) {
		return true
	}

	if isRedisAuthOrSyntax(err) {
		return false
	}

	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}

	if errors.Is(err, io.EOF) {
		return true
	}

	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "connection reset") || strings.Contains(msg, "broken pipe") {
		return true
	}

	return false
}

func isRedisAuthOrSyntax(err error) bool {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "noauth"),
		strings.Contains(msg, "wrongpass"),
		strings.Contains(msg, "invalid password"),
		strings.Contains(msg, "noperm"),
		strings.Contains(msg, "syntax error"),
		strings.Contains(msg, "wrongtype"),
		strings.Contains(msg, "unknown command"):
		return true
	default:
		return false
	}
}

// RetryPolicy requires explicit host evidence; nil evidence denies repetition.
func RetryPolicy[Req any](evidence attempt.Evidence[Req]) routery.RetryPredicate[Req] {
	predicate := attempt.RetryPredicate(IsTransientError, evidence)
	return func(ctx context.Context, req Req, err error) bool { return predicate(ctx, req, err) }
}
