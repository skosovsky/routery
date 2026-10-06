# Task 13 acceptance contract

Baseline: `516396ba5400ad61fdbfb5e4f4870c50d06e422d`. No release/publication.

Each criterion below is conjunctive: partial coverage does not close it. Denominator is fixed before implementation. Tests use Arrange–Act–Assert. Evidence and final review status are recorded in task13-closeout.md.
Fixed denominator: **60 criteria**. Report R, C, documentation, architecture and verification separately.

## R01

- [x] **R01.01** Chain cleanup failure stops the next handler and retains metadata/owner.
- [x] **R01.02** Fallback cleanup failure stops secondary and joins original and cleanup errors.
- [x] **R01.03** RetryIf cleanup failure stops retry and joins errors.
- [x] **R01.04** PredicateFallback cleanup failure stops secondary; cancellation retains errors/owner.

## R02

- [x] **R02.01** Both HTTP predicates have no body-close side effects, including a subsequent veto.
- [x] **R02.02** Executor closes intermediate response exactly once on actual retry.
- [x] **R02.03** Cancellation and final failure retain the final response owner.

## R03

- [x] **R03.01** InsertOne retains nonnil result plus original error; nil-result error aborts.
- [x] **R03.02** UpdateOne retains nonnil result plus original error; nil-result error aborts.
- [x] **R03.03** DeleteOne retains nonnil result plus original error; nil-result error aborts.

## R04

- [x] **R04.01** Slash-containing distinct segment paths have distinct binding fingerprints.
- [x] **R04.02** Identical paths reproduce fingerprints; segment count/boundaries are framed.

## R05

- [x] **R05.01** Build rejects NaN, infinities and thresholds outside [0,1] with ErrInvalidConfig.
- [x] **R05.02** Dispatch rejects malformed classifier confidence with a typed error before executing.
- [x] **R05.03** Confidence 0/1 and ordinary values preserve threshold semantics.

## R06

- [x] **R06.01** Direct Mount cycle returns ErrInvalidConfig with cycle path.
- [x] **R06.02** Indirect Mount cycle returns ErrInvalidConfig.
- [x] **R06.03** Shared acyclic subtree builds and dispatches normally.

## R07

- [x] **R07.01** Cancelled Settle/Release waiter returns without waiting for active backend I/O.
- [x] **R07.02** State is accessible during I/O, including synchronous backend State callback.
- [x] **R07.03** Concurrent settlement remains serialized and cannot double commit.
- [x] **R07.04** Stable settlement IDs, unknown acknowledgements and reconciliation remain valid.

## R08

- [x] **R08.01** Domain ineligibility and affinity rejection have separate dispositions.
- [x] **R08.02** Caller Reason is retained without conversion from library enum.

## R09

- [x] **R09.01** Each fuzz target runs with an anchored selector, including multiple targets in HTTP/SQL.
- [x] **R09.02** Package without fuzz targets is skipped successfully.
- [x] **R09.03** List/build/target failures propagate; configurable short smoke is available.

## C01

- [x] **C01.01** Admission Finish survives partial admission + error; no dispatch starts.
- [x] **C01.02** Finish receives NotExecuted and errors are joined.
- [x] **C01.03** Failed finish remains reconcilable through Receipt.
- [x] **C01.04** Unknown reserve acknowledgement is not automatically refunded.

## C02

- [x] **C02.01** Zero explicit deadline adopts context deadline.
- [x] **C02.02** Earlier context deadline reaches Validate/Eligible/Rank.
- [x] **C02.03** Earlier explicit deadline remains effective.
- [x] **C02.04** ValidatePinned uses identical normalization.

## D

- [x] **D.01** Journal Godoc describes raw borrowed payload/error/resource data and host redaction.
- [x] **D.02** README has install/module map/Go requirement/version-layout guidance and runnable root examples with two caller types.
- [x] **D.03** RetryIf documents total attempts, final predicate behavior and invalid/default config rules.
- [x] **D.04** Obsolete Stop/Ignore helper references are corrected.
- [x] **D.05** Docs index and versioned migration exist; task8–12 history is preserved under docs/history.

## A

- [x] **A.01** KEEP sealed Router: document compiled implementation and extension limits; no interface redesign required.
- [x] **A.02** CHANGE remove redundant Async constructor; caller Kind carries async meaning.
- [x] **A.03** CHANGE FirstCompleted to FirstSuccessfulPayload, documenting stream acceptance versus Race CompleteOnly.
- [x] **A.04** KEEP three fallback mechanisms; provide trigger/replay/ownership selection table, explicit predicate recommended.
- [x] **A.05** CHANGE OnClose to cleanup-complete registration semantics with concurrent regression coverage.
- [x] **A.06** CHANGE breaker panic bookkeeping releases probe and re-panics, including classifier panic.
- [x] **A.07** CHANGE detach observer metadata; document borrowed read-only payload/Lifetime and host safe projections.
- [x] **A.08** KEEP whole-snapshot strict model freshness, document intentional rejection of stale unrelated candidates.
- [x] **A.09** CHANGE policy.Dispatch requires Selected, returning typed ErrNoSelection for expected absence.
- [x] **A.10** KEEP exact affinity Key/Scope pin; document Compatible as additional compatibility only.
- [x] **A.11** CHANGE Redis callback to CommandInvoker; remove unused client parameter.
- [x] **A.12** CHANGE reject negative RetryIf/Timeout/gRPC configuration; document intentional zero defaults.
- [x] **A.13** CHANGE ErrTooManyRequests to ErrBulkheadFull with migration.
- [x] **A.14** CHANGE S3 transient classification uses exact Smithy codes/status/network errors, no string heuristics.
- [x] **A.15** CHANGE model error prefix to routery/policy/model.

## V

- [x] **V.01** All nine module race suites pass.
- [x] **V.02** All nine module lint checks pass.
- [x] **V.03** Every fuzz target passes short smoke.
- [x] **V.04** Executable examples pass and external consumer builds with GOWORK=off against corrected release-layout modules.
- [x] **V.05** Two independent final subagents report 100% completeness and no open confirmed defects against current diff.

