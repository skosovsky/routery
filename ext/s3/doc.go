// Package routerys3 wraps AWS SDK v2 S3 operations as [github.com/skosovsky/routery.RouteHandler] values.
//
// Use with [github.com/skosovsky/routery.Bulkhead] to cap concurrent large uploads and
// [github.com/skosovsky/routery.Fallback] to switch buckets on failures.
//
// [DefaultRetryPolicy] retries throttling and network timeouts; it does not retry 404/403 by default.
// GetObject results own their body through result.Lifetime. Closing either the body
// or the lifetime releases the resource and routing callbacks exactly once; headers
// alone do not end the lifetime. Each invocation must return its own output and body.
package routerys3
