package routeryhttp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	stdhttp "net/http"
	"time"

	"github.com/skosovsky/routery"
)

const defaultMaxReplayBodyBytes int64 = 10 << 20

// ErrReplayBodyTooLarge indicates that a request body cannot be buffered safely.
var ErrReplayBodyTooLarge = errors.New("routery/ext/http: replay body exceeds limit")

// Option configures request preparation.
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

	return fmt.Sprintf("routery/ext/http: unexpected status %d", err.Code)
}

// NewRouteHandler adapts a standard HTTP client to a routery route handler.
//
//nolint:bodyclose // Response bodies are returned through RouteResult or StatusError for caller ownership.
func NewRouteHandler(
	client *stdhttp.Client,
) routery.BasicRouteHandler[*stdhttp.Request, *stdhttp.Response] {
	if client == nil {
		return invalidRouteHandler(configError("http client is nil"))
	}

	return func(call routery.RouteCall[*stdhttp.Request]) (routery.BasicRouteResult[*stdhttp.Response], error) {
		request := call.Request
		if request == nil {
			return routery.AbortResult[routery.BasicKind, routery.BasicReason, *stdhttp.Response](),
				configError("request is nil")
		}

		attemptRequest, err := cloneForAttempt(call.Context, request)
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

		life := routery.NewLifetime(response.Body.Close)
		response.Body = &ownedBody{ReadCloser: response.Body, life: life}
		if response.StatusCode < stdhttp.StatusOK || response.StatusCode >= stdhttp.StatusMultipleChoices {
			result := routery.AbortResult[routery.BasicKind, routery.BasicReason, *stdhttp.Response]()
			result.Lifetime = life
			return result, &StatusError{
				Request:  request,
				Response: response,
				Code:     response.StatusCode,
			}
		}

		result := routery.BasicHandled(response)
		result.Lifetime = life
		return result, nil
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
) (*stdhttp.Request, error) {
	if request.Body != nil && request.Body != stdhttp.NoBody && request.GetBody == nil {
		return nil, configError("body requires PrepareRequest before dispatch")
	}
	cloned := request.Clone(ctx)

	if request.GetBody == nil {
		if request.Body == nil || request.Body == stdhttp.NoBody {
			return cloned, nil
		}

		return cloned, nil
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

// PrepareRequest consumes an unprepared body once before retries or parallel dispatch.
// The returned template must remain immutable; GetBody must be concurrency-safe.
// Original request fields are never modified. A body consumed here is always closed.
func PrepareRequest(request *stdhttp.Request, preparationOptions ...Option) (*stdhttp.Request, error) {
	if request == nil {
		return nil, configError("request is nil")
	}
	opts := applyOptions(preparationOptions)
	if opts.err != nil {
		return nil, opts.err
	}
	prepared := request.Clone(request.Context())
	if request.GetBody != nil || request.Body == nil || request.Body == stdhttp.NoBody {
		return prepared, nil
	}
	bodyBytes, err := readAllLimited(request.Body, opts.maxReplayBodyBytes)
	closeErr := request.Body.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, fmt.Errorf("routery/ext/http: close original body: %w", closeErr)
	}

	prepared.Body = stdhttp.NoBody
	prepared.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(bodyBytes)), nil
	}
	prepared.ContentLength = int64(len(bodyBytes))

	return prepared, nil
}

func readAllLimited(body io.Reader, maxReplayBodyBytes int64) ([]byte, error) {
	reader := body
	if maxReplayBodyBytes > 0 {
		// Avoid overflow for the largest supported limit.
		reader = io.LimitReader(body, maxReplayBodyBytes)
	}

	bodyBytes, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("routery/ext/http: read body: %w", err)
	}
	if maxReplayBodyBytes > 0 && int64(len(bodyBytes)) == maxReplayBodyBytes {
		var extra [1]byte
		count, readErr := io.ReadFull(body, extra[:])
		if count > 0 {
			return nil, fmt.Errorf("%w: %w", routery.ErrInvalidConfig, ErrReplayBodyTooLarge)
		}
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, fmt.Errorf("routery/ext/http: read body: %w", readErr)
		}
	}

	return bodyBytes, nil
}

type ownedBody struct {
	io.ReadCloser

	life *routery.Lifetime
}

func (body *ownedBody) Close() error {
	return body.life.Close()
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

// Timeout limits HTTP execution through the generic explicit Lifetime contract.
// Resource handlers must attach a Lifetime; it retains the timed context until Close.
//
//nolint:bodyclose // Generic specialization transfers owned responses to the caller, not this middleware.
func Timeout(timeout time.Duration) routery.BasicRouteMiddleware[*stdhttp.Request, *stdhttp.Response] {
	return routery.Timeout[*stdhttp.Request, routery.BasicKind, routery.BasicReason, *stdhttp.Response](timeout)
}
