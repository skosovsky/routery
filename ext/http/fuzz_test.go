package routeryhttp

import (
	"context"
	"errors"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func FuzzDefaultRetryPolicyNoPanics(f *testing.F) {
	f.Add("GET", stdhttp.StatusServiceUnavailable, true, true, true)
	f.Add("POST", stdhttp.StatusBadGateway, true, false, true)
	f.Add("", 0, false, false, false)

	f.Fuzz(func(t *testing.T, method string, statusCode int, withBody bool, withGetBody bool, statusErrorCase bool) {
		t.Helper()

		request := httptest.NewRequest(stdhttp.MethodGet, "http://example.com", nil)
		request.Method = method

		if withBody {
			request.Body = io.NopCloser(strings.NewReader("payload"))
			if withGetBody {
				request.GetBody = func() (io.ReadCloser, error) {
					return io.NopCloser(strings.NewReader("payload")), nil
				}
			} else {
				request.GetBody = nil
			}
		}

		if statusErrorCase {
			statusErr := &StatusError{
				Request: request,
				Response: &stdhttp.Response{
					StatusCode: statusCode,
					Body:       io.NopCloser(strings.NewReader("body")),
				},
				Code: statusCode,
			}
			_ = DefaultRetryPolicy(statusErr)
			return
		}

		_ = DefaultRetryPolicy(&requestError{
			request: request,
			err:     io.ErrUnexpectedEOF,
		})
		_ = DefaultRetryPolicy(&requestError{
			request: request,
			err:     errors.New("transport failure"),
		})
	})
}

func FuzzCloneForAttemptNoPanics(f *testing.F) {
	f.Add("GET", false, false, false)
	f.Add("POST", true, true, false)
	f.Add("PATCH", true, true, true)

	f.Fuzz(func(t *testing.T, method string, withBody bool, withGetBody bool, getBodyError bool) {
		t.Helper()

		request := httptest.NewRequest(stdhttp.MethodGet, "http://example.com", nil)
		request.Method = method

		if withBody {
			request.Body = io.NopCloser(strings.NewReader("payload"))
			if withGetBody {
				request.GetBody = func() (io.ReadCloser, error) {
					if getBodyError {
						return nil, errors.New("get body failed")
					}
					return io.NopCloser(strings.NewReader("payload")), nil
				}
			} else {
				request.GetBody = nil
			}
		}

		_, _ = cloneForAttempt(context.Background(), request)
	})
}
