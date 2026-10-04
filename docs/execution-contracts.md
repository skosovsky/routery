# Execution policies: public contract

This contract precedes the task 8 implementation. The root module keeps generic dispatch and resource ownership; opt-in `policy` packages provide selection, attempts, admission and affinity. Provider-specific facts remain caller-owned. No SDK, storage, pricing or observation dependency is required.

## Ownership and errors

`RouteResult.Lifetime` is optional explicit resource ownership. `Lifetime.Close` releases resources exactly once and returns the stored cleanup error. `OnClose` attaches cancellation/release callbacks without racing close; registrations after close execute immediately. Copies of a result share ownership. Value payloads have no lifetime. Resource adapters must attach one. Parent context cancellation still applies.

Projection preserves ownership, not only routing diagnostics. `ProjectionMeta.Lifetime`
always carries the original canonical result's owner through `ProjectRouteResult`
and `DispatchAndProject`, including nil/failed projectors, dispatch errors and error
policy mappings. A projector cannot replace or clear this owner through custom
metadata. Projection does not close it automatically: the caller closes the shared
lifetime after consuming or discarding the projection, even when an error is
returned. Projectors and error policies borrow the canonical result; any projected
resource shares its lifetime, rather than acquiring a second independent owner.

`FirstCompleted` uses per-branch contexts, cancels losers, cleans discarded/late owned results, and transfers the winner context to its lifetime. It has no model semantics. Safe acceptance, replay permissions, per-branch admission and attempt accounting are opt-in execution policy. Generic `Timeout` likewise attaches its cancellation to an owned result instead of cancelling a live stream on return.

HTTP `Timeout` is a typed specialization of generic `Timeout`, not a separate
body-ownership mechanism. Custom HTTP resource handlers must attach a Lifetime;
without one the result is treated as a plain value and the timed context is
cancelled on return. Timeout never inspects or wraps a body to infer ownership.

HTTP `PrepareRequest` consumes and closes a body without GetBody before retries/fan-out, returns a new frozen template, and installs a factory for independent bodies. It never mutates the original request fields. HTTP handlers reject unprepared bodies. Prepared headers and factories must be immutable/concurrency-safe while dispatched. Responses have a lifetime tied to Body.Close. Default POST/PATCH retry is disabled; safe replay requires an explicit policy.

Expected policy outcomes are values, not errors. Invalid metadata/configuration, callback failures and backend errors remain errors. Remote outcome certainty, route action, attempt decision and settlement state are distinct contracts. Context only carries cancellation/deadline.

`ValidateRouteResult(result, err)` is the shared canonical result boundary: a
non-nil error converts action to Abort while retaining payload, match and Lifetime;
Abort without error or an unknown action returns ErrInvalidConfig with the same
owned partial result. It does not invoke callbacks, cancel context, close resources
or infer remote outcome. Execution Boundary applies this validation immediately
after Dispatch without resetting its RouteCall metadata or its physical receipt.

Optional Logging, Metrics and Tracing middleware apply the same canonical result
validation and preserve owned partial payload, Lifetime and existing route match on
error. Observer callbacks receive canonical error/action and default shape-only
payload metadata, not a replacement empty result. They do not close or take over
the resource; the same caller/composition owner must close it. Observation neither
releases a Bulkhead permit nor finalizes an execution Receipt on its own.

Tracing requires an explicit third argument, `AttributeProjection`, or nil for
safe defaults. Its automatic output is only canonical `routery.action`, a fixed
`routery.handle` span name when no host name is supplied, and bounded `route failed`
error status. No raw error event, Kind/Reason, Match fields, request, payload or
opaque state is exported automatically. The optional projection borrows canonical
Action/Kind/Reason/Match/Err (not payload or ownership); the host must return only
allowlisted bounded attributes and redact sensitive data. It cannot override the
reserved `routery.action` attribute. Explicit span names and tracer SDK configuration
are host-owned and must likewise avoid secrets and high-cardinality labels.

Resource extension ownership: query rows and document cursors attach a Lifetime;
callers close the lifetime, not just the raw resource, to notify routing callbacks.
Cursor cleanup removes cancellation from the invocation context; host driver network
timeouts still bound cleanup. Object download bodies attach the same ownership and
their Body.Close delegates to Lifetime.Close. Adapters require independently owned
output/body objects for each invocation. Headers do not end a streaming lifetime.

## Selection and affinity

Core validation errors preserve the canonical partial result, payload metadata and
Lifetime while changing the action to Abort. The caller remains responsible for
closing that owned result; FirstCompleted may already have closed it, and another
Close is idempotent and exposes the stored cleanup error. Invalid action text is
not automatically published. Router/Chain fallthrough discards ActionNext resources
before proceeding. Router cleanup failure stops routing and returns that failure;
final nonterminal metadata can be returned but never promises a live Next resource.

Temporal dispatch validation is mandatory, not implied by matching fingerprints.
Selector.ValidatePinned receives the current evaluation clock, candidate and affinity;
it checks epochs, pinned identity, original affinity metadata, hard eligibility,
required compatibility and required ranking inputs without selecting another branch.
The original affinity stamp cannot be weakened or replaced under a saved binding.
Expired descriptor/quality/state facts reject even when all epochs are unchanged.
PinnedError exposes a bounded eligibility reason and unwraps to ErrStaleSnapshot.
policy.Dispatch requires an explicit validation callback for selected results. A nil
callback is invalid; no-eligible/affinity-unavailable still returns without dispatch.
Selection.ValidateFreshness is only the epoch guard; it is not temporal validation.
Dispatch accepts only the declared SelectionStatus values; an unknown status returns
ErrInvalidSelection rather than impersonating no-eligible or affinity-unavailable.
Use ValidatePinned from Boundary.Fresh before admission and again before dispatch.

Generic `policy.Selector` receives immutable candidates, a request projection, fixed evaluation time, snapshot references and caller eligibility/ranking callbacks. Candidate identity and fingerprints must be explicit. Eligibility runs before ranking; unknown required facts reject. Ranking only sees eligible candidates; deterministic ties preserve declaration order. Explanations contain safe references and typed reasons, never descriptors or request data. Selection produces existing `RouteBinding`; dispatch requires freshness validation against current references. Host freeze/copy responsibility is explicit for maps/pointers.

An opt-in model policy projects capabilities, window, residency/retention constraints, measured quality and cost/latency estimates into generic eligibility/ranking. Unknown/stale mandatory data rejects; optional estimates use declared reject/ignore/default behavior. Quality is task-scoped. Latency metrics distinguish first fragment, inter-fragment, completion and throughput with percentile/window/sample/cache-regime metadata. Estimates do not guarantee cost, deadline, quality or cache hits.

Affinity descriptors use required/preferred/none strength and trusted scope, endpoint constraints, state identity, expiry and invalidation fingerprint. Required affinity is a hard filter; unavailable required state returns affinity-unavailable. Preferred affinity only ranks eligible candidates. Required scope mismatch/stale state errors before dispatch. State compatibility is caller-owned. Rebuild is an explicit host transformation followed by fresh selection and a new physical attempt/lineage; routery does not store sessions or reconstruct history.

## Attempts and scheduling

`policy/execution.Boundary` composes one physical invocation with the existing
RouteCall/RouteResult contract. The host supplies its Coordinator and unique Identity,
Fresh callback, optional Admit port, Dispatch callback and a separate bounded cleanup
context factory. Freshness and cancellation run before admission and again after it,
before dispatch. Admission denial/defer is a typed outcome with zero provider calls.
Failed/uncertain admission retains the allocated identity in the returned Receipt.
Admitted-but-not-dispatched attempts record terminal NotExecuted and finalize their
lease. Dispatch receives an explicit Receipt to report phase/commit/remote outcome;
local return/error/cancellation never establishes remote success or zero usage.
Value results finalize at return. Owned results finalize through Lifetime.OnClose;
settlement errors remain inspectable on Receipt and may be explicitly reconciled.
Late definitive events can resolve terminal unknown and repeat the same idempotent
host settlement. Finish callbacks must not reenter their Receipt; host policy owns
usage completeness and must use stable settlement identities. No mandatory quota,
storage, observer, model types or implicit retry is introduced.

`policy/attempt` records operation and unique physical attempt identities with monotonic phase, independent committed output, and not-executed/completed/unknown outcome. Events are explicit, synchronized and idempotent when identical; contradictions error. Late completion updates unknown outcome without issuing another dispatch. Unknown never implies no side effect or zero cost. Default retry requires safe replay, no committed output, budget and deadline; reconcile/stop are explicit outcomes. Reset after output requires a caller protocol.

Scheduling takes local backoff, normalized hint with source/clock/uncertainty and caller scope cooldown; start is their maximum. Invalid hints use a declared reject/ignore policy. Hint does not grant replay. Start beyond deadline yields defer/stop. Cancellation prevents admission/dispatch even with zero delay. One layer owns retries; hidden nested attempts are either accounted or explicitly unavailable.

Scheduling timestamps use the portable UTC calendar range of years 1 through 9999;
the zero timestamp is reserved for absence. AddDelay validates nonnegative durations,
range and round-trip addition. Out-of-range local backoff/deadline/cooldown errors;
out-of-range hint/uncertainty follows the declared invalid-hint policy. Wait validates
its clock and repeats capped timer waits until not-before is actually reached; large
durations cannot truncate the declared lower bound into an early start.
HTTP RetryAfterHint accepts one Retry-After field containing decimal delay-seconds
or an HTTP date (per https://www.rfc-editor.org/rfc/rfc9110.html#section-10.2.3).
Relative delays are based on the explicit response-receipt time, not selection time.
The caller supplies source/clock reference and uncertainty; no server clock, Date or
reset-header convention is guessed. Absent header returns an absent hint. Empty,
ambiguous, negative, fractional, malformed or overflowing values return ErrInvalidHint
with a present-but-invalid hint, so an explicit ignore policy remains observable.
HTTP header values never appear in returned errors. Vendor reset formats are normalized
by the host to the same Hint; no automatic provider scope or replay permission is inferred.
StatusError.Error likewise contains only the numeric status code, never the raw
provider reason phrase; the original response remains explicitly inspectable by its owner.

`execution.Sequence` is opt-in retry/fallback composition over Boundary and attempt
Decide/Schedule/Wait. The host classifies failures into its own comparable Class,
supplies replay evidence and scoped scheduling inputs, then constructs the next
request/binding and unique identity after waiting. Every repeat reruns Boundary.
It preserves a trace of all attempts and the final partial result. Stop/reconcile/defer
are typed decisions; the original provider error is retained in Failure.Err and trace.
Configuration, cancellation, admission/backend, callback and settlement failures are
returned errors and cannot automatically trigger another reservation.
ErrInvalidEvent, ErrInvalidBoundary and core ErrInvalidConfig from dispatch are control-contract failures,
not provider failures: Sequence returns them without classification or replay,
even when the host's retry predicate would otherwise permit duplication.
Intermediate resources close before waiting; settlement failure blocks repetition. Replay facts and
remaining budget are rechecked after waiting against the current Receipt. A supplied
Wait must honor cancellation and advance the supplied clock to not-before; an early
return never dispatches. Explicit operation deadline and context deadline cannot be
extended by a scheduling callback. Next must keep the operation ID. NestedAttemptsKnown
is a host declaration: false explicitly marks hidden physical attempts as unobservable,
so exact remote accounting is not claimed. This layer must be the sole retry owner, or
host-owned nested calls must use the same Coordinator and account their identities.

Repeat authorization has one explicit linearization point after Next, both Fresh
checks and admission: Coordinator.AuthorizeRepeat compares the previous complete
event and marks the already allocated next identity Dispatched under the same lock
used by Update. Changed facts return ErrEventChanged; Sequence refreshes the prior
Receipt, reevaluates host Replay outside locks and tries again. Replay callbacks
are repeatable/idempotent, must not dispatch, and declare evidence that remains
valid through this transition. ResetProtocol declares an already completed host
handshake, not a command to reset on each evaluation. Classification and scheduling
remain fixed for the transition; changing Retry to Fallback after Next is rejected
as ErrInvalidBoundary, so a binding prepared for one action cannot execute as another.
Each reevaluation uses current remote facts, commit status, settlement error and
budget (including the next identity already allocated for this repeat).
The cancellation channel is checked inside Coordinator after acquiring its mutex;
ErrAuthorizationCancelled leaves the next identity undispatched. Waiting for prior
settlement is cancellation-aware, and the clock/deadline is checked again after that
wait outside the Receipt mutex. Now is a clock projection and must not reenter the
Receipt whose authorization is being evaluated.
The preceding Receipt serializes authorization with its Record/settlement; no
internal mutex is held during Replay, Fresh, Admit, Dispatch or cleanup callbacks.
Finish still must not reenter its Receipt because settlement is serial, even though
the callback runs outside its mutex. If authorization is denied, the new identity
remains allocated, its reservation settles as terminal NotExecuted and the previous
partial result/Failure remain final; the new preflight result is retained in Trace.
Events accepted before authorization participate in the decision. Events accepted
after it remain observable and reconcilable, but do not revoke an authorized call
or promise rollback. This is local dispatch authorization, not remote exactly-once.

Sequence.Deadline, Schedule deadlines, Now and context deadlines share one clock
domain. The earliest bound is retained across attempts and checked after host
callbacks and immediately before dispatch authorization. DeadlineContext optionally
constructs a bounded child context; nil uses context.WithDeadline. Hosts using a
synthetic clock must supply a context factory in that same domain which propagates
parent cancellation and cancels at the declared deadline. It must return a non-nil
context/cancel pair with a deadline no later than requested. Passing synthetic
calendar timestamps to the wall-clock default is unsupported. Child contexts are
released at return for values or transferred to the canonical Lifetime for owned
results, including partial errors; reaching the deadline still cancels a live stream.
Deadline checks do not establish remote NotExecuted after dispatch. DeadlineContext
and Replay failures remain control/callback errors and cannot authorize repetition.

Pre-dispatch retry is explicit host composition, not Sequence's provider retry path.
A failed Boundary returns Started=false, its allocated physical identity, remaining
budget and terminal NotExecuted when dispatch demonstrably never began. The host
may classify a known preflight failure, call Decide/Schedule/Wait, explicitly refresh
the request/binding and run Boundary again with the same operation and a new attempt.
Unknown reservation acknowledgements/backend failures are not blanket authorization
for this path. The pre_dispatch_test.go fixture proves zero first external calls,
one subsequent dispatch and separate budget allocation for both physical identities.

Safe race requires replay/duplicate-cost permission, concurrency and total attempt limits, accepted-result validation, and per-attempt admission. Output must be held by the caller until acceptance. Losers receive cooperative cancellation; owned results are cleaned and usage/unknown outcomes are still settled. Winner lifetime stays alive. Delayed hedging is outside scope.

`execution.Race` composes a bounded pool of Boundary invocations with FirstCompleted.

Lifecycle/config errors (`attempt.ErrInvalidEvent`, `execution.ErrInvalidBoundary`, `routery.ErrInvalidConfig`,
including wrapped/joined errors) close a shared queued-plan gate. Taking a plan and
closing this gate are serialized: no worker takes another plan after failure is
observed. Plans already taken remain independently owned/accounted and may finish;
this is not remote rollback. Register failure before resource cleanup, so blocked
cleanup cannot authorize queued work. After winner validation, acceptance is
finalized under that same gate lock: a fatal error observed before this explicit
linearization point overrides the concurrent winner and closes its ownership.
Finalization also stops queued-plan authorization. Errors observed afterward remain
visible in the shared Journal even if Run has not physically returned yet; acceptance
is already decided and cannot be retroactively changed. The journal is not a
completion barrier.
Multiple plans require Replayable and DuplicateCost permission and either ReadOnly
or explicit DuplicateEffects evidence. Forbidden duplication is a typed outcome with
zero admission/dispatch. Plans have distinct physical IDs under the same operation;
each uses the shared Coordinator budget and independent admission. Caller request
projections are immutable; adapters/factories own independent transport bodies.
CompleteOnly (default) evaluates acceptance only for terminal facts and a successful
payload. Stream handles/first fragments cannot win. EarlyOwned is a separate explicit
profile requiring stream-open facts and owned resources; it promises no full-stream
validation. Accept callbacks must be concurrency-safe and hold all output; committed
branch output is rejected. Routery does not buffer provider streams automatically.
Rejected results close before a worker takes another plan. Accepted losers are closed
by FirstCompleted; late results retain Receipts and settle through Boundary. The winner
keeps its lifetime. A synchronized Journal exposes all returned attempts, errors and
acceptance decisions, including late losers. A journal snapshot is not a completion
barrier; the host tracks provider completion and resource close explicitly. Noncooperative
providers may return later or never; cancellation is not zero usage. Journal entries
retain caller-owned result data for explicit inspection, never automatic publication.
Race carries the same explicit NestedAttemptsKnown declaration as Sequence; false
means the Coordinator budget counts observed Boundary attempts, not hidden SDK calls.

## Quota port and ordering

The local Bulkhead semaphore also follows explicit result ownership: value results
release at handler return; owned results retain their permit until Lifetime.Close,
including error results with partial resources. Cancellation prevents new invocation.
It is a concurrency limit, not distributed quota or evidence of remote cancellation.

`policy/quota` exposes Reserve/Commit/Release/Pending contracts over caller-owned scope, units and opaque handles. The backend guarantees atomic admission, idempotency per operation/attempt/settlement identity, conflicting-finalization rejection and acknowledgement certainty. No distributed ledger is implemented by routery. TTL is not proof of zero usage. Complete actual usage, including overage, commits unchanged; proven not-executed releases; unknown/incomplete usage remains pending. Fail-open is explicitly declared before admission and marks the attempt unreserved; uncertain acknowledgement keeps its identity.

Ordering: hard eligibility + required affinity → soft preference/ranking → binding freshness → cancellation/deadline → reservation for each physical attempt → dispatch → phase/output/outcome/usage → settlement/reconciliation. Backoff holds no execution reservation. Stream concurrency reservations live until completion/close, not just until headers arrive. Observers are optional and cannot provide atomicity.

## Delivery and migration

No compatibility shims are planned. Replace lazy HTTP body mutation with PrepareRequest; attach/close owned results; remove automatic POST/PATCH retries based solely on 503. Use explicit phase, accepted-completion, outcome and replay policy; own nested retries. Pass candidate/scope/freshness data and implement the host quota backend when needed. Release with `make release-break` after all-module lint/race tests. No release, publication or issue closure is authorized by this implementation goal.
