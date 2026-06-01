package routerysql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"testing"

	"github.com/skosovsky/routery"
)

func BenchmarkDBQueryExecutor(b *testing.B) {
	db, _ := openTestDB(b, testDriverConfig{})
	executor := NewDBQueryHandler(
		db,
		func(_ context.Context, req statementRequest) (string, []any, error) {
			return req.Query, req.Args, nil
		},
	)

	request := statementRequest{
		Query: "SELECT value FROM widgets WHERE id = ?",
		Args:  []any{1},
	}

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		rowsResult, err := executor.Handle(context.Background(), request)
		if err != nil {
			b.Fatalf("unexpected query error: %v", err)
		}
		if rowsErr := drainRows(rowsResult.Payload); rowsErr != nil {
			b.Fatalf("unexpected rows error: %v", rowsErr)
		}
	}
}

func BenchmarkDBExecExecutor(b *testing.B) {
	db, _ := openTestDB(b, testDriverConfig{})
	executor := NewDBExecHandler(
		db,
		func(_ context.Context, req statementRequest) (string, []any, error) {
			return req.Query, req.Args, nil
		},
	)

	request := statementRequest{
		Query: "UPDATE widgets SET active = ? WHERE id = ?",
		Args:  []any{true, 1},
	}

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		execResult, err := executor.Handle(context.Background(), request)
		if err != nil {
			b.Fatalf("unexpected exec error: %v", err)
		}

		if _, rowsErr := execResult.Payload.RowsAffected(); rowsErr != nil {
			b.Fatalf("unexpected rows affected error: %v", rowsErr)
		}
	}
}

func BenchmarkDefaultRetryPolicyBadConn(b *testing.B) {
	err := &ExecutionError{
		Operation: OperationExec,
		Err:       driver.ErrBadConn,
	}

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_ = DefaultRetryPolicy[statementRequest](context.Background(), statementRequest{}, err)
	}
}

func BenchmarkRetryIfWithDBExecutor(b *testing.B) {
	db, _ := openTestDB(b, testDriverConfig{execErr: driver.ErrBadConn})
	executor := routery.Apply(
		NewDBExecHandler(
			db,
			func(_ context.Context, req statementRequest) (string, []any, error) {
				return req.Query, req.Args, nil
			},
		),
		routery.RetryIf[statementRequest, sql.Result](2, 0, DefaultRetryPolicy[statementRequest]),
	)

	request := statementRequest{
		Query: "UPDATE widgets SET active = ? WHERE id = ?",
		Args:  []any{false, 1},
	}

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_, _ = executor.Handle(context.Background(), request)
	}
}

func drainRows(rows *sql.Rows) error {
	defer func() {
		_ = rows.Close()
	}()

	for rows.Next() {
	}

	return rows.Err()
}
