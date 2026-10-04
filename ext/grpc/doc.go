// Package routerygrpc provides gRPC client helpers for [github.com/skosovsky/routery]:
// unary handlers, client interceptors with [github.com/skosovsky/routery.RetryIf], and a
// [IsTransientError] based on [google.golang.org/grpc/status] codes.
//
// Retry interceptors fail closed when no explicit predicate is configured.
// IsTransientError alone does not authorize replay. DataLoss is not transient.
//
// Streaming: [RetryStreamInterceptor] retries only the initial stream creation (the
// Streamer call), not individual Recv/Send failures on an established stream.
// The interceptor retains the caller context; it does not own a generic RouteResult
// Lifetime or cancel an established stream on return. CloseSend is a send-side
// half-close, not receive completion. The caller cancels its RPC context on abandonment.
// For execution policy composition, adapt stream-open/terminal/commit facts explicitly,
// attach an owned lifetime that cancels the RPC, and disable hidden interceptor/SDK
// retries or account them in the shared Coordinator. A returned stream is not accepted
// completion; terminal usage and remote outcome remain provider/host evidence.
//
// Client/SDK retries are unobservable here. Configure one retry owner; see
// docs/adapter-replay-contracts.md in the routery repository for client controls.
// One handler invocation does not promise one physical network attempt.
package routerygrpc
