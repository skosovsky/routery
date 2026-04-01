package routerygrpc

import (
	"context"
	"errors"
	"io"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestDefaultRetryPolicy(t *testing.T) {
	t.Parallel()

	type row struct {
		name string
		req  any
		err  error
		want bool
	}

	tests := []row{
		{"nil", nil, nil, false},
		{"canceled", nil, context.Canceled, false},
		{"deadline_ctx", nil, context.DeadlineExceeded, false},
		{"unavailable", nil, status.Error(codes.Unavailable, "x"), true},
		{"data_loss", nil, status.Error(codes.DataLoss, "x"), true},
		{"invalid_arg", nil, status.Error(codes.InvalidArgument, "x"), false},
		{"unauthenticated", nil, status.Error(codes.Unauthenticated, "x"), false},
		{"permission_denied", nil, status.Error(codes.PermissionDenied, "x"), false},
		{"already_exists", nil, status.Error(codes.AlreadyExists, "x"), false},
		{"not_found", nil, status.Error(codes.NotFound, "x"), false},
		{"unimplemented", nil, status.Error(codes.Unimplemented, "x"), false},
		{"internal", nil, status.Error(codes.Internal, "x"), false},
		{"unknown", nil, status.Error(codes.Unknown, "x"), false},
		{"deadline_grpc_not_idempotent", "plain", status.Error(codes.DeadlineExceeded, "x"), false},
		{
			"deadline_grpc_idempotent",
			WithIdempotent[string]{Idempotent: true},
			status.Error(codes.DeadlineExceeded, "x"),
			true,
		},
		{"eof", nil, io.EOF, true},
		{"plain_error", nil, errors.New("not status"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := DefaultRetryPolicy[any](context.Background(), tc.req, tc.err)
			if got != tc.want {
				t.Fatalf("got %v want %v err=%v", got, tc.want, tc.err)
			}
		})
	}
}

func FuzzDefaultRetryPolicyNoPanics(f *testing.F) {
	f.Add(int32(0), "msg")
	f.Fuzz(func(t *testing.T, code int32, msg string) {
		t.Helper()
		err := status.Error(codes.Code(code%17), msg)
		_ = DefaultRetryPolicy[any](context.Background(), nil, err)
	})
}
