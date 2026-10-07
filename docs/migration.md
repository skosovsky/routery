# Migration

## v0.5.0 → task13 / next breaking release (unreleased)

This checkout contains unreleased correctness and API changes. No version has been
published by task13. Use matching root/adapter versions after a separate release.

| Previous API/behavior | Caller migration |
| --- | --- |
| Async(kind, reason, payload), BasicAsync(payload, reason) | Handled(kind, reason, payload); use caller async Kind or BasicKindAsync |
| FirstCompleted | FirstSuccessfulPayload; an open stream is a payload, not completed computation |
| ErrTooManyRequests | ErrBulkheadFull |
| Redis CommandExtractor and NewRouteHandler(client, extractor, scan) | CommandInvoker and NewRouteHandler(invoker, scan); similarly remove client from NewStringRouteHandler |
| policy.Dispatch zero,nil for absent selection | Handle Status before dispatch or errors.Is(err, policy.ErrNoSelection) |
| Negative RetryIf/Timeout/gRPC options silently normalized | Fix invalid configuration; zero remains documented default |
| Decision confidence outside finite [0,1] accepted | Normalize valid scores explicitly; handle ErrInvalidConfig / InvalidConfidenceError |
| Slash-joined binding fingerprint | Regenerate stored snapshots/fingerprints; path encoding now preserves boundaries |
| Retry predicates close HTTP response bodies | Return canonical response Lifetime from handlers; executor closes only discarded intermediate bodies |
| Ignored cleanup errors permit another attempt | Handle joined cleanup error and retained partial owner; no next attempt runs |
| OnClose registrations during cleanup execute immediately | Hooks now follow cleanup completion; avoid same-owner recursive Close |
| Empty admission error loses Finish | Supply idempotent Finish even on partial admission error; retain unknown reserve ack pending |
| Quota idempotent repeat can ignore cancelled context | Cancelled waiters return context errors; State remains readable during backend I/O |
| Affinity ineligibility looks like caller eligibility rejection | Read DomainEligible/Rejection separately from caller Reason |
| Evaluation later than context deadline | Callbacks now receive earliest nonzero deadline, including ValidatePinned |
| Mongo write result disappears on error | Inspect nonnil partial payload and original error; IDs/counts do not prove durability/replay permission |

See [API choices](api-choices.md) for every keep/change rationale and the entry-point table.

## v0.5.0: explicit execution policy

This is a clear break. Do not retain compatibility wrappers that restore unsafe
retry, lazy request mutation or automatic stream cancellation. Simple typed
routing remains independent of the optional policy packages.

## RTR-001: selection

Before: mix hard capability/privacy constraints with a weight or threshold and
invoke the highest-scoring endpoint directly.

Now: provide caller-owned `policy.Evaluation`, immutable `policy.Candidate`
descriptors and a `policy.Selector`. Implement `Freeze`, `Eligible` and `Rank`;
only eligible candidates are ranked. Handle `NoEligible` and
`AffinityUnavailable` without invoking a provider. A simple threshold router is
still appropriate when no richer constraints are needed.

Provide separate input, candidate, policy and estimate references. References
are not TTL validation. Before dispatch, pass a mandatory validation callback to
`policy.Dispatch`; use `Selector.ValidatePinned` with the current evaluation clock,
current pinned descriptor and original affinity. A stale rejection requires an
explicit new selection, never replacement inside the freshness callback.

The optional `policy/model` adapter interprets caller capabilities and measured
estimates. Supply task-scoped quality, provenance and validity windows. Required
quality never inherits an optional default. Choose optional estimate policy
explicitly; estimates are neither guaranteed prices nor deadline guarantees.

## RTR-002: execution and repeat ownership

Before: infer replay safety or remote completion from a local error, successful
headers, returned stream handle or absence of visible output.

Now: create one `attempt.Coordinator` for each logical operation and a unique
physical `attempt.Identity` for every actual dispatch. Report explicit phase,
remote outcome and consumer commit facts using `execution.Receipt.Record`.
Buffered output is not committed output. A nil local error does not prove remote
completion. Cancelled or failed attempts may remain unknown and billable.

Use `execution.Boundary` for freshness, admission, dispatch and settlement.
Use `execution.Sequence` as the sole retry owner when classification, replay,
scheduling and explicit next binding are required. Inspect typed decisions;
expected stop/reconcile/defer can have a nil returned error while the provider
error remains in `Failure.Err` and `Trace`. Reconcile unknown outcomes at the host.

Normalize provider hints at the adapter boundary. HTTP callers can use
`RetryAfterHint` with an explicit receipt clock/provenance. Core scheduling
accepts normalized timestamps, not HTTP headers. Honor not-before and scope
cooldown; a hint never supplies replay permission. Defer returns control to the
host and does not create a background job.

Disable hidden SDK retries, account each nested physical dispatch in the shared
coordinator, or explicitly report `NestedAttemptsKnown=false`. Do not claim exact
attempt/usage accounting when nested attempts are invisible.

For parallel requests use `execution.Race` only with replay and duplication
permission, a concurrency bound, a shared attempt budget and an explicit
accepted-result predicate. Keep branch output private until acceptance. The
complete-result profile does not accept an open stream as completion. Early-owned
acceptance is explicit and transfers responsibility for stream validation and
delivery to the host. Preserve loser receipts and late usage; cancellation is
not a refund. A journal snapshot is not a remote-completion barrier.

## RTR-003: quota

Before: treat a local semaphore or observer counter as shared quota admission.

Now: implement the caller-owned atomic `quota.Backend` contract if shared quota
is required. Define trusted scopes, units and estimates yourself. Call
`quota.Admit` for each physical attempt and retain its `Session`. Map the result
to `execution.Admission`; denied/deferred attempts must not dispatch.

Settlement identity remains stable across acknowledgement loss. Send actual
usage with explicit completeness, including overage. Use pending reconciliation
for incomplete or unknown usage; release only with explicit proof of no usage.
Do not release merely because cancellation occurred. Keep streaming concurrency
reservations until completion/close and perform bounded cleanup independently
of the cancelled attempt context. Backoff must not retain a live concurrency
permit from the previous attempt.

Declare fail-closed/fail-open before dispatch. Fail-open explicitly loses the
quota guarantee and is not applicable to unknown reservation acknowledgement.
Handle conflict, incompatible units and expired/unknown handles rather than
silently retrying under a new identity. Durable atomicity, TTL and idempotency
remain the backend owner's responsibility; routery does not provide a ledger.

Executable public-API example: `policy/execution/quota_example_test.go`,
`ExampleBoundary_quotaReconciliation`. It connects host admission/settlement to
Boundary, holds an open stream reservation, preserves unknown usage as Pending,
then reconciles a late complete report with the same settlement identity and
uncapped actual usage. Its backend is deliberately a configured single-reservation
in-memory fixture, not a production/durable quota store.

## RTR-004: affinity and rebuild

Before: fallback by matching an endpoint/model name and reuse opaque continuation
state, or silently drop state when its original endpoint is unavailable.

Now: pass `policy.Affinity` with required/preferred/none strength, trusted scope,
state and compatibility fingerprints, expiry and an explicit compatibility
callback. Required affinity is a hard constraint; unavailable required state
returns `AffinityUnavailable`. Preferred cache locality can be bypassed only by
an eligible candidate and does not guarantee a cache hit or extend TTL.

Never weaken required affinity during pinned validation. Host-authorized rebuild
is a separate transformation: validate trusted scope, reconstruct caller state,
record lineage, create new input/state references, reselect under fresh hard
constraints and dispatch a new physical identity with separate accounting.
Do not put state or credentials into automatic diagnostics. History and state
storage remain host-owned.

Executable public-API example: `policy/execution/affinity_example_test.go`,
`ExampleBoundary_authorizedAffinityRebuild`. It preserves the initial attempt's
usage, returns required-unavailable without another dispatch, rejects missing host
authorization, then rebuilds caller state, updates input references and explicitly
reselects/validates before a new physical attempt. Lineage and each usage charge are
host-owned; this is not a history service or an automatic portability guarantee.

## Correctness breaks

### Sequence authorization and deadlines

Replay is evaluated again after Next, Fresh and admission, and whenever late facts
change before the atomic dispatch authorization. Make it repeatable and idempotent;
perform a consumer reset handshake once in host code and return its evidence,
rather than resetting each time Replay is called. Evidence must remain valid through
the transition. Classify and Schedule run once per failed physical attempt. Changing
between Retry and Fallback after Next is a boundary error, requiring an explicit new
host composition rather than executing an already prepared binding under new policy.

Coordinator.AuthorizeRepeat compares the previous event and changes an allocated
next identity to Dispatched atomically with Update. It returns ErrEventChanged if
host evidence must be refreshed. Already allocated budget is not refunded on denial.
Sequence preserves the prior partial result and failure, and records the new
NotExecuted preflight/settlement in Trace. Facts recorded after authorization remain
visible; they do not promise rollback of an authorized remote action.

Sequence now bounds the attempt context with the effective deadline and transfers
its cancellation to the returned Lifetime. With a real clock, nil DeadlineContext
uses context.WithDeadline. A synthetic clock must provide DeadlineContext in the
same time domain, propagating parent cancellation and deadline expiry. Value
results release their child context on return; resources release it on Close, while
the deadline still applies during consumption. Never pass a historical fixture
timestamp to the wall-clock context constructor.

Settlement callbacks execute outside Receipt's mutex but remain serialized. They
must not reenter the same Receipt. Settlement errors still block new authorization.

### Owned results (BUG-001)

Before: returning a streaming winner cancelled the context needed to read it.

Now: `RouteResult.Lifetime` owns cleanup; `FirstSuccessfulPayload` transfers winner
ownership and cancels/cleans losers. Close the lifetime when finished, including
partial results returned with errors. Generic value results need no lifetime.
For HTTP/object responses the adapter links body close to lifetime close. For
query rows/cursors close the result lifetime, not just the raw handle: the
lifetime also releases wrapper cancellation hooks and held bulkhead permits.
Parent cancellation continues to apply. An opening-only stream interceptor is
not a complete stream-retry protocol; host-owned RPC cancellation and terminal
facts must remain explicit.

### HTTP preparation (BUG-002)

Before:

```go
handler := routerhttp.NewRouteHandler(client, options...)
// Pass a shared request with an unprepared body into parallel branches.
```

Now:

```go
prepared, err := routerhttp.PrepareRequest(request, options...)
if err != nil {
    return err // No transport calls.
}
handler := routerhttp.NewRouteHandler(client)
// Pass prepared to branches; each attempt obtains its own body.
```

`PrepareRequest` performs bounded one-time preparation before fan-out. An
unprepared body without `GetBody` is rejected by the handler, not materialized
lazily. The prepared template is immutable by caller contract. Preparation
consumes/closes the original stream; do not reuse it afterward. A caller-provided
`GetBody` must produce independent readers and be safe for concurrent invocation.

### Unsafe writes (BUG-003)

Before: replayable POST/PATCH bodies could be repeated automatically after 503.

Now: the default retry policy does not allow that repeat. Preserve the failure
and unknown outcome. Opt-in replay needs explicit proven-not-executed or verified
deduplication evidence plus technical body replayability. A header's presence
alone is not evidence. `UnsafeReplay` means insufficient evidence and does not
authorize a repeat. Account every physical attempt separately.

### Cancellation (BUG-004)

Before: zero backoff and permissive predicates could bypass cancellation.

Now: retry/fallback check cancellation before dispatch independently of delay
and predicate. Handle context errors; do not use permissive callbacks to attempt
to revive a cancelled operation. Already-started remote work can still finish.

### Projection ownership

Before: `DispatchAndProject` could return an error or mapped value without a way
to close the canonical partial result. Now: every path returns its shared owner
in `ProjectionMeta.Lifetime`, even if the projector supplied custom metadata.
After consuming or discarding the projected result, call `meta.Lifetime.Close()`
and handle its cleanup error. This also applies when dispatch, projection or error
mapping failed; nil lifetime is safe for plain values. Projectors/error policies
borrow the result and must not create independent ownership of the same resource.

### Canonical result validation

`ValidateRouteResult(result, err)` validates caller-produced results without
invoking a handler or resetting RouteCall metadata. Boundary uses this same core
contract. Abort without error and unknown actions now return `ErrInvalidConfig`
with the original partial payload, match and lifetime. Close that owner even when
validation fails; do not classify this error as a retryable provider failure.
Sequence and Race stop on core config, lifecycle and execution-boundary errors.

### Observation preserves ownership

Logging, Metrics and Tracing no longer replace error results with empty Abort
values. They return the canonical partial payload, existing match and shared
Lifetime, and report canonical validation errors to observer callbacks/spans.
Callers must close the result owner even when the observed handler failed.
Default payload metadata remains shape-only; observation does not prove completion
or release quota/concurrency reservations. Host payload metadata callbacks remain
responsible for what they explicitly publish.

### HTTP timeout ownership

Custom HTTP handlers must attach `RouteResult.Lifetime` for resources and make
`Body.Close` delegate to that same owner. HTTP Timeout now specializes the generic
middleware: it no longer detects bodies or installs a second cancel-on-body-close
wrapper. A result without Lifetime is a value; its timed context is cancelled on
return. Owned partial/error results retain their lifetime until the caller closes
it. `NewRouteHandler` supplies the body/lifetime binding automatically.

## Release and issue closeout

### Explicit tracing projection

Before: `Tracing(tracer, name)` automatically exported error text, Kind/Reason
and route/match fields, and derived an empty name from the route.
Now: `Tracing(tracer, name, nil)` exports only canonical action and bounded failure
status; an empty name is always `routery.handle`. Raw error events are removed.
For additional labels pass an `AttributeProjection[Kind, Reason]` that explicitly
maps `TraceResult` to safe bounded attributes. Do not stringify arbitrary metadata,
copy request-derived Match fields or export raw errors. `routery.action` is reserved
and cannot be overridden by the projection. Explicit names and configured SDK
exporters remain host responsibilities. No two-argument compatibility path exists.

### Release authorization

These API and behavioral changes are substantial: after acceptance and separate
authorization use `make release-break`. `make release-patch` is only for a
separately delivered minor fix without substantial contract/behavior changes.
This document does not authorize either command.

Before closing [issue #3](https://github.com/skosovsky/routery/issues/3), attach
verified evidence for every RTR card, the four regressions, full-module gates,
both independent audit reports and the final completeness fraction. No card is
currently deferred. Do not claim acceptance while those artifacts are missing.
Closing and posting require a separate user command.

The author-facing closeout must explicitly link this migration guide and explain
the changes required in caller code: eligibility/ranking and current descriptors;
phase/outcome/commit facts and one retry owner; normalized hints, typed defer and
owned results; an atomic quota port with pending/overage/idempotent settlement;
and required/preferred affinity with trusted scope and authorized rebuild. State
which contracts are host-owned and which checks actually passed. Never describe
unknown remote effects as exactly-once execution or guaranteed zero cost.

## Adapter replay break

The grpc/mongo/redis/kafka/s3 error-only DefaultRetryPolicy APIs and the gRPC
IdempotentMarker/WithIdempotent wrappers are removed. Use IsTransientError for
classification and RetryPolicy with host attempt.Evidence for synchronous repetition,
or execution.Sequence for coordinated retries/reconciliation. Nil evidence denies replay.
gRPC interceptor defaults deny repeats. No request wrapper is required.

S3 PutObject handlers now accept PutRequest with a body-free Input and independent
BodyFactory, or PreparePutRequest with an explicit buffer limit. Kafka producers now
return PublishResult with original indexed message outcomes on partial failures;
synchronous broker acknowledgment is a host precondition. See adapter-replay-contracts.md.

RetryIf cancellation after a failed call takes precedence while preserving partial
results and the provider error via errors.Join. Successful calls retain success.

## Routing correctness break

The cyclic pairwise prefix comparator is removed. Default order is priority, prefix
group/length on ties, then declaration; LongestPrefixWins places all prefix routes
first by length then priority, with other kinds following by priority/declaration.
Topology fingerprints change format and include nested tables, options, fallback
and decision thresholds. Refresh stored bindings against new topology fingerprints;
there is no automatic rebind or compatibility ordering mode. Optional model quality
with another/missing task is removed before rank, including mismatched defaults.

## Host conformance and observation

Opt-in policy/quota/quotatest accepts unrelated host types and independent-client
fixtures; reports unsupported fault injection separately from passing evidence.
It adds no runtime store or SDK dependency. Logging/metrics Event metadata is raw,
not a redacted serialization format: synchronous callbacks must be bounded, concurrent-safe
and non-panicking, exporting explicit projections. OTel defaults remain private; only
host allowlisted IDs belong in traces, never arbitrary metric labels. Invocation spans
end on return; applications may explicitly create separate lifecycle spans/events.

README now documents retained partial payload/owner on errors. Close canonical owners
even when dispatch failed, then inspect Receipt settlement errors separately and retry
Receipt.Reconcile with stable identity when the host policy permits. Public contracts
for execution authorization, adapter replay and routing topology are the final breaking
APIs from tasks09–11; no legacy wrapper, order mode or implicit rebind is provided.

## Lazy stream resource ownership

Before: a bare lazy stream lost its timed context on handler return, while
`NewLifetime(source.Close)` released middleware permits early when source Close
only requested cancellation.

Now: transfer exclusive consumption to `stream.New(events, cancel, discard)`,
attach `owner.Lifetime()` to the RouteResult (also on partial handler errors), and
consume `owner.Events()`. The cancel port must be callback-safe and nonblocking;
discard synchronously frees an unused source without dispatch. For the optional
concrete bridge use `ext/prompty.New(source)`. Do not consume the raw source too.

Call `owner.Cancel()` from event callbacks and `owner.Close()` outside the iterator
stack. The owner automatically closes its Lifetime after actual source unwind;
Close waits for cleanup/hooks and exposes cleanup errors. Join dispatch, yielded
consumption and Close errors rather than discarding a partial owned result.
Cancellation or a terminal frame is not completed cleanup. An uncooperative source
keeps its permit until it really returns.

Keep one retry owner: consumption errors never trigger handler retry/fallback,
and unknown outcome needs explicit host replay evidence. Invocation spans still
end on handler return. See [the full contract](stream-lifetime-contracts.md).
