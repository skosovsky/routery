# routery

`routery` is a zero-dependency, generic routing and resiliency library for Go.

It routes calls shaped as:

```go
func(context.Context, Req) (RouteResult[Res], error)
```

The core package is intentionally domain-agnostic. It can route any handler:
database calls, HTTP calls, internal service calls, tools, or custom workloads.

## Core Concepts

- `Handler[Req, Res]`: generic execution contract.
- `HandlerMiddleware[Req, Res]`: composable decorator for handlers.
- `Apply(base, mws...)`: handler middleware composition helper.
- `RetryPredicate[Req]`: `func(ctx context.Context, req Req, err error) bool` for use with `RetryIf`.

Core resiliency and routing primitives:

- `Fallback`
- `RetryIf` (context- and request-aware predicate; exponential backoff with equal jitter)
- `RoundRobin`
- `Timeout`
- `CircuitBreaker` (closed / open / half-open; `ErrCircuitOpen`)
- `Bulkhead` (non-blocking concurrency limit; `ErrTooManyRequests`)
- `PredicateFallback`
- `FirstCompleted`
- `WeightBasedRouter`

Observability primitives are provided in `routery/observability` via callback-based middleware. Events include `ResultMeta` (`status`, `reason_code`) and serializable `PayloadMeta` (shape/fingerprint) for safe telemetry.

Optional OpenTelemetry tracing lives in `ext/otel` (separate module, separate `go.mod`).

## Middleware Order

`Apply` wraps in reverse order, so middleware order changes behavior:

```go
// Retry wraps Timeout(base): timeout is per-attempt.
handlerA := routery.Apply(base, routery.RetryIf(...), routery.Timeout(...))

// Timeout wraps Retry(base): timeout is global for the full retry flow.
handlerB := routery.Apply(base, routery.Timeout(...), routery.RetryIf(...))
```

See `ExampleApply_middlewareOrder` in tests for runnable output.

## HTTP Adapter (`ext/http`)

`ext/http` is a separate module and keeps root `routery` generic.

It provides:

- `NewHandler(*http.Client)` to adapt `net/http` to `Handler[*http.Request, *http.Response]`.
- `StatusError` for non-2xx responses.
- `DefaultRetryPolicy(ctx, req *http.Request, err error) bool` for use with `RetryIf`.

Retry policy behavior:

- Retries transport failures and statuses `429`, `502`, `503`, `504`.
- Retries require method/body safety checks.
- Intermediate retryable responses are closed before the next attempt.
- On final exhaustion, the last `*http.Response` is returned with open body so callers can inspect it.

## SQL Adapter (`ext/sql`)

`ext/sql` is a separate module and keeps root `routery` generic.

It provides extractor-based handlers for both `*sql.DB` and `*sql.Tx`:

- `NewDBQueryHandler`, `NewDBExecHandler`
- `NewTxQueryHandler`, `NewTxExecHandler`
- `DefaultRetryPolicy[Req any](ctx context.Context, req Req, err error) bool`

SQL adapter behavior:

- Request mapping is BYOT via `StatementExtractor[Req]`.
- Query handlers return `*sql.Rows`; callers must always close rows with `defer rows.Close()`.
- `DefaultRetryPolicy` never retries `context.Canceled` or `context.DeadlineExceeded`.
- `DefaultRetryPolicy` retries only non-transactional `driver.ErrBadConn`.
- Statement-level retries inside an existing `*sql.Tx` are intentionally not enabled by default.

## OpenTelemetry (`ext/otel`)

`ext/otel` provides `Tracing[Req, Res](tracer trace.Tracer, spanName string) routery.HandlerMiddleware[Req, Res]`
with `span.RecordError` and error status on failure. Core `routery` stays dependency-free.

## More adapters (`ext/grpc`, `ext/redis`, `ext/kafka`, `ext/mongo`, `ext/s3`)

Each of these is a **separate Go module** under `ext/<name>/` with `replace github.com/skosovsky/routery => ../..`, same as `ext/http` and `ext/sql`.

- **`ext/grpc`**: `NewUnaryHandler`, `RetryUnaryInterceptor` / `RetryStreamInterceptor`, `DefaultRetryPolicy` over gRPC status codes; idempotent retries for `DeadlineExceeded` via `GRPCIdempotent()`.
- **`ext/redis`**: `NewHandler` / `NewStringHandler` with `CommandExtractor` + `ScanResult`, `DefaultRetryPolicy` (never retries `redis.Nil`).
- **`ext/kafka`**: `NewProducerHandler` around `kafka-go` writers, `DefaultRetryPolicy` for broker errors; document idempotent producers in package docs.
- **`ext/mongo`**: `NewFindHandler`, `NewInsertOneHandler`, `NewUpdateOneHandler`, `NewDeleteOneHandler` over collection interfaces; `DefaultRetryPolicy` skips retries in transactions (`SessionFromContext` / `TransactionalRequest`).
- **`ext/s3`**: `NewPutObjectHandler` / `NewGetObjectHandler` for AWS SDK v2 S3 clients; `DefaultRetryPolicy` for throttling and 5xx.

A root **`go.work`** includes the core module and all `ext/*` adapters for local development (`go work sync`).

## Quality Gates

- `make lint`
- `make test` (race-enabled)
- `make cover`
- `make bench`
- `make fuzz`
