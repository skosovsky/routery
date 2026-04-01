package routeryhttp

import (
	"context"
	"fmt"
	stdhttp "net/http"

	"github.com/skosovsky/routery"
)

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
func NewExecutor(client *stdhttp.Client) routery.Executor[*stdhttp.Request, *stdhttp.Response] {
	if client == nil {
		//nolint:bodyclose // No HTTP response is created in this branch.
		return invalidExecutor(configError("http client is nil"))
	}

	return routery.ExecutorFunc[*stdhttp.Request, *stdhttp.Response](
		func(ctx context.Context, request *stdhttp.Request) (*stdhttp.Response, error) {
			if request == nil {
				return nil, configError("request is nil")
			}

			attemptRequest, err := cloneForAttempt(ctx, request)
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

func cloneForAttempt(ctx context.Context, request *stdhttp.Request) (*stdhttp.Request, error) {
	cloned := request.Clone(ctx)

	if request.Body == nil || request.Body == stdhttp.NoBody {
		return cloned, nil
	}

	if request.GetBody == nil {
		cloned.Body = request.Body
		return cloned, nil
	}

	body, err := request.GetBody()
	if err != nil {
		return nil, fmt.Errorf("routery/ext/http: get body: %w", err)
	}
	cloned.Body = body

	return cloned, nil
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
