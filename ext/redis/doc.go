// Package routeryredis adapts [github.com/redis/go-redis/v9] clients to [github.com/skosovsky/routery.RouteHandler].
//
// Use [NewRouteHandler] with a [CommandExtractor] that returns a [redis.Cmder] bound to the same
// [redis.Client] (for example the result of [redis.Client.Get]). The handler evaluates the
// command via [redis.Cmder.Err] and then maps the result with [ScanResult].
//
// Cache misses: [redis.Nil] is returned as-is and [IsTransientError] never classifies it as transient so a
// [routery.Fallback] can load from another store.
//
// Use [RetryPolicy] with explicit host evidence to authorize replay.
//
// Client/SDK retries are unobservable here. Configure one retry owner; see
// docs/adapter-replay-contracts.md in the routery repository for client controls.
// One handler invocation does not promise one physical network attempt.
package routeryredis
