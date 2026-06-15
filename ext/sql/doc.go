// Package routerysql adapts database/sql [RouteHandler] values to routery contracts.
//
// Query route handlers return [database/sql.Rows] payloads and callers must always close rows,
// usually with defer rows.Close(), to avoid exhausting the connection pool.
//
// Transaction handlers are supported for timeout/logging/routing use-cases.
// Retrying single statements inside an existing [database/sql.Tx] is intentionally not
// part of the default retry policy; retry should wrap the full transaction
// factory in caller code.
package routerysql
