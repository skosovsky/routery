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
func DefaultRetryPolicy(err error) bool {
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
