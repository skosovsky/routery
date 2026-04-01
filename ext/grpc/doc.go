// Package routerygrpc provides gRPC client helpers for [github.com/skosovsky/routery]:
// unary executors, client interceptors with [github.com/skosovsky/routery.RetryIf], and a
// [DefaultRetryPolicy] based on [google.golang.org/grpc/status] codes.
//
// DeadlineExceeded from the server is retried only when the request value implements
// [IdempotentMarker] and [IdempotentMarker.GRPCIdempotent] returns true.
//
// Streaming: [RetryStreamInterceptor] retries only the initial stream creation (the
// Streamer call), not individual Recv/Send failures on an established stream.
package routerygrpc
