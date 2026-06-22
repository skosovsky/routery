# routery

`routery` is a zero-dependency, generic routing and resiliency library for Go.

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
- `Bulkhead` (non-blocking concurrency limit; `ErrTooManyRequests`)
- `PredicateFallback`
- `FirstCompleted` (returns `ErrNoSuccessfulOutcome` when no handler returns a terminal payload)
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
- `DefaultProjectionMeta(result)` for action, kind, reason, route match, and safe payload type metadata.
- `NewRouteBinding(branch, binding, match, inputFingerprint, revision)` for route-owned binding snapshots.
- `ValidateSnapshotFreshness(snapshot, current, policy)` for caller-defined stale/rebind checks.

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
_ = meta
_ = err
```

The projector owns successful result shape. The policy owns dispatch/projection/system
failure mapping. Neither path mutates routing state or encodes valid routing outcomes as
`error`.

Mutable route registration:

- `NewRouteRegistry[Req, Kind, Reason, Payload]()` creates an empty registry.
- `NewRouteSpec(...)`, `ExactRouteSpec(...)`, `PrefixRouteSpec(...)`, and `LongestPrefixRouteSpec(...)` create generic route specs.
- `registry.Snapshot()` returns an immutable `RouteTableSnapshot`; concurrent dispatch never observes a partially rebuilt table.

Observability primitives are provided in `routery/observability` via callback-based middleware. Events include action, typed kind, typed reason, `RouteMatch`, and serializable `PayloadMeta` (shape/fingerprint) for safe telemetry.

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
if err != nil { /* ... */ }
if outcome.HasPayload {
    _ = outcome.Payload
}
// Always check err before reading outcome fields.
// outcome.Action, outcome.Kind, outcome.Reason, outcome.Match
```

## Disposition Semantics

| Result helper        | RouteResult.Action | HasPayload | RouteTable behavior                    |
| -------------------- | ------------------ | ---------- | -------------------------------------- |
| `Handled(...)`       | `ActionStop`       | true       | Stop dispatch                          |
| `Async(...)`         | `ActionStop`       | true       | Stop dispatch                          |
| `Ignored(...)`       | `ActionStop`       | false      | Stop dispatch; fallback **not** called |
| `Next(...)`          | `ActionNext`       | false      | Continue to next route or fallback     |
| Handler `return err` | `ActionAbort`      | false      | Abort dispatch                         |

Use `Next` (not `Ignore`) when a matched route should defer to the next route or table fallback.
`ActionAbort` without a non-nil error is an invalid handler result.

`FirstCompleted` selects the first parallel handler that returns a terminal payload; completion order may differ from registration order.

Route table fingerprints reflect route topology (route IDs, priorities, match kinds, and static keys), not matcher function identity.

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
