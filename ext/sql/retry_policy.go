package routerysql

import (
	"context"
	"database/sql/driver"
	"errors"
)

// DefaultRetryPolicy is a conservative retry policy for SQL execution.
//
// It retries only non-transactional execution failures that unwrap to
// [driver.ErrBadConn]. Transaction failures are not retried by default.
//
// Req is unused but generic so the function can be passed directly to
// [github.com/skosovsky/routery.RetryIf] for any request type.
func DefaultRetryPolicy[Req any](ctx context.Context, req Req, err error) bool {
	_ = ctx
	_ = req

	if err == nil {
		return false
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	var executionErr *ExecutionError
	if !errors.As(err, &executionErr) || executionErr == nil {
		return false
	}

	if executionErr.InTransaction {
		return false
	}

	return errors.Is(executionErr, driver.ErrBadConn)
}
