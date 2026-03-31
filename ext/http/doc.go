// Package routeryhttp adapts net/http clients to routery executors.
//
// The adapter returns regular *http.Response values for successful (2xx) calls.
// For non-2xx responses it returns both the response and *StatusError so callers
// can inspect the body while still using routery.RetryIf with DefaultRetryPolicy.
//
// DefaultRetryPolicy closes only intermediate retryable response bodies before
// the next attempt. On exhausted retries, the final response body remains open
// for the caller.
package routeryhttp
