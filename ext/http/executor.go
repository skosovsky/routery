package routeryhttp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	stdhttp "net/http"
	"sync"
	"time"

	"github.com/skosovsky/routery"
)

const defaultMaxReplayBodyBytes int64 = 10 << 20

// ErrReplayBodyTooLarge indicates that a request body cannot be buffered safely.
var ErrReplayBodyTooLarge = errors.New("routery/ext/http: replay body exceeds limit")

// Option configures the HTTP route handler.
type Option func(*options)

type options struct {
	maxReplayBodyBytes int64
	err                error
}

// WithMaxReplayBodyBytes limits in-memory buffering for replayable request bodies.
func WithMaxReplayBodyBytes(maxBytes int64) Option {
	return func(opts *options) {
		if maxBytes < 0 {
			opts.err = configError("max replay body bytes must be non-negative")
			return
		}

		opts.maxReplayBodyBytes = maxBytes
	}
}

// StatusError represents a non-2xx HTTP response.
type StatusError struct {
	Request  *stdhttp.Request
	Response *stdhttp.Response
	Code     int
}

// Error implements error.
func (err *StatusError) Error() string {
	if err == nil {
		return "routery/ext/http: status error"
	}

	if err.Response != nil {
		return fmt.Sprintf("routery/ext/http: unexpected status %d (%s)", err.Code, err.Response.Status)
	}

	return fmt.Sprintf("routery/ext/http: unexpected status %d", err.Code)
}

// NewRouteHandler adapts a standard HTTP client to a routery route handler.
//
//nolint:bodyclose // Response bodies are returned through RouteResult or StatusError for caller ownership.
func NewRouteHandler(
	client *stdhttp.Client,
	handlerOptions ...Option,
) routery.BasicRouteHandler[*stdhttp.Request, *stdhttp.Response] {
	if client == nil {
		return invalidRouteHandler(configError("http client is nil"))
	}

	opts := applyOptions(handlerOptions)
	if opts.err != nil {
		return invalidRouteHandler(opts.err)
	}

	return func(call routery.RouteCall[*stdhttp.Request]) (routery.BasicRouteResult[*stdhttp.Response], error) {
		request := call.Request
		if request == nil {
			return routery.AbortResult[routery.BasicKind, routery.BasicReason, *stdhttp.Response](),
				configError("request is nil")
		}

		attemptRequest, err := cloneForAttempt(call.Context, request, opts.maxReplayBodyBytes)
		if err != nil {
			return routery.AbortResult[routery.BasicKind, routery.BasicReason, *stdhttp.Response](), err
		}

		response, executeErr := client.Do(attemptRequest)
		if executeErr != nil {
			if response != nil && response.Body != nil {
				_ = response.Body.Close()
			}

			return routery.AbortResult[routery.BasicKind, routery.BasicReason, *stdhttp.Response](), executeErr
		}

		if response.StatusCode < stdhttp.StatusOK || response.StatusCode >= stdhttp.StatusMultipleChoices {
			return routery.AbortResult[routery.BasicKind, routery.BasicReason, *stdhttp.Response](), &StatusError{
				Request:  request,
				Response: response,
				Code:     response.StatusCode,
			}
		}

		return routery.BasicHandled(response), nil
	}
}

func applyOptions(handlerOptions []Option) options {
	opts := options{
		maxReplayBodyBytes: defaultMaxReplayBodyBytes,
		err:                nil,
	}
	for _, option := range handlerOptions {
		if option != nil {
			option(&opts)
		}
	}

	return opts
}

func cloneForAttempt(
	ctx context.Context,
	request *stdhttp.Request,
	maxReplayBodyBytes int64,
) (*stdhttp.Request, error) {
	cloned := request.Clone(ctx)

	if request.GetBody == nil {
		if request.Body == nil || request.Body == stdhttp.NoBody {
			return cloned, nil
		}

		if err := materializeBody(request, maxReplayBodyBytes); err != nil {
			return nil, err
		}
	}

	body, err := request.GetBody()
	if err != nil {
		if errors.Is(err, ErrReplayBodyTooLarge) {
			return nil, err
		}

		return nil, fmt.Errorf("routery/ext/http: get body: %w", err)
	}
	cloned.Body = body
	cloned.GetBody = request.GetBody
	cloned.ContentLength = request.ContentLength

	return cloned, nil
}

func materializeBody(request *stdhttp.Request, maxReplayBodyBytes int64) error {
	bodyBytes, err := readAllLimited(request.Body, maxReplayBodyBytes)
	_ = request.Body.Close()
	if err != nil {
		request.Body = stdhttp.NoBody
		request.GetBody = func() (io.ReadCloser, error) {
			return nil, err
		}
		request.ContentLength = 0

		return err
	}

	request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	request.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(bodyBytes)), nil
	}
	request.ContentLength = int64(len(bodyBytes))

	return nil
}

func readAllLimited(body io.Reader, maxReplayBodyBytes int64) ([]byte, error) {
	reader := body
	if maxReplayBodyBytes > 0 {
		reader = io.LimitReader(body, maxReplayBodyBytes+1)
	}

	bodyBytes, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("routery/ext/http: read body: %w", err)
	}
	if maxReplayBodyBytes > 0 && int64(len(bodyBytes)) > maxReplayBodyBytes {
		return nil, fmt.Errorf("%w: %w", routery.ErrInvalidConfig, ErrReplayBodyTooLarge)
	}

	return bodyBytes, nil
}

func configError(detail string) error {
	return fmt.Errorf("%w: %s", routery.ErrInvalidConfig, detail)
}

//nolint:bodyclose // Config-error handlers never create an HTTP response body.
func invalidRouteHandler(
	err error,
) routery.BasicRouteHandler[*stdhttp.Request, *stdhttp.Response] {
	return func(routery.RouteCall[*stdhttp.Request]) (routery.BasicRouteResult[*stdhttp.Response], error) {
		return routery.AbortResult[routery.BasicKind, routery.BasicReason, *stdhttp.Response](), err
	}
}

// Timeout limits HTTP execution time without cancelling response body reads.
//
//nolint:bodyclose // Response bodies remain caller-owned and are wrapped only to cancel the timer on Close.
func Timeout(timeout time.Duration) routery.BasicRouteMiddleware[*stdhttp.Request, *stdhttp.Response] {
	if timeout <= 0 {
		return func(
			next routery.BasicRouteHandler[*stdhttp.Request, *stdhttp.Response],
		) routery.BasicRouteHandler[*stdhttp.Request, *stdhttp.Response] {
			if next == nil {
				return invalidRouteHandler(configError("timeout middleware requires non-nil next route handler"))
			}

			return next
		}
	}

	return func(
		next routery.BasicRouteHandler[*stdhttp.Request, *stdhttp.Response],
	) routery.BasicRouteHandler[*stdhttp.Request, *stdhttp.Response] {
		if next == nil {
			return invalidRouteHandler(configError("timeout middleware requires non-nil next route handler"))
		}

		return func(call routery.RouteCall[*stdhttp.Request]) (routery.BasicRouteResult[*stdhttp.Response], error) {
			timedCtx, cancel := context.WithTimeout(call.Context, timeout)
			defer func() {
				if recovered := recover(); recovered != nil {
					cancel()
					panic(recovered)
				}
			}()

			result, err := next(call.WithContext(timedCtx))
			if err != nil {
				cancel()
				return routery.AbortResult[routery.BasicKind, routery.BasicReason, *stdhttp.Response]().
					WithMatch(result.Match), err
			}

			payload := result.Payload
			if !result.HasPayload || payload == nil || payload.Body == nil || payload.Body == stdhttp.NoBody {
				cancel()
				return result, nil
			}

			payload.Body = &cancelTimerBody{
				ReadCloser: payload.Body,
				cancelOnce: sync.Once{},
				cancel:     cancel,
			}
			result.Payload = payload
			return result, nil
		}
	}
}

type cancelTimerBody struct {
	io.ReadCloser

	cancelOnce sync.Once
	cancel     context.CancelFunc
}

func (body *cancelTimerBody) Close() error {
	defer body.cancelOnce.Do(body.cancel)

	return body.ReadCloser.Close()
}
