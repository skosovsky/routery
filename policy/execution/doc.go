// Package execution composes explicit physical attempts with generic route handlers.
// It does not select providers, infer remote outcomes, store usage, or retry implicitly.
// Callers own freshness, atomic admission, settlement evidence and cleanup deadlines.
package execution
