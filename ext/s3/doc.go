// Package routerys3 wraps AWS SDK v2 S3 operations as [github.com/skosovsky/routery.RouteHandler] values.
//
// Use with [github.com/skosovsky/routery.Bulkhead] to cap concurrent large uploads and
// [github.com/skosovsky/routery.Fallback] to switch buckets on failures.
//
// [IsTransientError] classifies throttling and network timeouts; it never authorizes replay.
// [RetryPolicy] requires host evidence, and uploads require independent [PutRequest] bodies.
// GetObject results own their body through result.Lifetime. Closing either the body
// or the lifetime releases the resource and routing callbacks exactly once; headers
// alone do not end the lifetime. Each invocation must return its own output and body.
//
// Client/SDK retries are unobservable here. Configure one retry owner; see
// docs/adapter-replay-contracts.md in the routery repository for client controls.
// One handler invocation does not promise one physical network attempt.
package routerys3
