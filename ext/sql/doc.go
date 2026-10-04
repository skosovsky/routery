// Package routerysql adapts database/sql [RouteHandler] values to routery contracts.
//
// Query route handlers return [database/sql.Rows] payloads and callers must always close rows,
// through result.Lifetime.Close(), to release both rows and routing callbacks.
// Calling rows.Close() alone does not notify routing lifetime callbacks. In particular,
// FirstCompleted and Timeout retain the winning query context until the lifetime closes.
//
// Transaction handlers are supported for timeout/logging/routing use-cases.
// Retrying single statements inside an existing [database/sql.Tx] is intentionally not
// part of the default retry policy; retry should wrap the full transaction
// factory in caller code.
package routerysql
