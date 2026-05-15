// Package routeryhttp adapts net/http clients to routery executors.
//
// The adapter returns regular *http.Response values for successful (2xx) calls.
// For non-2xx responses it returns both the response and *StatusError so callers
// can inspect the body while still using routery.RetryIf with DefaultRetryPolicy.
//
// NewExecutor makes request bodies replayable. When a request has Body but no
// GetBody, the executor reads Body into memory once, closes the original Body,
// and updates the original *http.Request with a fresh Body, GetBody, and
// ContentLength. This package assumes *http.Request values are not used
// concurrently.
//
// Replay buffering is limited to 10 MiB by default. Use
// WithMaxReplayBodyBytes to change the limit; pass 0 to disable it. If the body
// is too large, execution fails before sending the request with
// ErrReplayBodyTooLarge.
//
// DefaultRetryPolicy(ctx, req, err) closes only intermediate retryable response
// bodies before the next attempt. On exhausted retries, the final response body
// remains open for the caller. Transport failures are returned as plain errors;
// the original request passed to the executor must be forwarded as req.
//
// Use Timeout for HTTP chains instead of routery.Timeout. The generic timeout
// cancels the context as soon as Execute returns, but HTTP response bodies are
// often read after headers have been returned. Timeout ties cancellation to
// Response.Body.Close.
//
// Middlewares between routery.RetryIf and NewExecutor must not clone
// *http.Request with Clone or WithContext. NewExecutor mutates the original
// request to install replay support, and cloning in between would hide that
// mutation from RetryIf.
//
// Breaking changes in this package are intentional: original requests are
// mutated for replay support, POST/PATCH requests with arbitrary reader bodies
// can become retryable, replay buffering has a default size limit, HTTP chains
// should use Timeout instead of routery.Timeout, and request-cloning middleware
// must not be placed between RetryIf and NewExecutor.
package routeryhttp
