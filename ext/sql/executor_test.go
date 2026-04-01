package routerysql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"

	"github.com/skosovsky/routery"
)

type statementRequest struct {
	Args  []any
	Query string
}

func statementExtractor(_ context.Context, req statementRequest) (string, []any, error) {
	return req.Query, req.Args, nil
}

func TestNewDBQueryExecutorInvalidConfig(t *testing.T) {
	t.Parallel()

	executor := NewDBQueryExecutor[statementRequest](nil, statementExtractor)
	rows, err := executor.Execute(context.Background(), statementRequest{})
	assertRowsCheckedAndClosed(t, rows)
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestNewDBExecExecutorInvalidConfig(t *testing.T) {
	t.Parallel()

	db, _ := openTestDB(t, testDriverConfig{})
	executor := NewDBExecExecutor[statementRequest](db, nil)

	_, err := executor.Execute(context.Background(), statementRequest{})
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestNewTxQueryExecutorInvalidConfig(t *testing.T) {
	t.Parallel()

	executor := NewTxQueryExecutor[statementRequest](nil, statementExtractor)
	rows, err := executor.Execute(context.Background(), statementRequest{})
	assertRowsCheckedAndClosed(t, rows)
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestNewTxExecExecutorInvalidConfig(t *testing.T) {
	t.Parallel()

	db, _ := openTestDB(t, testDriverConfig{})
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}
	t.Cleanup(func() {
		_ = tx.Rollback()
	})

	executor := NewTxExecExecutor[statementRequest](tx, nil)
	_, executeErr := executor.Execute(context.Background(), statementRequest{})
	if !errors.Is(executeErr, routery.ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", executeErr)
	}
}

func TestNewDBQueryExecutorExecutesQuery(t *testing.T) {
	t.Parallel()

	db, state := openTestDB(t, testDriverConfig{})
	executor := NewDBQueryExecutor[statementRequest](db, statementExtractor)

	rows, err := executor.Execute(context.Background(), statementRequest{
		Query: "SELECT value FROM widgets WHERE id = ?",
		Args:  []any{1},
	})
	if err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}
	t.Cleanup(func() {
		_ = rows.Close()
	})
	if !rows.Next() {
		t.Fatal("expected at least one row")
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		t.Fatalf("unexpected rows error: %v", rowsErr)
	}

	if state.queryCallCount() != 1 {
		t.Fatalf("unexpected query call count: got %d, want 1", state.queryCallCount())
	}
	statement, args := state.lastQuery()
	if statement != "SELECT value FROM widgets WHERE id = ?" {
		t.Fatalf("unexpected query statement: %q", statement)
	}
	if got := fmt.Sprint(args); got != "[1]" {
		t.Fatalf("unexpected query args: %#v", args)
	}
}

func TestNewDBExecExecutorExecutesStatement(t *testing.T) {
	t.Parallel()

	db, state := openTestDB(t, testDriverConfig{})
	executor := NewDBExecExecutor[statementRequest](db, statementExtractor)

	result, err := executor.Execute(context.Background(), statementRequest{
		Query: "UPDATE widgets SET active = ? WHERE id = ?",
		Args:  []any{true, 2},
	})
	if err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}

	rowsAffected, rowsAffectedErr := result.RowsAffected()
	if rowsAffectedErr != nil {
		t.Fatalf("unexpected rows affected error: %v", rowsAffectedErr)
	}
	if rowsAffected != 1 {
		t.Fatalf("unexpected rows affected: got %d, want 1", rowsAffected)
	}

	if state.execCallCount() != 1 {
		t.Fatalf("unexpected exec call count: got %d, want 1", state.execCallCount())
	}
	statement, args := state.lastExec()
	if statement != "UPDATE widgets SET active = ? WHERE id = ?" {
		t.Fatalf("unexpected exec statement: %q", statement)
	}
	if got := fmt.Sprint(args); got != "[true 2]" {
		t.Fatalf("unexpected exec args: %#v", args)
	}
}

func TestNewTxQueryExecutorExecutesQuery(t *testing.T) {
	t.Parallel()

	db, state := openTestDB(t, testDriverConfig{})
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}
	t.Cleanup(func() {
		_ = tx.Rollback()
	})

	executor := NewTxQueryExecutor[statementRequest](tx, statementExtractor)
	rows, executeErr := executor.Execute(context.Background(), statementRequest{
		Query: "SELECT value FROM widgets",
	})
	if executeErr != nil {
		t.Fatalf("unexpected execute error: %v", executeErr)
	}
	t.Cleanup(func() {
		_ = rows.Close()
	})
	if !rows.Next() {
		t.Fatal("expected at least one row")
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		t.Fatalf("unexpected rows error: %v", rowsErr)
	}

	if state.beginCallCount() != 1 {
		t.Fatalf("unexpected begin count: got %d, want 1", state.beginCallCount())
	}
	if state.queryCallCount() != 1 {
		t.Fatalf("unexpected query count: got %d, want 1", state.queryCallCount())
	}
}

func TestNewTxExecExecutorExecutesStatement(t *testing.T) {
	t.Parallel()

	db, state := openTestDB(t, testDriverConfig{})
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}
	t.Cleanup(func() {
		_ = tx.Rollback()
	})

	executor := NewTxExecExecutor[statementRequest](tx, statementExtractor)
	result, executeErr := executor.Execute(context.Background(), statementRequest{
		Query: "DELETE FROM widgets WHERE id = ?",
		Args:  []any{3},
	})
	if executeErr != nil {
		t.Fatalf("unexpected execute error: %v", executeErr)
	}

	rowsAffected, rowsAffectedErr := result.RowsAffected()
	if rowsAffectedErr != nil {
		t.Fatalf("unexpected rows affected error: %v", rowsAffectedErr)
	}
	if rowsAffected != 1 {
		t.Fatalf("unexpected rows affected: got %d, want 1", rowsAffected)
	}
	if state.beginCallCount() != 1 {
		t.Fatalf("unexpected begin count: got %d, want 1", state.beginCallCount())
	}
}

func TestExtractorErrorIsReturnedWithoutWrapping(t *testing.T) {
	t.Parallel()

	db, _ := openTestDB(t, testDriverConfig{})
	extractorErr := errors.New("extractor failed")
	executor := NewDBExecExecutor[statementRequest](
		db,
		func(context.Context, statementRequest) (string, []any, error) {
			return "", nil, extractorErr
		},
	)

	_, err := executor.Execute(context.Background(), statementRequest{})
	if !errors.Is(err, extractorErr) {
		t.Fatalf("expected extractor error, got %v", err)
	}

	var executionErr *ExecutionError
	if errors.As(err, &executionErr) {
		t.Fatalf("did not expect ExecutionError wrapper, got %v", err)
	}
}

func TestExecutionErrorWrapsQueryFailure(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("query failed")
	db, _ := openTestDB(t, testDriverConfig{queryErr: queryErr})
	executor := NewDBQueryExecutor[statementRequest](db, statementExtractor)

	rows, err := executor.Execute(context.Background(), statementRequest{
		Query: "SELECT value FROM widgets",
	})
	assertRowsCheckedAndClosed(t, rows)
	if err == nil {
		t.Fatal("expected execute error")
	}
	if !errors.Is(err, queryErr) {
		t.Fatalf("expected wrapped query error, got %v", err)
	}

	var executionErr *ExecutionError
	if !errors.As(err, &executionErr) {
		t.Fatalf("expected ExecutionError, got %T", err)
	}
	if executionErr.Operation != OperationQuery {
		t.Fatalf("unexpected operation: got %q, want %q", executionErr.Operation, OperationQuery)
	}
	if executionErr.InTransaction {
		t.Fatal("expected non-transactional execution error")
	}
}

func TestExecutionErrorWrapsTxFailure(t *testing.T) {
	t.Parallel()

	db, _ := openTestDB(t, testDriverConfig{execErr: driver.ErrBadConn})
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}
	t.Cleanup(func() {
		_ = tx.Rollback()
	})

	executor := NewTxExecExecutor[statementRequest](tx, statementExtractor)
	_, executeErr := executor.Execute(context.Background(), statementRequest{
		Query: "UPDATE widgets SET active = ? WHERE id = ?",
		Args:  []any{false, 1},
	})
	if executeErr == nil {
		t.Fatal("expected execute error")
	}

	var executionErr *ExecutionError
	if !errors.As(executeErr, &executionErr) {
		t.Fatalf("expected ExecutionError, got %T", executeErr)
	}
	if !executionErr.InTransaction {
		t.Fatal("expected transactional execution error")
	}
	if executionErr.Operation != OperationExec {
		t.Fatalf("unexpected operation: got %q, want %q", executionErr.Operation, OperationExec)
	}
}

func TestRowsMustBeClosedByCaller(t *testing.T) {
	t.Parallel()

	db, state := openTestDB(t, testDriverConfig{})
	executor := NewDBQueryExecutor[statementRequest](db, statementExtractor)

	rows, err := executor.Execute(context.Background(), statementRequest{
		Query: "SELECT value FROM widgets",
	})
	if err != nil {
		t.Fatalf("unexpected execute error: %v", err)
	}
	if state.rowsClosedCount() != 0 {
		t.Fatalf("unexpected rows closed count before close: got %d, want 0", state.rowsClosedCount())
	}
	closeRowsAndCheck(t, rows)
	if state.rowsClosedCount() != 1 {
		t.Fatalf("unexpected rows closed count after close: got %d, want 1", state.rowsClosedCount())
	}
}

func TestContextCancellationIsNotRetriedByDefaultPolicy(t *testing.T) {
	t.Parallel()

	db, _ := openTestDB(t, testDriverConfig{block: true})
	executor := NewDBExecExecutor[statementRequest](db, statementExtractor)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := executor.Execute(ctx, statementRequest{
		Query: "UPDATE widgets SET active = ? WHERE id = ?",
		Args:  []any{false, 1},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	if DefaultRetryPolicy[statementRequest](context.Background(), statementRequest{}, err) {
		t.Fatalf("expected no retry for canceled context, got retry=true")
	}
}

func TestTxLifecycleCounters(t *testing.T) {
	t.Parallel()

	db, state := openTestDB(t, testDriverConfig{})
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}
	if commitErr := tx.Commit(); commitErr != nil {
		t.Fatalf("failed to commit transaction: %v", commitErr)
	}

	tx2, err2 := db.BeginTx(context.Background(), nil)
	if err2 != nil {
		t.Fatalf("failed to begin second transaction: %v", err2)
	}
	if rollbackErr := tx2.Rollback(); rollbackErr != nil {
		t.Fatalf("failed to rollback transaction: %v", rollbackErr)
	}

	if state.beginCallCount() != 2 {
		t.Fatalf("unexpected begin count: got %d, want 2", state.beginCallCount())
	}
	if state.commitCallCount() != 1 {
		t.Fatalf("unexpected commit count: got %d, want 1", state.commitCallCount())
	}
	if state.rollbackCallCount() != 1 {
		t.Fatalf("unexpected rollback count: got %d, want 1", state.rollbackCallCount())
	}
}

func TestExecutionErrorString(t *testing.T) {
	t.Parallel()

	t.Run("nil receiver", func(t *testing.T) {
		t.Parallel()

		var executionErr *ExecutionError
		if got := executionErr.Error(); got != "routery/ext/sql: execution error" {
			t.Fatalf("unexpected error string: %q", got)
		}
	})

	t.Run("without inner error", func(t *testing.T) {
		t.Parallel()

		executionErr := &ExecutionError{
			Operation:     OperationExec,
			InTransaction: true,
		}
		if got := executionErr.Error(); got != "routery/ext/sql: exec failed in transaction" {
			t.Fatalf("unexpected error string: %q", got)
		}
	})

	t.Run("with inner error", func(t *testing.T) {
		t.Parallel()

		innerErr := errors.New("boom")
		executionErr := &ExecutionError{
			Operation: OperationQuery,
			Err:       innerErr,
		}

		if !errors.Is(executionErr, innerErr) {
			t.Fatalf("expected errors.Is to match inner error, got %v", executionErr)
		}
		if got := executionErr.Error(); got != "routery/ext/sql: query failed: boom" {
			t.Fatalf("unexpected error string: %q", got)
		}
	})
}

func TestDefaultRetryPolicy(t *testing.T) {
	t.Parallel()

	cases := []struct {
		err       error
		name      string
		wantRetry bool
	}{
		{name: "nil", err: nil, wantRetry: false},
		{name: "canceled", err: context.Canceled, wantRetry: false},
		{name: "deadline", err: context.DeadlineExceeded, wantRetry: false},
		{name: "unknown", err: errors.New("unknown"), wantRetry: false},
		{
			name: "bad conn db",
			err: &ExecutionError{
				Operation: OperationQuery,
				Err:       driver.ErrBadConn,
			},
			wantRetry: true,
		},
		{
			name: "bad conn tx",
			err: &ExecutionError{
				Operation:     OperationExec,
				InTransaction: true,
				Err:           driver.ErrBadConn,
			},
			wantRetry: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := DefaultRetryPolicy[statementRequest](
				context.Background(),
				statementRequest{},
				tc.err,
			)
			if got != tc.wantRetry {
				t.Fatalf("unexpected retry decision: got %v, want %v", got, tc.wantRetry)
			}
		})
	}
}

func assertRowsCheckedAndClosed(t *testing.T, rows *sql.Rows) {
	t.Helper()
	if rows == nil {
		return
	}

	closeRowsAndCheck(t, rows)
}

func closeRowsAndCheck(t *testing.T, rows *sql.Rows) {
	t.Helper()

	var closeErr error
	func() {
		defer func() {
			closeErr = rows.Close()
		}()

		for rows.Next() {
		}

		if rowsErr := rows.Err(); rowsErr != nil {
			t.Fatalf("unexpected rows error: %v", rowsErr)
		}
	}()

	if closeErr != nil {
		t.Fatalf("failed to close rows: %v", closeErr)
	}
}
