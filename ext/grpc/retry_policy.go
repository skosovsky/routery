package routerygrpc

import (
	"context"
	"errors"
	"io"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// DefaultRetryPolicy classifies gRPC errors for [github.com/skosovsky/routery.RetryIf].
//
// Client-side [context.Canceled] and [context.DeadlineExceeded] are not retried.
// When [status.FromError] fails, only [io.EOF] is treated as retryable (connection drop).
func DefaultRetryPolicy[Req any](ctx context.Context, req Req, err error) bool {
	_ = ctx

	if err == nil {
		return false
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	st, ok := status.FromError(err)
	if !ok {
		return errors.Is(err, io.EOF)
	}

	switch st.Code() {
	case codes.InvalidArgument,
		codes.Unauthenticated,
		codes.PermissionDenied,
		codes.AlreadyExists,
		codes.NotFound,
		codes.Unimplemented:
		return false
	case codes.Unavailable, codes.DataLoss:
		return true
	case codes.DeadlineExceeded:
		return idempotentFromAny(req)
	default:
		return false
	}
}
