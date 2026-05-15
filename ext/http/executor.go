package routeryhttp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	stdhttp "net/http"

	"github.com/skosovsky/routery"
)

const defaultMaxReplayBodyBytes int64 = 10 << 20

// ErrReplayBodyTooLarge indicates that a request body cannot be buffered safely.
var ErrReplayBodyTooLarge = errors.New("routery/ext/http: replay body exceeds limit")

// Option configures the HTTP executor.
type Option func(*options)

type options struct {
	maxReplayBodyBytes int64
	err                error
}

// WithMaxReplayBodyBytes limits in-memory buffering for replayable request bodies.
//
// A value of 0 disables the limit. Negative values make NewExecutor return a
// configuration error executor.
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

// NewExecutor adapts a standard HTTP client to a routery executor.
func NewExecutor(
	client *stdhttp.Client,
	executorOptions ...Option,
) routery.Executor[*stdhttp.Request, *stdhttp.Response] {
	if client == nil {
		//nolint:bodyclose // No HTTP response is created in this branch.
		return invalidExecutor(configError("http client is nil"))
	}

	opts := applyOptions(executorOptions)
	if opts.err != nil {
		//nolint:bodyclose // No HTTP response is created in this branch.
		return invalidExecutor(opts.err)
	}

	return routery.ExecutorFunc[*stdhttp.Request, *stdhttp.Response](
		func(ctx context.Context, request *stdhttp.Request) (*stdhttp.Response, error) {
			if request == nil {
				return nil, configError("request is nil")
			}

			attemptRequest, err := cloneForAttempt(ctx, request, opts.maxReplayBodyBytes)
			if err != nil {
				return nil, err
			}

			//nolint:gosec // Caller provides the request target; this adapter must forward it unchanged.
			response, executeErr := client.Do(attemptRequest)
			if executeErr != nil {
				if response != nil && response.Body != nil {
					_ = response.Body.Close()
				}

				return nil, executeErr
			}

			if response.StatusCode < stdhttp.StatusOK || response.StatusCode >= stdhttp.StatusMultipleChoices {
				return response, &StatusError{
					Request:  request,
					Response: response,
					Code:     response.StatusCode,
				}
			}

			return response, nil
		},
	)
}

func applyOptions(executorOptions []Option) options {
	opts := options{
		maxReplayBodyBytes: defaultMaxReplayBodyBytes,
		err:                nil,
	}
	for _, option := range executorOptions {
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

func invalidExecutor(
	err error,
) routery.Executor[*stdhttp.Request, *stdhttp.Response] {
	return routery.ExecutorFunc[*stdhttp.Request, *stdhttp.Response](
		func(context.Context, *stdhttp.Request) (*stdhttp.Response, error) {
			return nil, err
		},
	)
}
