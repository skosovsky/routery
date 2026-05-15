package routeryhttp

import (
	"context"
	"io"
	stdhttp "net/http"
	"sync"
	"time"

	"github.com/skosovsky/routery"
)

// Timeout limits HTTP execution time without cancelling response body reads.
func Timeout(timeout time.Duration) routery.Middleware[*stdhttp.Request, *stdhttp.Response] {
	if timeout <= 0 {
		return func(
			next routery.Executor[*stdhttp.Request, *stdhttp.Response],
		) routery.Executor[*stdhttp.Request, *stdhttp.Response] {
			if next == nil {
				//nolint:bodyclose // No HTTP response is created in this branch.
				return invalidExecutor(configError("timeout middleware requires non-nil next executor"))
			}

			return next
		}
	}

	return func(
		next routery.Executor[*stdhttp.Request, *stdhttp.Response],
	) routery.Executor[*stdhttp.Request, *stdhttp.Response] {
		if next == nil {
			//nolint:bodyclose // No HTTP response is created in this branch.
			return invalidExecutor(configError("timeout middleware requires non-nil next executor"))
		}

		return routery.ExecutorFunc[*stdhttp.Request, *stdhttp.Response](
			func(ctx context.Context, req *stdhttp.Request) (*stdhttp.Response, error) {
				timedCtx, cancel := context.WithTimeout(ctx, timeout)
				defer func() {
					if recovered := recover(); recovered != nil {
						cancel()
						panic(recovered)
					}
				}()

				resp, err := next.Execute(timedCtx, req)
				if err != nil || resp == nil || resp.Body == nil || resp.Body == stdhttp.NoBody {
					cancel()
					return resp, err
				}

				resp.Body = &cancelTimerBody{
					ReadCloser: resp.Body,
					cancelOnce: sync.Once{},
					cancel:     cancel,
				}

				return resp, nil
			},
		)
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
