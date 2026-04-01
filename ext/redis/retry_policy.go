package routeryredis

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"

	"github.com/redis/go-redis/v9"
)

// DefaultRetryPolicy decides whether a Redis execution error should be retried.
//
// It never retries [redis.Nil] (cache miss), client cancellations, or likely auth/syntax errors.
func DefaultRetryPolicy[Req any](_ context.Context, _ Req, err error) bool {
	if err == nil {
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
