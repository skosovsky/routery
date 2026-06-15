# routery

`routery` is a zero-dependency, generic routing and resiliency library for Go.

It routes calls shaped as:

```go
func(context.Context, Req, ResultRecorder[Res]) error
```

Outcomes are recorded in a typed `ResultRecorder` (not `context.WithValue`). Declarative ingress is `RouteTable` → `Router.Dispatch`.

The core package is intentionally domain-agnostic. It can route any workload:
database calls, HTTP calls, internal service calls, tools, or custom handlers.

## Core Concepts

- `RouteHandler[Req, Res]`: generic execution contract with typed outcome recording.
- `ResultRecorder[Res]`: records `Stop`, `Next`, `Ignore`, `Async` dispositions.
- `RouteAction`: control flow only — `ActionNext`, `ActionStop`, `ActionAbort`.
- `RouteTable[Req, Res]` + `Router[Req, Res].Dispatch`: declarative routing with priority, nested tables, fallback. Returns `RouteOutcome` with payload, action, and reason code.
- `RouteMiddleware[Req, Res]`: composable decorator for route handlers.
- `ApplyRoute(base, mws...)`: middleware composition helper.
- `RetryPredicate[Req]`: `func(ctx context.Context, req Req, err error) bool` for use with `RetryIf`.

Core resiliency and routing primitives:

- `Fallback`
- `RetryIf` (context- and request-aware predicate; exponential backoff with equal jitter)
- `RoundRobin`
- `Timeout`
- `CircuitBreaker` (closed / open / half-open; `ErrCircuitOpen`)
- `Bulkhead` (non-blocking concurrency limit; `ErrTooManyRequests`)
- `PredicateFallback`
- `FirstCompleted` (returns `ErrNoSuccessfulOutcome` when no handler records payload)
- `WeightBasedRouter`

Observability primitives are provided in `routery/observability` via callback-based middleware. Events include `ResultMeta` (`action`, `reason_code`) and serializable `PayloadMeta` (shape/fingerprint) for safe telemetry.

Optional OpenTelemetry tracing lives in `ext/otel` (separate module, separate `go.mod`).

## Middleware Order

`ApplyRoute` wraps in reverse order, so middleware order changes behavior:

```go
// Retry wraps Timeout(base): timeout is per-attempt.
handlerA := routery.ApplyRoute(base, routery.RetryIf(...), routery.Timeout(...))

// Timeout wraps Retry(base): timeout is global for the full retry flow.
handlerB := routery.ApplyRoute(base, routery.Timeout(...), routery.RetryIf(...))
```

See `ExampleApplyRoute_middlewareOrder` in tests for runnable output.

## Declarative Routing

```go
router, err := routery.NewRouteTable[Req, Res]().
    Route("primary", 10, matcher, handler).
    Mount("group", 5, groupMatcher, nestedTable).
    Fallback(fallbackHandler).
    Build()
if err != nil { /* ... */ }

outcome, err := router.Dispatch(ctx, req)
if err != nil { /* ... */ }
if outcome.HasPayload {
    _ = outcome.Payload
}
// Always check err before reading outcome fields.
// outcome.Action, outcome.ReasonCode — e.g. "no_match" after root no-match
```

## Disposition Semantics

| Recorder call        | RouteOutcome.Action | HasPayload | RouteTable behavior                    |
| -------------------- | ------------------- | ---------- | -------------------------------------- |
| `Stop(payload)`      | `ActionStop`        | true       | Stop dispatch                          |
| `Async(payload)`     | `ActionStop`        | true       | Stop dispatch                          |
| `Ignore(code)`       | `ActionStop`        | false      | Stop dispatch; fallback **not** called |
| `Next(code)`         | `ActionNext`        | false      | Continue to next route or fallback     |
| Handler `return err` | `ActionAbort`       | false      | Abort dispatch                         |

Use `Next` (not `Ignore`) when a matched route should defer to the next route or table fallback.

`FirstCompleted` selects the first parallel handler that records a payload; completion order may differ from registration order.

Route table fingerprints reflect route topology (route IDs and priorities), not matcher function identity.

### Handler contract

Pass-through middleware (`Bulkhead`, `CircuitBreaker`, `RoundRobin`, observability) shares one `ResultRecorder` with the inner handler. Do not call `rec.Stop`, `rec.Ignore`, or `rec.Next` before `return err` unless partial recorder state is intentional for telemetry.

## HTTP Adapter (`ext/http`)

`ext/http` is a separate module and keeps root `routery` generic.

It provides:

- `NewRouteHandler(*http.Client)` to adapt `net/http` to `RouteHandler[*http.Request, *http.Response]`.
- `StatusError` for non-2xx responses.
- `DefaultRetryPolicy(ctx, req *http.Request, err error) bool` for use with `RetryIf`.

Retry policy behavior:

- Retries transport failures and statuses `429`, `502`, `503`, `504`.
- Retries require method/body safety checks.
- Intermediate retryable responses are closed before the next attempt.
- On final exhaustion, the last `*http.Response` is returned with open body so callers can inspect it.

## SQL Adapter (`ext/sql`)

`ext/sql` is a separate module and keeps root `routery` generic.

It provides extractor-based route handlers for both `*sql.DB` and `*sql.Tx`:

- `NewDBQueryRouteHandler`, `NewDBExecRouteHandler`
- `NewTxQueryRouteHandler`, `NewTxExecRouteHandler`
- `DefaultRetryPolicy[Req any](ctx context.Context, req Req, err error) bool`

SQL adapter behavior:

- Request mapping is BYOT via `StatementExtractor[Req]`.
- Query handlers return `*sql.Rows`; callers must always close rows with `defer rows.Close()`.
- `DefaultRetryPolicy` never retries `context.Canceled` or `context.DeadlineExceeded`.
- `DefaultRetryPolicy` retries only non-transactional `driver.ErrBadConn`.
- Statement-level retries inside an existing `*sql.Tx` are intentionally not enabled by default.

## OpenTelemetry (`ext/otel`)

`ext/otel` provides `Tracing[Req, Res](tracer trace.Tracer, spanName string) routery.RouteMiddleware[Req, Res]`
with `span.RecordError` and error status on failure. Core `routery` stays dependency-free.

## More adapters (`ext/grpc`, `ext/redis`, `ext/kafka`, `ext/mongo`, `ext/s3`)

Each of these is a **separate Go module** under `ext/<name>/` with `replace github.com/skosovsky/routery => ../..`, same as `ext/http` and `ext/sql`.

- **`ext/grpc`**: `NewUnaryRouteHandler`, `RetryUnaryInterceptor` / `RetryStreamInterceptor`, `DefaultRetryPolicy` over gRPC status codes; idempotent retries for `DeadlineExceeded` via `GRPCIdempotent()`.
- **`ext/redis`**: `NewRouteHandler` / `NewStringRouteHandler` with `CommandExtractor` + `ScanResult`, `DefaultRetryPolicy` (never retries `redis.Nil`).
- **`ext/kafka`**: `NewProducerRouteHandler` around `kafka-go` writers, `DefaultRetryPolicy` for broker errors; document idempotent producers in package docs.
- **`ext/mongo`**: `NewFindRouteHandler`, `NewInsertOneRouteHandler`, `NewUpdateOneRouteHandler`, `NewDeleteOneRouteHandler` over collection interfaces; `DefaultRetryPolicy` skips retries in transactions (`SessionFromContext` / `TransactionalRequest`).
- **`ext/s3`**: `NewPutObjectRouteHandler` / `NewGetObjectRouteHandler` for AWS SDK v2 S3 clients; `DefaultRetryPolicy` for throttling and 5xx.

A root **`go.work`** includes the core module and all `ext/*` adapters for local development (`go work sync`).

## Quality Gates

- `make lint`
- `make test` (race-enabled)
- `make cover`
- `make bench`
- `make fuzz`
