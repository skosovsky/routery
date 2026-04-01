package routerysql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/skosovsky/routery"
)

const (
	errorPrefix = "routery/ext/sql"
)

// StatementExtractor converts an arbitrary request into SQL text and arguments.
type StatementExtractor[Req any] func(ctx context.Context, req Req) (query string, args []any, err error)

// Operation identifies which SQL operation failed.
type Operation string

const (
	// OperationQuery identifies QueryContext failures.
	OperationQuery Operation = "query"
	// OperationExec identifies ExecContext failures.
	OperationExec Operation = "exec"
)

// ExecutionError describes a SQL operation failure.
type ExecutionError struct {
	Operation     Operation
	InTransaction bool
	Err           error
}

// Error implements error.
func (err *ExecutionError) Error() string {
	if err == nil {
		return errorPrefix + ": execution error"
	}

	operation := string(err.Operation)
	if operation == "" {
		operation = "operation"
	}

	if err.Err == nil {
		if err.InTransaction {
			return fmt.Sprintf("%s: %s failed in transaction", errorPrefix, operation)
		}

		return fmt.Sprintf("%s: %s failed", errorPrefix, operation)
	}

	if err.InTransaction {
		return fmt.Sprintf("%s: %s failed in transaction: %v", errorPrefix, operation, err.Err)
	}

	return fmt.Sprintf("%s: %s failed: %v", errorPrefix, operation, err.Err)
}

// Unwrap returns the original execution error.
func (err *ExecutionError) Unwrap() error {
	if err == nil {
		return nil
	}

	return err.Err
}

// NewDBQueryExecutor adapts a [sql.DB] query operation to a routery executor.
func NewDBQueryExecutor[Req any](
	db *sql.DB,
	extractor StatementExtractor[Req],
) routery.Executor[Req, *sql.Rows] {
	if db == nil {
		return invalidExecutor[Req, *sql.Rows](configError("db is nil"))
	}

	return newQueryExecutor[Req](db, false, extractor)
}

// NewDBExecExecutor adapts a [sql.DB] exec operation to a routery executor.
func NewDBExecExecutor[Req any](
	db *sql.DB,
	extractor StatementExtractor[Req],
) routery.Executor[Req, sql.Result] {
	if db == nil {
		return invalidExecutor[Req, sql.Result](configError("db is nil"))
	}

	return newExecExecutor[Req](db, false, extractor)
}

// NewTxQueryExecutor adapts a [sql.Tx] query operation to a routery executor.
func NewTxQueryExecutor[Req any](
	tx *sql.Tx,
	extractor StatementExtractor[Req],
) routery.Executor[Req, *sql.Rows] {
	if tx == nil {
		return invalidExecutor[Req, *sql.Rows](configError("tx is nil"))
	}

	return newQueryExecutor[Req](tx, true, extractor)
}

// NewTxExecExecutor adapts a [sql.Tx] exec operation to a routery executor.
func NewTxExecExecutor[Req any](
	tx *sql.Tx,
	extractor StatementExtractor[Req],
) routery.Executor[Req, sql.Result] {
	if tx == nil {
		return invalidExecutor[Req, sql.Result](configError("tx is nil"))
	}

	return newExecExecutor[Req](tx, true, extractor)
}

type queryRunner interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type execRunner interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func newQueryExecutor[Req any](
	runner queryRunner,
	inTransaction bool,
	extractor StatementExtractor[Req],
) routery.Executor[Req, *sql.Rows] {
	if extractor == nil {
		return invalidExecutor[Req, *sql.Rows](configError("statement extractor is nil"))
	}

	return routery.ExecutorFunc[Req, *sql.Rows](
		func(ctx context.Context, req Req) (*sql.Rows, error) {
			query, args, err := extractor(ctx, req)
			if err != nil {
				return nil, err
			}

			rows, queryErr := runner.QueryContext(ctx, query, args...)
			if queryErr != nil {
				return nil, &ExecutionError{
					Operation:     OperationQuery,
					InTransaction: inTransaction,
					Err:           queryErr,
				}
			}

			return rows, nil
		},
	)
}

func newExecExecutor[Req any](
	runner execRunner,
	inTransaction bool,
	extractor StatementExtractor[Req],
) routery.Executor[Req, sql.Result] {
	if extractor == nil {
		return invalidExecutor[Req, sql.Result](configError("statement extractor is nil"))
	}

	return routery.ExecutorFunc[Req, sql.Result](
		func(ctx context.Context, req Req) (sql.Result, error) {
			query, args, err := extractor(ctx, req)
			if err != nil {
				return nil, err
			}

			result, execErr := runner.ExecContext(ctx, query, args...)
			if execErr != nil {
				return nil, &ExecutionError{
					Operation:     OperationExec,
					InTransaction: inTransaction,
					Err:           execErr,
				}
			}

			return result, nil
		},
	)
}

func configError(detail string) error {
	return fmt.Errorf("%w: %s", routery.ErrInvalidConfig, detail)
}

func invalidExecutor[Req any, Res any](err error) routery.Executor[Req, Res] {
	return routery.ExecutorFunc[Req, Res](func(context.Context, Req) (Res, error) {
		var zero Res
		return zero, err
	})
}
