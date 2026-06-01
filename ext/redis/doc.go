// Package routeryredis adapts [github.com/redis/go-redis/v9] clients to [github.com/skosovsky/routery.Handler].
//
// Use [NewHandler] with a [CommandExtractor] that returns a [redis.Cmder] bound to the same
// [redis.Client] (for example the result of [redis.Client.Get]). The handler evaluates the
// command via [redis.Cmder.Err] and then maps the result with [ScanResult].
//
// Cache misses: [redis.Nil] is returned as-is and [DefaultRetryPolicy] never retries it so a
// [routery.Fallback] can load from another store.
//
// Pair with [github.com/skosovsky/routery.RetryIf] and [DefaultRetryPolicy] for resilient Redis calls.
package routeryredis
