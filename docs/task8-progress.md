# Task 8 implementation checkpoint

Goal remains active. No release, publication or issue closure was performed.
The original specification is unchanged; all four cards and four bugs remain in scope.
Current checklist denominator is 120; completeness is not yet independently audited.

## Implemented and checked

- Contract-first design in execution-contracts.md; source-derived checklist in task8-checklist.md.
- Optional generic resource Lifetime, per-branch contexts, winner ownership transfer,
  cooperative loser cancellation and cleanup of late/discarded results.
- Generic timeout respects owned resource lifetime; routing error wrappers preserve
  partial result ownership. Cancellation blocks retry/fallback independent of delay.
- HTTP PrepareRequest replaces lazy original request mutation. Fan-out sends independent
  complete bodies; unprepared requests and failed preparation reject before transport.
- Default POST/PATCH 503 retry disabled. Explicit RetryPolicy requires caller-provided
  proven-not-executed or verified-deduplication evidence plus replayable bodies.
- policy selection: hard eligibility before ranking, frozen descriptors, typed
  explanations, canonical bindings and explicit freshness checks before dispatch.
- Required/preferred affinity scope, expiry and caller compatibility gates.
- policy/model capability, context, data policy and mandatory task-quality gates;
  measured cost/performance metadata and declared handling of optional estimates.
- policy/attempt synchronized identity/budget/phase/commit/outcome state, repeated
  events, late reconciliation, safe replay decisions, not-before/deadline scheduling
  and scope-isolated cooldowns.
- policy/quota host Backend port and synchronized per-handle Session, lost-ack
  retries, actual overage, pending reconciliation, explicit no-usage release,
  conflicting-finalization guards and classified fail-open/closed behavior.
- Query rows, document cursors and object download bodies now attach resource
  Lifetime. Regression fixtures cover readable race winners and exactly-once close;
  SQL executable examples close the lifetime rather than only the raw rows.
- Bulkhead holds local concurrency permits until owned results close, including
  partial error results; pre-cancelled calls never invoke the underlying handler.
- policy/execution Boundary composes one physical attempt: identity/budget, explicit
  Receipt metadata, freshness before and after admission, cancellation, typed
  denial/defer and resource-bound settlement. Settlement errors stay inspectable;
  explicit reconciliation and late definitive events retain the same identity.
- Execution/quota integration fixture retains the live reservation after headers and
  unknown stream close; late verified usage commits raw overage without double charge.
- execution.Sequence composes Boundary with caller failure classes, replay evidence,
  Decide/Schedule/Wait and explicit request/binding construction after waiting. It
  preserves physical traces and partial results; hidden nested-attempt visibility is
  explicit. Every repeat gets a new identity and its own freshness/admission checks.
- Sequence closes intermediate resources before backoff; it rechecks commit facts,
  remaining budget, cancellation and deadline before a repeat. Settlement errors and
  uncertain reserve acknowledgements cannot trigger a new reservation automatically.
- execution.Race composes a bounded worker pool of Boundary calls with FirstCompleted,
  duplicate-cost/replay/effect permissions, terminal acceptance and separate EarlyOwned
  profile. A synchronized journal retains rejected/late results and Receipts. Winner
  lifetime transfers; rejected/late loser resources close without inferring zero cost.
- HTTP RetryAfterHint normalizes decimal seconds and HTTP dates with caller receipt
  time, source/clock/uncertainty and explicit invalid-hint handling. Raw provider
  reason phrases are no longer published through StatusError.Error.
- Scheduling validates UTC calendar range, duration/calendar overflow and uncertainty;
  Wait checks its clock and cannot treat a capped timer as reaching a later not-before.
- Stream interceptor review confirms an opening-only boundary, caller-owned RPC
  context and no receive-side restart. A regression fixture proves that CloseSend and
  established-stream Unavailable do not create another RPC or cancel receive lifetime.

## Verification evidence

- Current hints/stream checkpoint: make lint and make test passed for every module
  with isolated TMPDIR and GOCACHE. One earlier test invocation failed because shared
  Go cache files for standard packages were missing; the isolated-cache rerun passed
  without disabling vet, race or any lint rule. Verification cache for subsequent runs:
  /private/tmp/routery-verification.37dM1A/go-cache (temp lock parent is the same directory).
  HTTP controlled-dispatch integration confirms one physical request and typed defer
  when Retry-After exceeds deadline. Fixtures cover malformed/duplicate/overflow hints,
  past timestamps, uncertainty, scope isolation and bounded provider diagnostics.
- Current safe-race checkpoint: make lint passed for all modules with an isolated
  temporary directory (golangci-lint's global temp lock was held by other projects;
  no rules/configuration were weakened). make test passed for all modules with race.
  Barrier fixtures cover invalid-first/accepted-second, resource concurrency limit,
  independent quota sessions, late loser cleanup, pending usage and measured overage,
  duplicate permissions, denied admission, attempt budget, duplicate IDs, cancellation,
  committed output during validation and stream-handle rejection by CompleteOnly.
- Current sequence checkpoint: make lint and make test passed for all modules on
  the delivered code. Controlled-clock regression
  fixtures cover hint/uncertainty precedence, per-attempt fallback admission,
  committed/buffered partials, unsafe replay, reconcile/defer, cancellation with
  zero/nonzero backoff, early/late Wait, late commit and backend settlement failures.
- Current execution-boundary checkpoint: make lint and make test passed for every
  module, including the new execution package and quota integration fixture. Cases
  cover denial/defer, cancellation after reserve, stale binding after reserve,
  physical identity/budget, local unknown outcome and lost settlement acknowledgement.
- Current resource-ownership checkpoint: make lint and make test passed for every
  module after SQL example migration. New race fixtures include query winner rows,
  cursor ownership, object winner body and Bulkhead partial-result permit retention.
- make lint: all root/extension modules passed after latest code changes.
- make test: all modules passed with race detector; final whole-goal gate must be
  rerun after remaining implementation and audits.
- After later cancellation/formatting changes, go test -race -timeout 60s ./...
  ./ext/http/... passed on the current state.
- Regression tests cover BUG-001 winner response reading/loser cancellation/late
  cleanup; BUG-002 prepared fan-out/body independence; BUG-003 default single write
  and explicit deduplicated repeat; BUG-004 zero/nonzero-delay cancellation.
- One newly written late-result test initially hung because its loser had not
  entered before winner cancellation. Fixed with an explicit loser-start barrier;
  identified stopped test processes were terminated. Current regression suite passes.

## Temporal, examples and rebuild checkpoint

Temporal dispatch checkpoint: `Selection.ValidateFreshness` is explicitly epochs-only.
`Dispatch` now requires a validator; `Selector.ValidatePinned` rechecks the current
clock, hard eligibility, measurements, pinned identity and original affinity stamp.
`policy/temporal_test.go` covers required expiry, weakening, scope/fingerprint
substitution, changed compatibility and cancellation inside validation.
`policy/model/temporal_test.go` proves descriptor/quality TTL expiry with unchanged
references and zero provider calls. The policy suite passed with the race detector;
`make lint` and `make test` passed for all root/extension modules after this code
checkpoint, using isolated Go/lint caches. This is not the final post-audit gate.
`docs/migration.md` now covers all RTR cards, correctness breaks, substantial
release guidance and the author-facing closeout requirements.

`policy/execution/example_test.go` supplies executable model/ordinary-operation
examples with unrelated request/descriptor types, no SDK, storage or telemetry.
`rebuild_test.go` supplies a controlled host transformation with explicit permission,
trusted scope, re-eligibility, new input reference, parent lineage, unique physical
identity and separate quota settlement. Unauthorized/foreign-scope cases transform
nothing; hard-ineligible rebuilt targets do not dispatch. The fake backend reports
five units for each explicitly completed attempt, ten total after successful rebuild.
This is a host composition fixture, not a history reconstruction API in routery.

The invalid-result race test exposed an insufficient barrier: raw resource cleanup
signalled before settlement hooks finished. The test now waits on settlement itself;
100 race-detector repetitions of that test and both examples passed. Full-module
`make lint` and `make test` passed after these changes. Independent audits remain due.

## Next required work (not deferred)

Audit remediation has begun; initial complete statuses were downgraded where real
evidence was missing or code contradicted the contract. Completeness auditor added
final handoff requirement T8-C01, so the denominator is now 121, not reduced.
Confirmed ownership leaks in Router fallthrough, invalid-result validation and
projection errors, and Race queue continuation after invalid lifecycle remain open.
The HTTP Timeout legacy ownership fallback also needs clear-break reconciliation.

New fixes/proofs: invalid SelectionStatus first reproduced nil error, then Dispatch
was changed to ErrInvalidSelection. `execution/pre_dispatch_test.go` explicitly
composes host-classified preflight failure, NotExecuted proof, Decide/Schedule/Wait,
fresh request/new physical identity and one external dispatch with budget exhausted.
Live-winner parent cancellation is now asserted BEFORE closing the lifetime; cleanup
then happens once. Quota fixture directly denies second admission while the first
returned stream remains open/Reserved. Current root race suite and root lint passed.
Reader ownership counters and public quota/affinity executable examples remain due.

Independent-audit handoff checkpoint: all implementation rows now have an initial
evidence assessment in the checklist. `admission_failure_test.go` verifies definitive
fail-open is explicitly Unreserved, fail-closed/unknown acknowledgement never
dispatch, and physical identity survives. `lifetime_test.go` covers every failed
FirstCompleted branch owning a resource, exactly-once cleanup and cancelled contexts.
`docs/task8-closeout.md` now supplies applicability/evidence matrix and an unpublished
author-facing migration draft covering all cards and bugs. Current full-module
make lint and make test passed. No release or issue write was performed.

Two independent auditors have been started: completeness (including checklist
omissions and denominator) and correctness (including core/policy/extension
composition). They must inspect original source and current files independently;
complete statuses here are not trusted evidence by themselves. Remaining audit
rows T8-A07/A08/A09 and final post-audit gate T8-A10 remain pending. The older next-work
items below describe areas the audits must explicitly challenge, not silent deferrals.

Checklist reconciliation checkpoint: architectural/selection rows T8-001–026 now
have inspected code/test/documentation evidence. T8-012/014/025 gained the dedicated
fallback fixture described below; no requirement was dropped to raise completeness.
`quota/adversarial_test.go` adds competing independent-session finalization,
expired-handle complete/incomplete/release errors without a refund, and conflicting
duplicate reserve facts. The quota suite passed 100 race-detector repetitions.
`selection_test.go` now asserts stable safe explanations; `snapshot.go` explicitly
states that generic storage does not freeze maps/slices/pointer-referenced state.
The policy suite passed with race after these changes. Independent audits have
not started; remaining checklist rows are not assumed complete from green gates.
Full-module lint passed after adapting the safe-explanation assertion to normal
formatting (without adding transport serialization tags to generic public types).
Full-module tests passed before that test-only assertion change, and the current
policy suite passed again with race afterward.

Fallback/lifecycle checkpoint: `selection_fallback_test.go` composes model selection,
explicit Next reselect and Boundary.ValidatePinned. Both capability and residency
cases exclude favorable/cache-preferred B before ranking and dispatch eligible C
under the original hard policy with a new physical ID. `attempt/transitions_test.go`
checks invalid/regressive phase/outcome/identity metadata without state/budget mutation.
The new Sequence malformed-lifecycle regression reproduced two dispatches and nil
error under permissive replay: invalid event was incorrectly treated as provider
failure. The contract now declares ErrInvalidEvent/ErrInvalidBoundary terminal control
errors; Run returns them without classification/replay. Regression now proves one
dispatch and retained error. Policy tests passed 20 race-detector repetitions; all
module `make lint` and `make test` passed after the fix. Rows T8-027–056 gained evidence.
Remaining quota/affinity/general-delivery rows and both independent audits are due.

Reset/nested/constraints checkpoint: `sequence_reset_test.go` now covers committed
unknown failure with no reset, reset without safe-duplicate evidence, and host
reset plus explicit evidence. Replacement has a new physical identity and does
not clear the original committed/unknown facts. A nested Boundary fixture consumes
the same Coordinator budget and reserves/settles its own quota; the outer unknown
remains pending. Hidden nested attempts retain `NestedAttemptsKnown=false`.
`model/constraints_test.go` checks window, residency, retention, low/stale quality
before ranking, and distinct invalid-policy/floor/descriptor/estimate errors.
The complete policy suite passed ten race-detector repetitions after these additions.
Full-module `make lint` and `make test` also passed on this checkpoint. Checklist
rows T8-018/024/032/035 and T8-A12 now carry concrete evidence; this is an initial
implementation assessment, not either independent audit or final completeness claim.

1. Reconcile remaining end-to-end requirements of Sequence against the source spec.
   Reset, nested-budget consumption, host rebuild and both basic examples now have fixtures.
   Retry/fallback, failure classification, replay, scheduling and Boundary accounting
   are implemented; no claim of complete task coverage has been made.
2. Extend adversarial safe-race coverage and verify its complete contract during audits.
   Acceptance, permissions, concurrency/budget, per-branch admission/settlement and
   resource ownership are implemented, not yet independently audited.
3. Check remaining rebuild/reset composition scenarios against the full source spec.
4. Validate remaining adversarial lifecycle scenarios during the independent audits;
   resource extensions and the opening-only stream-interceptor boundary were reviewed.
5. Complete migration/closeout drafts and remaining acceptance matrices, including
   remaining freshness/affinity matrices beyond the temporal checkpoint above.
   HTTP hint normalization and controlled scheduling integration are implemented.
6. Validate example coverage for each delivered card during checklist reconciliation.
7. Fill checklist evidence row-by-row, then launch two separate independent auditors:
   completeness against original spec AND checklist, and correctness of code/composition.
   Fix confirmed findings, re-audit, rerun make lint and make test on delivered state.

No 100% claim is justified yet. Persistent backend guarantees are host-owned;
the atomic backend in quota_test.go is a conformance fake, not a production ledger.

Ownership audit remediation checkpoint: CA-001 Router discarded Next cleanup and
CA-002 validation-error partial-result preservation now have permanent AAA matrices
in ownership_boundaries_test.go; root/policy race tests and root lint passed.
CA-004 projection now explicitly returns the canonical owner through
ProjectionMeta.Lifetime on success, nil/failed projector, dispatch errors and
error mapping. projection_ownership_test.go verifies caller-owned exactly-once
cleanup, retained cleanup errors and no premature close. Independent re-audit is
still required; these changes do not close CA-003 race control-error handling or
the remaining example/body-reader coverage and HTTP legacy Timeout findings.
After the projection change, root/policy `go test -race ./...` passed; the three
ownership regression matrices passed ten race repetitions and root lint reported
zero issues. Full-module gates on this changed public contract were started and
are not yet recorded as passed at this checkpoint.

Projection checkpoint full-module gates subsequently passed: `make lint` reported
zero issues for root and every extension; `make test` passed root/observability/
policy and every extension under race detection. These gates preceded the next
HTTP Timeout edit and do not prove the final delivered state.

HTTP Timeout remediation: removed the body-inference fallback and cancelTimerBody;
Timeout delegates to generic routery.Timeout under a single explicit Lifetime
contract. Custom resource fixtures now attach ownership instead of relying on
legacy inference. New AAA tests cover no ownership inference and preservation of
owned partial/error payload, live context until close and retained cleanup error;
the body/lifetime fixture verifies shared exactly-once hooks. Timeout/ownership
tests passed ten race repetitions. Migration guidance states the caller change;
the independent audit still needs to confirm this removal and the other fixes.
On the final HTTP Timeout checkpoint, HTTP-module lint reported zero issues and
the complete HTTP suite passed `go test -race -count=1 -timeout 60s ./...`.

CA-003 remediation checkpoint: a permanent single-worker regression reproduced
two dispatches after InvalidEvent/InvalidBoundary (including wrapped/joined errors).
Race now serializes queued-plan authorization with observation of fatal control
errors through a shared gate. Failure is recorded before cleanup, so a blocked
resource cleanup does not authorize more queued attempts. Run checks the shared
failure before accepting a concurrent winner and releases a discarded winner's
owner; already-started attempts remain journaled, including late results.
New barrier-based regressions cover a concurrent accepted winner during blocked
fatal cleanup and two active workers with two queued plans. Initial fixture lacked
a barrier ensuring the valid branch had started and timed out; the barrier was
added rather than weakening the assertion. Repetition exposed an Abort-with-nil-
error path on another worker observing the shared failure; it now returns that
failure explicitly. Final repeated verification and independent re-audit are due.
The three CA-003 regressions passed 100 race-detector repetitions on the corrected
implementation. This is local regression evidence, not independent acceptance.

BUG-002 request-reader coverage: request_ownership_test.go now runs both a body
without GetBody and a pre-existing GetBody factory through four concurrent
branches. It counts bytes consumed from the original stream before and after
fanout (one complete consumption for preparation, zero for an existing factory),
checks original close count and unchanged request fields, and tracks every
independent attempt reader's complete byte consumption and exactly-one close.
Transport barriers ensure all request bodies finish before any winner returns;
response close counters are not used as evidence for request ownership.
Both cases passed 100 race-detector repetitions. Independent re-audit remains due.
The full HTTP race suite and HTTP-module lint also passed on this fixture checkpoint.

RTR-003 public example: quota_example_test.go is in external execution_test and
uses only exported ports/contracts. It demonstrates pre-dispatch quota.Admit,
stream ownership, unknown close into Pending, late Receipt.Record reconciliation,
stable settlement identity, seven measured units against estimate two and duplicate
terminal evidence without double settlement. Host callbacks own actual usage and
bounded cleanup contexts. Its single-reservation backend validates scope/identity,
reservation repeats, conflicting finalization and complete usage; it is explicitly
a local fixture, not durable infrastructure. Root lint passed; executable Output
verification and repetition are being checked before independent re-audit.
The public quota example's checked Output passed 100 race-detector repetitions.

RTR-004 public example: affinity_example_test.go uses only exported API from
external execution_test. It retains initial usage, proves required-unavailable and
missing authorization add no dispatch, explicitly reconstructs caller state in a
host callback, updates state/input references, re-runs eligibility/selection and
ValidatePinned, then uses a new physical identity with host lineage and separate
usage. Checked Output passed 100 race-detector repetitions. Both missing public
examples are now present; final gates and independent re-audit remain mandatory.
On this checkpoint (both public examples and all CA remediation), full-module
`make lint` reported zero issues for root and every extension; `make test` passed
root, observability, all policy packages and all eight extension modules under
race detection. Both original independent auditors have been dispatched for a
fresh review without reading each other's report. Their final conclusions and
handoff remain pending; the successful gates do not establish 100% completeness.

Repeat audit found CA-003 still open: a deterministic parent-context barrier in
checkWinner let a started loser report InvalidEvent after the first fatal check;
Run then returned accepted/nil. The independent reproduction and permanent
TestRaceFatalDuringWinnerValidationPreventsAcceptance both failed on that snapshot.
Race now finalizes acceptance under the same shared gate lock after checkWinner.
Fatal observation and finalization have an explicit ordering; errors observed
before finalization reject/close the winner, later errors remain journal evidence.
Finalization also stops queue authorization. This clarifies the contract's
linearization point instead of promising retroactive error changes until literal
function return. Fresh regression repetition/root lint and re-audit are required.
The auditor additionally confirmed CA-005/P2: Boundary Dispatch bypasses canonical
route-action validation; invalid callbacks can return Abort/unknown action with
nil error. Boundary/Sequence/Race validation regression and remediation remain due.
On the atomic-finalization checkpoint, all four fatal/control race regressions
passed 100 race-detector repetitions and root lint reported zero issues.
The completeness auditor's second checkpoint is 114/121 (94.21%), implementation
113/116 (97.41%); original reader/example gaps are independently closed, but the
new Boundary/Race findings and final verification/handoff deliverables remain.
Both auditors independently checked the delivered checkpoint without reading the
other report. No final 100% or defect-free claim is justified yet.

CA-005 remediation: route_validation_test.go reproduced Abort/unknown action with
nil error through Boundary, Sequence and Race; the original Race dispatched both
plans. Core now exposes ValidateRouteResult as the shared canonical action/error
boundary, and InvokeRouteHandler and execution Boundary both use it. No callback
is reinvoked and original call metadata, partial payload/Lifetime and receipt are
preserved. Sequence/Race treat ErrInvalidConfig as a fatal control error, alongside
InvalidEvent/InvalidBoundary. The six execution regression cases plus fatal Race
matrix passed 100 race-detector repetitions; initial root/policy suite and lint
passed before the additional direct exported-validator matrix. That matrix and
fresh all-module gates still need verification, then both auditors re-check fixes.
The direct exported-validator matrix passed 100 race repetitions. After test-only
helper extraction to satisfy strict lint complexity, full-module `make lint` and
`make test` passed root/observability/policy plus all eight extensions; diff check
is clean. README and closeout prose now include the shared validation caller change.
Both auditors are independently reviewing CA-003 finalization and CA-005 again.
These are checkpoint gates, not final acceptance before their verdicts.

The third correctness review confirmed CA-001–005 resolved but found CA-006/P1:
Logging, Metrics and the tracing extension still replaced error results with empty
Abort values, dropping owned partial payload/Lifetime. The independent repro was
run locally before the fix: all three reported owned=false, empty payload and
cleanup0. A permanent Logging/Metrics matrix reproduced typed-provider-error,
Abort-without-error and unknown-action failures. All three middleware now reuse
ValidateRouteResult, preserve original match/partial/owner, and report canonical
errors instead of inventing an empty result. Logging/Metrics matrix passed100×race;
the original repro now reports owned=true/full partial/cleanup1 for all three.
Tracing has its own permanent SDK-span fixture; its verification and composition
regressions for Bulkhead/FirstCompleted/Boundary remain required before re-audit.
Completeness auditor's116/116 substantive assessment and checkpoint gates preceded
this new confirmed finding; they are not final delivered-state acceptance.
Tracing's permanent ownership/action matrix passed100×race; tracing-module lint
required repair of an unstable formatter line split. Root lint passed. Composition tests
and independent re-audit remain pending for CA-006.

CA-006 composition evidence: Logging/Metrics ownership_composition_test.go runs
an observed provider error through Bulkhead inside execution Boundary. Before
owner close, a probe is denied and resource/settlement counters remain zero;
close releases exactly one resource and finalizes exactly one terminal/Unknown
receipt. A second physical attempt through FirstCompleted discards/closes its
owned error, releases the permit and independently settles its reservation; a
subsequent probe succeeds, without duplicate cleanup/settlement. Both observers'
direct/malformed-result and composition tests passed100×race and root lint passed.
The tracing extension has separate Boundary/Bulkhead/FirstCompleted fixtures,
including retained unknown receipts, resource hooks and released permits; its
100×race and lint verification are being checked before both independent audits.
Tracing's direct/malformed-result and composition suites passed100×race; tracing
lint reported zero issues and git diff --check is clean. Fresh all-module gates
and both independent re-audits were started on this CA-006 checkpoint.
Main full-module gates on this checkpoint completed successfully: root and all
eight extensions lint0issues; root/observability/policy and all extensions tests
pass under race detection. No source edits followed these gates. Both auditors'
fresh independent verdicts are pending; goal acceptance remains unproven.

## T8-009 tracing privacy correction

The completeness auditor identified automatic raw error, generic Kind/Reason and
request-derived Match/span-name publication. A synthetic-secret regression failed
on both success and failure before the fix. Contract-first clear break now requires
the third AttributeProjection argument (nil for safe defaults). Default output is
only canonical action, fixed default name and bounded failure status, with no raw
error events. Explicit host projections exclude request/payload/ownership and may
not override reserved action. Unsafe automatic helpers were removed. Migration
and closeout describe the caller change. Tracing module all tests passed100×race;
fresh strict lint/all-module gates and independent audits remain required.

## Final source verification and verdict integration

Privacy regression additionally covers caller Match-derived span names and opaque
decision metadata. Final all-module gates sessions76405/11316 both exited0:
root plus8extensions lint0issues and tests pass with race detection. Both independent
auditors passed entire tracing suite100×race; substantive completeness116/116 and
correctness CA-001–006 resolved/open0. Main checklist integrates A08/A09/A10.
`task8-handoff.md` records evidence/limitations: pre-delivery120/121, C01 only
completed by the actual final response. No conditional card is deferred and no
release/publication/issue closure has occurred.
