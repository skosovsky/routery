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
	OperationQuery Operation = "query"
	OperationExec  Operation = "exec"
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

// NewDBQueryRouteHandler adapts a [sql.DB] query operation to a routery route handler.
func NewDBQueryRouteHandler[Req any](
	db *sql.DB,
	extractor StatementExtractor[Req],
) routery.BasicRouteHandler[Req, *sql.Rows] {
	if db == nil {
		return invalidRouteHandler[Req, *sql.Rows](configError("db is nil"))
	}

	return newQueryRouteHandler[Req](db, false, extractor)
}

// NewDBExecRouteHandler adapts a [sql.DB] exec operation to a routery route handler.
func NewDBExecRouteHandler[Req any](
	db *sql.DB,
	extractor StatementExtractor[Req],
) routery.BasicRouteHandler[Req, sql.Result] {
	if db == nil {
		return invalidRouteHandler[Req, sql.Result](configError("db is nil"))
	}

	return newExecRouteHandler[Req](db, false, extractor)
}

// NewTxQueryRouteHandler adapts a [sql.Tx] query operation to a routery route handler.
func NewTxQueryRouteHandler[Req any](
	tx *sql.Tx,
	extractor StatementExtractor[Req],
) routery.BasicRouteHandler[Req, *sql.Rows] {
	if tx == nil {
		return invalidRouteHandler[Req, *sql.Rows](configError("tx is nil"))
	}

	return newQueryRouteHandler[Req](tx, true, extractor)
}

// NewTxExecRouteHandler adapts a [sql.Tx] exec operation to a routery route handler.
func NewTxExecRouteHandler[Req any](
	tx *sql.Tx,
	extractor StatementExtractor[Req],
) routery.BasicRouteHandler[Req, sql.Result] {
	if tx == nil {
		return invalidRouteHandler[Req, sql.Result](configError("tx is nil"))
	}

	return newExecRouteHandler[Req](tx, true, extractor)
}

type queryRunner interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type execRunner interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func newQueryRouteHandler[Req any](
	runner queryRunner,
	inTransaction bool,
	extractor StatementExtractor[Req],
) routery.BasicRouteHandler[Req, *sql.Rows] {
	if extractor == nil {
		return invalidRouteHandler[Req, *sql.Rows](configError("statement extractor is nil"))
	}

	return func(call routery.RouteCall[Req]) (routery.BasicRouteResult[*sql.Rows], error) {
		query, args, err := extractor(call.Context, call.Request)
		if err != nil {
			return routery.AbortResult[routery.BasicKind, routery.BasicReason, *sql.Rows](), err
		}

		rows, queryErr := runner.QueryContext(call.Context, query, args...)
		if queryErr != nil {
			return routery.AbortResult[routery.BasicKind, routery.BasicReason, *sql.Rows](), &ExecutionError{
				Operation:     OperationQuery,
				InTransaction: inTransaction,
				Err:           queryErr,
			}
		}

		return routery.BasicHandled(rows), nil
	}
}

func newExecRouteHandler[Req any](
	runner execRunner,
	inTransaction bool,
	extractor StatementExtractor[Req],
) routery.BasicRouteHandler[Req, sql.Result] {
	if extractor == nil {
		return invalidRouteHandler[Req, sql.Result](configError("statement extractor is nil"))
	}

	return func(call routery.RouteCall[Req]) (routery.BasicRouteResult[sql.Result], error) {
		query, args, err := extractor(call.Context, call.Request)
		if err != nil {
			return routery.AbortResult[routery.BasicKind, routery.BasicReason, sql.Result](), err
		}

		result, execErr := runner.ExecContext(call.Context, query, args...)
		if execErr != nil {
			return routery.AbortResult[routery.BasicKind, routery.BasicReason, sql.Result](), &ExecutionError{
				Operation:     OperationExec,
				InTransaction: inTransaction,
				Err:           execErr,
			}
		}

		return routery.BasicHandled(result), nil
	}
}

func configError(detail string) error {
	return fmt.Errorf("%w: %s", routery.ErrInvalidConfig, detail)
}

func invalidRouteHandler[Req any, Res any](err error) routery.BasicRouteHandler[Req, Res] {
	return func(routery.RouteCall[Req]) (routery.BasicRouteResult[Res], error) {
		return routery.AbortResult[routery.BasicKind, routery.BasicReason, Res](), err
	}
}
