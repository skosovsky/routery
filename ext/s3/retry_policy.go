package routerys3

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/skosovsky/routery"
	"github.com/skosovsky/routery/policy/attempt"

	smithy "github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

const (
	httpBadRequest          = http.StatusBadRequest
	httpForbidden           = http.StatusForbidden
	httpNotFound            = http.StatusNotFound
	httpTooManyRequests     = http.StatusTooManyRequests
	httpInternalServerError = http.StatusInternalServerError
	httpBadGateway          = http.StatusBadGateway
	httpServiceUnavailable  = http.StatusServiceUnavailable
	httpGatewayTimeout      = http.StatusGatewayTimeout
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

	if apiErr, ok := errors.AsType[smithy.APIError](err); ok {
		code := strings.ToLower(apiErr.ErrorCode())
		if strings.Contains(code, "slowdown") || strings.Contains(code, "503") {
			return true
		}
	}

	if respErr, ok := errors.AsType[*smithyhttp.ResponseError](err); ok {
		switch respErr.HTTPStatusCode() {
		case httpNotFound, httpForbidden, httpBadRequest:
			return false
		case httpTooManyRequests, httpInternalServerError, httpBadGateway,
			httpServiceUnavailable, httpGatewayTimeout:
			return true
		default:
			return false
		}
	}

	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}

	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "slow down") || strings.Contains(msg, "slowdown") ||
		strings.Contains(msg, "throttl") {
		return true
	}

	return false
}

// RetryPolicy requires explicit host evidence; nil evidence denies repetition.
func RetryPolicy[Req any](evidence attempt.Evidence[Req]) routery.RetryPredicate[Req] {
	predicate := attempt.RetryPredicate(IsTransientError, evidence)
	return func(ctx context.Context, req Req, err error) bool { return predicate(ctx, req, err) }
}
