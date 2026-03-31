package routeryhttp

import (
	"context"
	"errors"
	"io"
	"net"
	stdhttp "net/http"
	"net/url"
	"strings"
)

type requestError struct {
	request *stdhttp.Request
	err     error
}

func (err *requestError) Error() string {
	return err.err.Error()
}

func (err *requestError) Unwrap() error {
	return err.err
}

// IsRetryableStatus reports whether code is retryable by the default policy.
func IsRetryableStatus(code int) bool {
	switch code {
	case stdhttp.StatusTooManyRequests,
		stdhttp.StatusBadGateway,
		stdhttp.StatusServiceUnavailable,
		stdhttp.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// DefaultRetryPolicy is a conservative retry policy for HTTP execution.
//
// It retries transport failures and selected HTTP status codes while keeping
// request-method and request-body replay safety checks.
func DefaultRetryPolicy(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	var statusErr *StatusError
	if errors.As(err, &statusErr) {
		return shouldRetryStatus(statusErr)
	}

	var reqErr *requestError
	if errors.As(err, &reqErr) {
		return shouldRetryTransport(reqErr)
	}

	return false
}

func shouldRetryStatus(err *StatusError) bool {
	if err == nil || !IsRetryableStatus(err.Code) {
		return false
	}
	if !isStatusMethodRetryable(err.Request, err.Code) || !isReplayableRequest(err.Request) {
		return false
	}

	if err.Response != nil && err.Response.Body != nil {
		_ = err.Response.Body.Close()
	}

	return true
}

func shouldRetryTransport(err *requestError) bool {
	if err == nil {
		return false
	}
	if !isIdempotentMethod(normalizeMethod(err.request)) {
		return false
	}
	if !isReplayableRequest(err.request) {
		return false
	}

	return isRetryableTransportError(err.err)
}

func isRetryableTransportError(err error) bool {
	if err == nil {
		return false
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return isRetryableTransportError(urlErr.Err)
	}

	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}

	var netErr net.Error
	return errors.As(err, &netErr)
}

func normalizeMethod(request *stdhttp.Request) string {
	if request == nil || request.Method == "" {
		return stdhttp.MethodGet
	}

	return strings.ToUpper(request.Method)
}

func isIdempotentMethod(method string) bool {
	switch method {
	case stdhttp.MethodGet, stdhttp.MethodHead, stdhttp.MethodOptions, stdhttp.MethodPut:
		return true
	default:
		return false
	}
}

func isStatusMethodRetryable(request *stdhttp.Request, statusCode int) bool {
	method := normalizeMethod(request)
	if isIdempotentMethod(method) {
		return true
	}
	if method == stdhttp.MethodPost || method == stdhttp.MethodPatch {
		return statusCode == stdhttp.StatusServiceUnavailable
	}

	return false
}

func isReplayableRequest(request *stdhttp.Request) bool {
	if request == nil {
		return false
	}
	if request.Body == nil || request.Body == stdhttp.NoBody {
		return true
	}

	return request.GetBody != nil
}
