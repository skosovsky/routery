package routeryhttp

import (
	"context"
	"errors"
	"io"
	"net"
	stdhttp "net/http"
	"net/url"
	"strings"

	"github.com/skosovsky/routery"
)

// ReplaySafety is caller-owned evidence, independent of request body replayability.
type ReplaySafety uint8

const (
	UnsafeReplay ReplaySafety = iota
	ProvenNotExecuted
	VerifiedDeduplication
)

// RetryPolicy adds explicit replay evidence for non-idempotent requests.
// A header or status code alone is not evidence. The callback belongs to the host.
func RetryPolicy(
	evidence func(context.Context, *stdhttp.Request, error) ReplaySafety,
) routery.RetryPredicate[*stdhttp.Request] {
	return func(ctx context.Context, request *stdhttp.Request, err error) bool {
		if DefaultRetryPolicy(ctx, request, err) {
			return true
		}
		if evidence == nil || ctx.Err() != nil || err == nil || !isReplayableRequest(request) ||
			errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false
		}
		var statusErr *StatusError
		if errors.As(err, &statusErr) {
			if !IsRetryableStatus(statusErr.Code) {
				return false
			}
		} else if !isRetryableTransportError(err) {
			return false
		}
		safety := evidence(ctx, request, err)
		if safety != ProvenNotExecuted && safety != VerifiedDeduplication {
			return false
		}
		if statusErr != nil && statusErr.Response != nil && statusErr.Response.Body != nil {
			_ = statusErr.Response.Body.Close()
		}
		return true
	}
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
// request-method and request-body replay safety checks. The original request
// passed to the handler must be supplied as req (the same value RetryIf forwards).
func DefaultRetryPolicy(ctx context.Context, req *stdhttp.Request, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	if statusErr, ok := errors.AsType[*StatusError](err); ok {
		return shouldRetryStatus(req, statusErr)
	}

	return shouldRetryTransport(req, err)
}

func shouldRetryStatus(req *stdhttp.Request, err *StatusError) bool {
	if err == nil || !IsRetryableStatus(err.Code) {
		return false
	}

	effective := req
	if err.Request != nil {
		effective = err.Request
	}

	if !isStatusMethodRetryable(effective) || !isReplayableRequest(effective) {
		return false
	}

	if err.Response != nil && err.Response.Body != nil {
		_ = err.Response.Body.Close()
	}

	return true
}

func shouldRetryTransport(req *stdhttp.Request, err error) bool {
	if err == nil {
		return false
	}
	if !isIdempotentMethod(normalizeMethod(req)) {
		return false
	}
	if !isReplayableRequest(req) {
		return false
	}

	return isRetryableTransportError(err)
}

func isRetryableTransportError(err error) bool {
	if err == nil {
		return false
	}

	if urlErr, ok := errors.AsType[*url.Error](err); ok {
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

func isStatusMethodRetryable(request *stdhttp.Request) bool {
	return isIdempotentMethod(normalizeMethod(request))
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
