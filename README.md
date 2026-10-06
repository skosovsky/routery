# routery

`routery` is a zero-dependency, generic routing and resiliency library for Go.

Requires **Go 1.27.1 or newer**. Install the root module:

```sh
go get github.com/skosovsky/routery@v0.5.0
```

The published v0.5.0 precedes the task13 changes in this checkout. Select matching
released versions for each separately versioned module; workspace `v0.0.0` plus
local `replace` directives are development configuration, not consumer releases.

| Module | Purpose |
| --- | --- |
| `github.com/skosovsky/routery` | Core, observability projection and optional policy packages; no runtime dependencies |
| `github.com/skosovsky/routery/ext/http` | HTTP request/response ownership and replay |
| `github.com/skosovsky/routery/ext/grpc` | gRPC invocation and initial stream retries |
| `github.com/skosovsky/routery/ext/sql` | SQL rows/results |
| `github.com/skosovsky/routery/ext/mongo` | Mongo cursors and write results |
| `github.com/skosovsky/routery/ext/redis` | Caller-owned Redis command invocation |
| `github.com/skosovsky/routery/ext/kafka` | Kafka delivery mapping |
| `github.com/skosovsky/routery/ext/s3` | AWS S3 SDK mapping |
| `github.com/skosovsky/routery/ext/otel` | Optional OpenTelemetry integration |

Install an adapter explicitly, e.g. `go get github.com/skosovsky/routery/ext/http@v0.5.0`.
Start with this complete core program:

```go
package main

import (
    "context"
    "fmt"
    "github.com/skosovsky/routery"
)

func main() {
    type Request struct { Destination string }
    type Kind string
    type Reason string
    table := routery.NewRouteTable[Request, Kind, Reason, string]()
    table.Route("local", 0,
        func(req Request) bool { return req.Destination == "local" },
        func(routery.RouteCall[Request]) (routery.RouteResult[Kind, Reason, string], error) {
            return routery.Handled(Kind("answer"), Reason("local"), "hello"), nil
        })
    table.Fallback(func(routery.RouteCall[Request]) (routery.RouteResult[Kind, Reason, string], error) {
        return routery.Handled(Kind("answer"), Reason("fallback"), "remote"), nil
    })
    router, err := table.Build()
    if err != nil { panic(err) }
    result, err := router.Dispatch(context.Background(), Request{Destination: "local"})
    if err != nil { panic(err) }
    fmt.Println(result.Kind, result.Reason, result.Payload)
}
```

[Executable root examples](example_test.go) show both string and numeric caller types.
Use the [documentation index](docs/README.md) for routing, ownership, replay and migration.

Optional execution policies are specified in [the public contract](docs/execution-contracts.md).
Start with a `Router` for declarative dispatch; add `RetryIf` only with explicit replay
permission for the operation. The `policy` packages
compose eligibility, ranking, affinity, attempt lifecycle and host-owned quota ports;
model facts live in the opt-in `policy/model` adapter.
Caller changes and correctness breaks are described in [the migration guide](docs/migration.md).
Executable model and ordinary-operation compositions live in
[the boundary examples](policy/execution/example_test.go); both use independent caller types
and run without SDKs, storage or observability.

`policy/execution.Boundary` composes a single physical attempt with explicit lifecycle
facts, freshness checks, optional host admission and resource-bound settlement.
It does not infer remote outcome from local errors or perform implicit retries.
`policy/execution.Sequence` adds opt-in retry/fallback with caller failure classes,
explicit replay evidence, scoped scheduling inputs and a trace of physical attempts.
The host builds each next request/binding; partial output and unknown outcome remain
explicit. Nested attempts must be accounted by the host or marked unobservable.
`policy/execution.Race` adds bounded concurrency and caller acceptance over independent
attempts. Duplication needs explicit cost/replay/effect evidence; the default profile
does not accept a stream handle or first fragment. Late results remain in its journal.

Resource results carry an optional `Lifetime`. Close the canonical result Lifetime when done, including on errors. Adapter body
Close follows its package contract; outer composition may add ownership hooks. Parallel handlers have independent contexts; the winner's context
stays alive until its resource closes. Value payloads require no ownership hook.
HTTP requests with bodies lacking `GetBody` must pass through `PrepareRequest`
before retry or fan-out; handlers never mutate the original request.
POST/PATCH retries need an explicit `RetryPolicy` with verified replay evidence.

It routes calls shaped as:

```go
func(routery.RouteCall[Req]) (routery.RouteResult[Kind, Reason, Payload], error)
```

`RouteCall` carries `context.Context`, the request, and route metadata. `context.Context`
is not used as a side channel for routing state. Declarative ingress is `RouteTable` →
`Router.Dispatch`.

The core package is intentionally domain-agnostic. It routes caller-owned
workloads without taking dependencies on a transport, storage client, tool, or
application framework.

## Core Concepts

- `RouteHandler[Req, Kind, Reason, Payload]`: generic execution contract returning a typed `RouteResult`.
- `RouteResult[Kind, Reason, Payload]`: separates engine action, caller-defined terminal kind, caller-defined reason, payload, and route metadata.
- `ValidateRouteResult(result, err)`: canonical action/error validation retaining owned partial payload, match and lifetime; does not invoke a handler.
- `RouteCall[Req]`: explicit handler input with `Context`, `Request`, and `Match`.
- `RouteAction`: control flow only — `ActionNext`, `ActionStop`, `ActionAbort`.
- `RouteTable[Req, Kind, Reason, Payload]` + `Router.Dispatch`: declarative routing with priority, nested tables, fallback, keyed routes, and decision routes.
- `OutcomeSink[Kind, Reason, Payload]`: explicit event sink for observed dispatches via `DispatchWithSink`.
- `OutcomeProjector[Kind, Reason, Payload, Projection]`: converts canonical route results into caller-owned projections.
- `ErrorPolicy[Kind, Reason, Payload, Projection]`: maps dispatch/projection/system errors without forcing string reason codes.
- `RouteBinding[Branch, Binding]`: typed branch and payload binding with route snapshot and caller freshness data.
- `DecisionTable[Input, Action, Reason]`: ordered typed decision table for recovery/preflight-style routing.
- `RouteRegistry[Req, Kind, Reason, Payload]`: runtime registration with immutable dispatch snapshots.
- `RouteMiddleware[Req, Kind, Reason, Payload]`: composable decorator for route handlers.
- `ApplyRoute(base, mws...)`: middleware composition helper.
- `RetryPredicate[Req]`: `func(ctx context.Context, req Req, err error) bool` for use with `RetryIf`.

Core resiliency and routing primitives:

- `Fallback`
- `RetryIf` (context- and request-aware predicate; exponential backoff with equal jitter)
- `RoundRobin`
- `Timeout`
- `CircuitBreaker` (closed / open / half-open; `ErrCircuitOpen`)
- `Bulkhead` (non-blocking concurrency limit; `ErrBulkheadFull`)
- `PredicateFallback`
- `FirstSuccessfulPayload` (returns `ErrNoSuccessfulOutcome` when no handler returns a terminal payload)
- `WeightBasedRouter`
- `Chain` for typed fallthrough without `handled bool`

Routing primitives:

- `OnKey(table, extractor).Exact(...)`
- `OnStringKey(table, extractor).Prefix(...).LongestPrefixWins()`
- `OnDecision(table, classifier).Case(...)`
- `OnDecisionTable(table, decisions).Case(...)`
- `MatchDecisionReason[T](match)` for reading typed classifier reasons from decision-route metadata

Projection and binding primitives:

- `DispatchAndProject(ctx, router, req, projector, policy)` for canonical dispatch + caller-owned projection.
- `DefaultProjectionMeta(result)` for action, kind, reason, route match, safe payload type metadata, and the shared resource `Lifetime`.
- `NewRouteBinding(branch, binding, match, inputFingerprint, revision)` for route-owned binding snapshots.
- `ValidateSnapshotFreshness(snapshot, current, policy)` for caller-defined stale/rebind checks.

`ProjectRouteResult` and `DispatchAndProject` preserve the canonical owner in
`ProjectionMeta.Lifetime`, including dispatch/projection errors and error-policy
mapping. Close that lifetime after consuming or discarding the projection, even
when an error was returned. A custom projector cannot clear or replace this owner.

Use projection when application code currently has local helpers such as
`ToXResult(RouteResult)` and `RouteErrorToXResult(error)`. Keep dispatch canonical,
then centralize projection and error mapping at the boundary:

```go
type Projection struct {
    Action  routery.RouteAction
    RouteID routery.RouteID
}

projector := routery.OutcomeProjectorFunc[Kind, Reason, Payload, Projection](
    func(result routery.RouteResult[Kind, Reason, Payload]) (
        Projection,
        routery.ProjectionMeta[Kind, Reason],
        error,
    ) {
        meta := routery.DefaultProjectionMeta(result)
        return Projection{Action: meta.Action, RouteID: meta.Match.RouteID}, meta, nil
    },
)

policy := routery.ErrorPolicyFunc[Kind, Reason, Payload, Projection](
    func(routeErr routery.RouteError[Kind, Reason, Payload]) (Projection, error) {
        return Projection{Action: routeErr.Result.Action, RouteID: routeErr.Match.RouteID}, nil
    },
)

projection, meta, err := routery.DispatchAndProject(ctx, router, req, projector, policy)
_ = projection
// Consume or discard the payload before closing its canonical owner.
err = errors.Join(err, meta.Lifetime.Close())
_ = err // Return or handle the combined dispatch/cleanup failure.
```

The projector owns successful result shape. The policy owns dispatch/projection/system
failure mapping. Neither path mutates routing state or encodes valid routing outcomes as
`error`.

Mutable route registration:

- `NewRouteRegistry[Req, Kind, Reason, Payload]()` creates an empty registry.
- `NewRouteSpec(...)`, `ExactRouteSpec(...)`, `PrefixRouteSpec(...)`, and `LongestPrefixRouteSpec(...)` create generic route specs.
- `registry.Snapshot()` returns an immutable `RouteTableSnapshot`; concurrent dispatch never observes a partially rebuilt table.

Observability primitives are provided in `routery/observability` via callback-based middleware. Logging events include raw request/error and caller metadata. `PayloadMeta` does not
redact the event. Use a bounded host projection; callbacks run synchronously and may
run concurrently. Never use arbitrary IDs/request data as metric labels.

## Middleware Order

`ApplyRoute` wraps in reverse order, so middleware order changes behavior:

```go
// Retry wraps Timeout(base): timeout is per-attempt.
handlerA := routery.ApplyRoute(base, routery.RetryIf(...), routery.Timeout(...))

// Timeout wraps Retry(base): timeout is global for the full retry flow.
handlerB := routery.ApplyRoute(base, routery.Timeout(...), routery.RetryIf(...))
```

The two compositions intentionally produce different timeout and retry boundaries.

## Declarative Routing

```go
type Kind string
type Reason string

const (
    KindHandled Kind = "handled"
    ReasonOK    Reason = "ok"
)

router, err := routery.NewRouteTable[Req, Kind, Reason, Payload]().
    Route("primary", 10, matcher, handler).
    Mount("group", 5, groupMatcher, nestedTable).
    Fallback(fallbackHandler).
    Build()
if err != nil { /* ... */ }

outcome, err := router.Dispatch(ctx, req)
// Handle partial results and close their canonical owner even when err != nil.
if err != nil {
    cleanupErr := outcome.Lifetime.Close()
    return errors.Join(err, cleanupErr)
}
if outcome.HasPayload {
    _ = outcome.Payload
}
// After consuming or discarding the payload:
if cleanupErr := outcome.Lifetime.Close(); cleanupErr != nil {
    return cleanupErr
}
// Error results may retain payload/match/lifetime; HasPayload is not proof of success.
// outcome.Action, outcome.Kind, outcome.Reason, outcome.Match
```

## Disposition Semantics

| Result helper        | RouteResult.Action | HasPayload | RouteTable behavior                    |
| -------------------- | ------------------ | ---------- | -------------------------------------- |
| `Handled(...)`       | `ActionStop`       | true       | Stop dispatch                          |
| `Ignored(...)`       | `ActionStop`       | false      | Stop dispatch; fallback **not** called |
| `Next(...)`          | `ActionNext`       | false      | Continue to next route or fallback     |
| Handler `return err` | `ActionAbort`      | preserved if supplied | Abort dispatch; caller retains partial owner |

Use `Next` (not `Ignored`) when a matched route should defer to the next route or table fallback.
`ActionAbort` without a non-nil error is an invalid handler result.

`FirstSuccessfulPayload` selects the first parallel handler that returns a terminal payload; completion order may differ from registration order.

Route table fingerprints include nested topology, routing options and classifier memoization groups; matcher/handler function identity and opaque caller implementations are excluded.

### Handler contract

Pass-through middleware (`Bulkhead`, `CircuitBreaker`, `RoundRobin`, observability) receives the same `RouteCall` as the inner handler. Middleware can read `call.Match` directly; it must not store route metadata in `context.Context`.

## Package Boundary

The root package documents only the universal routing contract: `Req`, `Kind`,
`Reason`, `Payload`, dispatch, middleware, and typed results. Integration
packages are responsible for their own request mapping, retry policy, and
dependency-specific behavior.

Root examples should remain generic and caller-owned. Do not make core docs
depend on neighboring package names, concrete clients, or external libraries.

## Quality Gates

- `make lint`
- `make test` (race-enabled)
- `make cover`
- `make bench`
- `make fuzz`

Routing order is explicit: priority first, then prefix group/length on priority ties,
then declaration order. LongestPrefixWins moves the complete prefix group first,
ordered by length then priority. See [routing contracts](docs/routing-contracts.md)
for recursive topology fingerprints, breaker generations and task-scoped quality.

Host integrations can run [quotatest](policy/quota/quotatest/suite.go) with their own
fixture/types. [Conformance contracts](docs/conformance-contracts.md) explain required
checks, unsupported faults and storage-restart limits. Executable [lifecycle examples](policy/execution/lifecycle_example_test.go)
show a minimal model value and an ordinary partial resource, bounded cleanup, permit
ownership, unknown outcome and late settlement. `Lifetime.Close` reports resource cleanup;
`Receipt.Snapshot` reports settlement errors and `Receipt.Reconcile` retries the stable
settlement identity. Always handle both; closing a resource does not prove remote completion.
