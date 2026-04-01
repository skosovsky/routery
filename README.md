# routery

`routery` is a zero-dependency, generic routing and resiliency library for Go.

It routes calls shaped as:

```go
func(context.Context, Req) (Res, error)
```

The core package is intentionally domain-agnostic. It can route any executor:
database calls, HTTP calls, internal service calls, tools, or custom workloads.

## Core Concepts

- `Executor[Req, Res]`: generic execution contract.
- `Middleware[Req, Res]`: composable decorator for executors.
- `Apply(base, mws...)`: middleware composition helper.

Core resiliency and routing primitives:

- `Fallback`
- `RetryIf`
- `RoundRobin`
- `Timeout`
- `PredicateFallback`
- `FirstCompleted`
- `WeightBasedRouter`

Observability primitives are provided in `routery/observability` via callback-based middleware.

## Middleware Order

`Apply` wraps in reverse order, so middleware order changes behavior:

```go
// Retry wraps Timeout(base): timeout is per-attempt.
executorA := routery.Apply(base, routery.RetryIf(...), routery.Timeout(...))

// Timeout wraps Retry(base): timeout is global for the full retry flow.
executorB := routery.Apply(base, routery.Timeout(...), routery.RetryIf(...))
```

See `ExampleApply_middlewareOrder` in tests for runnable output.

## HTTP Adapter (`ext/http`)

`ext/http` is a separate module and keeps root `routery` generic.

It provides:

- `NewExecutor(*http.Client)` to adapt `net/http` to `Executor[*http.Request, *http.Response]`.
- `StatusError` for non-2xx responses.
- `DefaultRetryPolicy(error) bool` for use with `RetryIf`.

Retry policy behavior:

- Retries transport failures and statuses `429`, `502`, `503`, `504`.
- Retries require method/body safety checks.
- Intermediate retryable responses are closed before the next attempt.
- On final exhaustion, the last `*http.Response` is returned with open body so callers can inspect it.

## SQL Adapter (`ext/sql`)

`ext/sql` is a separate module and keeps root `routery` generic.

It provides extractor-based executors for both `*sql.DB` and `*sql.Tx`:

- `NewDBQueryExecutor`, `NewDBExecExecutor`
- `NewTxQueryExecutor`, `NewTxExecExecutor`
- `DefaultRetryPolicy(error) bool`

SQL adapter behavior:

- Request mapping is BYOT via `StatementExtractor[Req]`.
- Query executors return `*sql.Rows`; callers must always close rows with `defer rows.Close()`.
- `DefaultRetryPolicy` never retries `context.Canceled` or `context.DeadlineExceeded`.
- `DefaultRetryPolicy` retries only non-transactional `driver.ErrBadConn`.
- Statement-level retries inside an existing `*sql.Tx` are intentionally not enabled by default.

## Quality Gates

- `make lint`
- `make test` (race-enabled)
- `make cover`
- `make bench`
- `make fuzz`
