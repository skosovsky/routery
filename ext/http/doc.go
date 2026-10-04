// Package routeryhttp adapts net/http clients to routery [RouteHandler] values.
//
// The adapter returns *[http.Response] payloads for successful (2xx) calls. For non-2xx responses it returns *StatusError; inspect the
// response body via StatusError.Response while using routery.RetryIf with DefaultRetryPolicy.
//
// PrepareRequest consumes unprepared bodies before retries or fan-out. It closes
// the source once and returns a new immutable template with independent GetBody
// readers. NewRouteHandler rejects unprepared bodies and never mutates requests.
// Prepared headers and caller-supplied GetBody factories must be concurrency-safe.
//
// Replay buffering is limited to 10 MiB by default. Use
// WithMaxReplayBodyBytes to change the limit; pass 0 to disable it. If the body
// is too large, execution fails before sending the request with
// ErrReplayBodyTooLarge.
//
// DefaultRetryPolicy(ctx, req, err) closes only intermediate retryable response
// bodies before the next attempt. On exhausted retries, the final response body
// remains open for the caller. Transport failures are returned as plain errors;
// the original request passed to the handler must be forwarded as req.
//
// HTTP results have an explicit routery.Lifetime tied to Response.Body.Close.
// Timeouts and parallel selection preserve winner ownership until completion or
// close. Always close final response bodies, including StatusError.Response.
//
// DefaultRetryPolicy never retries POST/PATCH based solely on status 503.
// Technical body replayability does not prove idempotency or no remote effect.
// Use an explicit caller-owned replay/deduplication policy for those operations.
//
// RetryAfterHint normalizes Retry-After using explicit receipt time and clock
// provenance. Pass its Hint to the attempt scheduler; it does not grant retry
// permission, choose a scope, or guess vendor reset-header semantics.
package routeryhttp
