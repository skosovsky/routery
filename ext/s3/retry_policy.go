package routerys3

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"

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

// DefaultRetryPolicy classifies S3 client errors for [github.com/skosovsky/routery.RetryIf].
func DefaultRetryPolicy[Req any](_ context.Context, _ Req, err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code := strings.ToLower(apiErr.ErrorCode())
		if strings.Contains(code, "slowdown") || strings.Contains(code, "503") {
			return true
		}
	}

	var respErr *smithyhttp.ResponseError
	if errors.As(err, &respErr) {
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
