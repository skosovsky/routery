package routerygrpc

import (
	"context"
	"errors"
	"io"

	"github.com/skosovsky/routery"
	"github.com/skosovsky/routery/policy/attempt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// IsTransientError classifies provider errors without authorizing repetition.
// Replay safety must be provided separately through RetryPolicy or execution.Sequence.
func IsTransientError(err error) bool {
	if err == nil || errors.Is(err, routery.ErrInvalidConfig) {
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
		codes.Unimplemented,
		codes.OK, codes.Canceled, codes.Unknown, codes.ResourceExhausted, codes.FailedPrecondition,
		codes.Aborted, codes.OutOfRange, codes.Internal, codes.DataLoss:
		return false
	case codes.Unavailable, codes.DeadlineExceeded:
		return true
	default:
		return false
	}
}

// RetryPolicy requires explicit host evidence; nil evidence denies repetition.
func RetryPolicy[Req any](evidence attempt.Evidence[Req]) routery.RetryPredicate[Req] {
	predicate := attempt.RetryPredicate(IsTransientError, evidence)
	return func(ctx context.Context, req Req, err error) bool { return predicate(ctx, req, err) }
}
