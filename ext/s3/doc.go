// Package routerys3 wraps AWS SDK v2 S3 operations as [github.com/skosovsky/routery.Executor] values.
//
// Use with [github.com/skosovsky/routery.Bulkhead] to cap concurrent large uploads and
// [github.com/skosovsky/routery.Fallback] to switch buckets on failures.
//
// [DefaultRetryPolicy] retries throttling and network timeouts; it does not retry 404/403 by default.
package routerys3
