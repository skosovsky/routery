package routerysql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"testing"

	"github.com/skosovsky/routery"
)

func BenchmarkDBQueryRouteHandler(b *testing.B) {
	db, _ := openTestDB(b, testDriverConfig{})
	handler := NewDBQueryRouteHandler(
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
		outcome, err := routery.InvokeRouteHandler(context.Background(), request, handler)
		if err != nil {
			b.Fatalf("unexpected query error: %v", err)
		}
		if !outcome.HasPayload {
			b.Fatal("expected rows payload")
		}
		if rowsErr := drainRows(outcome.Payload); rowsErr != nil {
			b.Fatalf("unexpected rows error: %v", rowsErr)
		}
	}
}

func BenchmarkDBExecRouteHandler(b *testing.B) {
	db, _ := openTestDB(b, testDriverConfig{})
	handler := NewDBExecRouteHandler(
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
		outcome, err := routery.InvokeRouteHandler(context.Background(), request, handler)
		if err != nil {
			b.Fatalf("unexpected exec error: %v", err)
		}

		if !outcome.HasPayload {
			b.Fatal("expected exec result payload")
		}
		if _, rowsErr := outcome.Payload.RowsAffected(); rowsErr != nil {
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

func BenchmarkRetryIfWithDBRouteHandler(b *testing.B) {
	db, _ := openTestDB(b, testDriverConfig{execErr: driver.ErrBadConn})
	handler := routery.ApplyRoute(
		NewDBExecRouteHandler(
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
		_, _ = routery.InvokeRouteHandler(context.Background(), request, handler)
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
