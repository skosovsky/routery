package routerys3

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"testing"

	smithy "github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

func TestDefaultRetryPolicy(t *testing.T) {
	t.Parallel()

	type row struct {
		name string
		err  error
		want bool
	}

	resp503 := &smithyhttp.ResponseError{
		Response: &smithyhttp.Response{
			Response: &http.Response{StatusCode: http.StatusServiceUnavailable},
		},
	}
	resp404 := &smithyhttp.ResponseError{
		Response: &smithyhttp.Response{
			Response: &http.Response{StatusCode: http.StatusNotFound},
		},
	}
	resp429 := &smithyhttp.ResponseError{
		Response: &smithyhttp.Response{
			Response: &http.Response{StatusCode: http.StatusTooManyRequests},
		},
	}

	tests := []row{
		{"nil", nil, false},
		{"canceled", context.Canceled, false},
		{"deadline", context.DeadlineExceeded, false},
		{"503", resp503, true},
		{"429", resp429, true},
		{"404", resp404, false},
		{
			"403",
			&smithyhttp.ResponseError{
				Response: &smithyhttp.Response{
					Response: &http.Response{StatusCode: http.StatusForbidden},
				},
			},
			false,
		},
		{"timeout", &net.OpError{Err: s3TimeoutError{}}, true},
		{"slow_msg", errors.New("Please reduce your request rate (SlowDown)"), true},
		{"plain", errors.New("x"), false},
		{"api_slow", &smithySlowDownError{code: "SlowDown"}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := DefaultRetryPolicy[struct{}](context.Background(), struct{}{}, tc.err)
			if got != tc.want {
				t.Fatalf("got %v want %v for %v", got, tc.want, tc.err)
			}
		})
	}
}

type s3TimeoutError struct{}

func (s3TimeoutError) Error() string   { return "timeout" }
func (s3TimeoutError) Timeout() bool   { return true }
func (s3TimeoutError) Temporary() bool { return true }

type smithySlowDownError struct {
	code string
}

func (e *smithySlowDownError) Error() string {
	return fmt.Sprintf("api: %s", e.code)
}

func (e *smithySlowDownError) ErrorCode() string { return e.code }

func (e *smithySlowDownError) ErrorMessage() string { return e.code }

func (e *smithySlowDownError) ErrorFault() smithy.ErrorFault { return smithy.FaultUnknown }

var _ smithy.APIError = (*smithySlowDownError)(nil)
