package routerysql

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
)

func FuzzDefaultRetryPolicyNoPanics(f *testing.F) {
	f.Add(true, true, true, "query")
	f.Add(false, false, false, "exec")
	f.Add(false, true, false, "")

	f.Fuzz(func(t *testing.T, useExecutionError bool, inTransaction bool, useBadConn bool, operation string) {
		t.Helper()

		if !useExecutionError {
			_ = DefaultRetryPolicy(errors.New("unknown"))
			_ = DefaultRetryPolicy(context.Canceled)
			_ = DefaultRetryPolicy(context.DeadlineExceeded)
			_ = DefaultRetryPolicy(nil)
			return
		}

		innerErr := errors.New("unknown")
		if useBadConn {
			innerErr = driver.ErrBadConn
		}

		_ = DefaultRetryPolicy(&ExecutionError{
			Operation:     Operation(operation),
			InTransaction: inTransaction,
			Err:           innerErr,
		})
	})
}

func FuzzExecutionErrorNoPanics(f *testing.F) {
	f.Add("query", true)
	f.Add("", false)

	f.Fuzz(func(t *testing.T, operation string, inTransaction bool) {
		t.Helper()

		var nilErr *ExecutionError
		_ = nilErr.Error()
		_ = nilErr.Unwrap()

		executionErr := &ExecutionError{
			Operation:     Operation(operation),
			InTransaction: inTransaction,
			Err:           errors.New("boom"),
		}

		_ = executionErr.Error()
		_ = executionErr.Unwrap()
	})
}
