// Package observability provides optional callback-based logging and metrics.
// Callbacks are synchronous, potentially concurrent, bounded and non-panicking by contract.
// Event contains raw Request/Err and caller metadata; PayloadMeta does not redact it.
// Export explicit bounded projections. Observers do not own resources or settlement.
package observability
