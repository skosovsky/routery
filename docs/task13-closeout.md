# Task13 implementation closeout — accepted

Baseline: `516396ba5400ad61fdbfb5e4f4870c50d06e422d`. Working-tree implementation; no release or publication.

## Requirement evidence

Independent acceptance: **60/60 (100%)** — R 27/27, C 8/8, documentation 5/5, API registry 15/15, verification 5/5. Fixed denominator: 60 criteria. All conjunctions within a criterion must be satisfied. Both independent auditors reviewed the corrected current diff and final evidence.

| Criterion | Implementation / contract | Evidence |
| --- | --- | --- |
| R01.01 | discard.go, chain.go, routers.go, smart.go | remediation_test.go: TestCombinatorsStopOnCleanupFailure; TestFallbackCancellationRetainsCanonicalOwner; TestFallbackCleanupCancellationStopsSecondary; retry_cancellation_contract_test.go |
| R01.02 | discard.go, chain.go, routers.go, smart.go | remediation_test.go: TestCombinatorsStopOnCleanupFailure; TestFallbackCancellationRetainsCanonicalOwner; TestFallbackCleanupCancellationStopsSecondary; retry_cancellation_contract_test.go |
| R01.03 | discard.go, chain.go, routers.go, smart.go | remediation_test.go: TestCombinatorsStopOnCleanupFailure; TestFallbackCancellationRetainsCanonicalOwner; TestFallbackCleanupCancellationStopsSecondary; retry_cancellation_contract_test.go |
| R01.04 | discard.go, chain.go, routers.go, smart.go | remediation_test.go: TestCombinatorsStopOnCleanupFailure; TestFallbackCancellationRetainsCanonicalOwner; TestFallbackCleanupCancellationStopsSecondary; retry_cancellation_contract_test.go |
| R02.01 | ext/http/retry_policy.go, routers.go | ext/http/retry_policy_test.go: TestRetryPolicyVetoRetainsFinalBody; TestRetryIfClosesAllIntermediateStatusBodies; ext/http/executor_test.go: TestRetryIfWithDefaultRetryPolicyKeepsFinalBodyOpen |
| R02.02 | ext/http/retry_policy.go, routers.go | ext/http/retry_policy_test.go: TestRetryPolicyVetoRetainsFinalBody; TestRetryIfClosesAllIntermediateStatusBodies; ext/http/executor_test.go: TestRetryIfWithDefaultRetryPolicyKeepsFinalBodyOpen |
| R02.03 | ext/http/retry_policy.go, routers.go | ext/http/retry_policy_test.go: TestRetryPolicyVetoRetainsFinalBody; TestRetryIfClosesAllIntermediateStatusBodies; ext/http/executor_test.go: TestRetryIfWithDefaultRetryPolicyKeepsFinalBodyOpen |
| R03.01 | ext/mongo/executor.go | ext/mongo/executor_test.go: TestWritesRetainPartialResultsOnError |
| R03.02 | ext/mongo/executor.go | ext/mongo/executor_test.go: TestWritesRetainPartialResultsOnError |
| R03.03 | ext/mongo/executor.go | ext/mongo/executor_test.go: TestWritesRetainPartialResultsOnError |
| R04.01 | binding.go | remediation_test.go: TestBindingPathSegmentIdentity |
| R04.02 | binding.go | remediation_test.go: TestBindingPathSegmentIdentity |
| R05.01 | route_table.go | remediation_test.go: TestDecisionConfidenceValidation; route_table_test.go existing ordinary thresholds |
| R05.02 | route_table.go | remediation_test.go: TestDecisionConfidenceValidation; route_table_test.go existing ordinary thresholds |
| R05.03 | route_table.go | remediation_test.go: TestDecisionConfidenceValidation; route_table_test.go existing ordinary thresholds |
| R06.01 | route_table.go | remediation_test.go: TestMountCyclesAndSharedSubtree |
| R06.02 | route_table.go | remediation_test.go: TestMountCyclesAndSharedSubtree |
| R06.03 | route_table.go | remediation_test.go: TestMountCyclesAndSharedSubtree |
| R07.01 | policy/quota/quota.go | policy/quota/remediation_test.go: both TestSession tests; quota_test.go: TestSettlementLostAckOverageAndConflicts, TestIncompleteCancelledUsageRetainedAndResolved |
| R07.02 | policy/quota/quota.go | policy/quota/remediation_test.go: both TestSession tests; quota_test.go: TestSettlementLostAckOverageAndConflicts, TestIncompleteCancelledUsageRetainedAndResolved |
| R07.03 | policy/quota/quota.go | policy/quota/remediation_test.go: both TestSession tests; quota_test.go: TestSettlementLostAckOverageAndConflicts, TestIncompleteCancelledUsageRetainedAndResolved |
| R07.04 | policy/quota/quota.go | policy/quota/remediation_test.go: both TestSession tests; quota_test.go: TestSettlementLostAckOverageAndConflicts, TestIncompleteCancelledUsageRetainedAndResolved |
| R08.01 | policy/selection.go | policy/selection_test.go: TestAffinityRejectionExplainsPolicyStage; TestDomainRejectionHasSeparateStage |
| R08.02 | policy/selection.go | policy/selection_test.go: TestAffinityRejectionExplainsPolicyStage; TestDomainRejectionHasSeparateStage |
| R09.01 | Makefile, scripts/fuzz.sh, .github/workflows/ci.yml | scripts/test-fuzz.py: none/one/multi/list-fail/build-fail/target-fail; evidence/task13/fuzz.log |
| R09.02 | Makefile, scripts/fuzz.sh, .github/workflows/ci.yml | scripts/test-fuzz.py: none/one/multi/list-fail/build-fail/target-fail; evidence/task13/fuzz.log |
| R09.03 | Makefile, scripts/fuzz.sh, .github/workflows/ci.yml | scripts/test-fuzz.py: none/one/multi/list-fail/build-fail/target-fail; evidence/task13/fuzz.log |
| C01.01 | policy/execution/boundary.go, docs/execution-contracts.md | policy/execution/boundary_test.go: TestPartialAdmissionErrorRetainsFinishAndReconciliation; policy/quota/quota_test.go: TestBackendFailurePolicyAndCancellation (unknown ack never creates release session) |
| C01.02 | policy/execution/boundary.go, docs/execution-contracts.md | policy/execution/boundary_test.go: TestPartialAdmissionErrorRetainsFinishAndReconciliation; policy/quota/quota_test.go: TestBackendFailurePolicyAndCancellation (unknown ack never creates release session) |
| C01.03 | policy/execution/boundary.go, docs/execution-contracts.md | policy/execution/boundary_test.go: TestPartialAdmissionErrorRetainsFinishAndReconciliation; policy/quota/quota_test.go: TestBackendFailurePolicyAndCancellation (unknown ack never creates release session) |
| C01.04 | policy/execution/boundary.go, docs/execution-contracts.md | policy/execution/boundary_test.go: TestPartialAdmissionErrorRetainsFinishAndReconciliation; policy/quota/quota_test.go: TestBackendFailurePolicyAndCancellation (unknown ack never creates release session) |
| C02.01 | policy/selection.go | policy/selection_test.go: TestCallbacksReceiveEffectiveDeadline (Select and ValidatePinned, all callback types) |
| C02.02 | policy/selection.go | policy/selection_test.go: TestCallbacksReceiveEffectiveDeadline (Select and ValidatePinned, all callback types) |
| C02.03 | policy/selection.go | policy/selection_test.go: TestCallbacksReceiveEffectiveDeadline (Select and ValidatePinned, all callback types) |
| C02.04 | policy/selection.go | policy/selection_test.go: TestCallbacksReceiveEffectiveDeadline (Select and ValidatePinned, all callback types) |
| D.01 | README.md, docs/README.md, docs/migration.md, docs/history, routers.go, doc.go, policy/execution/race.go | example_test.go: ExampleRouteTable and ExampleRouteTable_callerTypes; source/Godoc inspection |
| D.02 | README.md, docs/README.md, docs/migration.md, docs/history, routers.go, doc.go, policy/execution/race.go | example_test.go: ExampleRouteTable and ExampleRouteTable_callerTypes; source/Godoc inspection |
| D.03 | README.md, docs/README.md, docs/migration.md, docs/history, routers.go, doc.go, policy/execution/race.go | example_test.go: ExampleRouteTable and ExampleRouteTable_callerTypes; source/Godoc inspection |
| D.04 | README.md, docs/README.md, docs/migration.md, docs/history, routers.go, doc.go, policy/execution/race.go | example_test.go: ExampleRouteTable and ExampleRouteTable_callerTypes; source/Godoc inspection |
| D.05 | README.md, docs/README.md, docs/migration.md, docs/history, routers.go, doc.go, policy/execution/race.go | example_test.go: ExampleRouteTable and ExampleRouteTable_callerTypes; source/Godoc inspection |
| A.01 | docs/api-choices.md, docs/migration.md | api-choices.md decision 1; see API changes and regression mapping below |
| A.02 | docs/api-choices.md, docs/migration.md | api-choices.md decision 2; see API changes and regression mapping below |
| A.03 | docs/api-choices.md, docs/migration.md | api-choices.md decision 3; see API changes and regression mapping below |
| A.04 | docs/api-choices.md, docs/migration.md | api-choices.md decision 4; see API changes and regression mapping below |
| A.05 | docs/api-choices.md, docs/migration.md | api-choices.md decision 5; see API changes and regression mapping below |
| A.06 | docs/api-choices.md, docs/migration.md | api-choices.md decision 6; see API changes and regression mapping below |
| A.07 | docs/api-choices.md, docs/migration.md | api-choices.md decision 7; see API changes and regression mapping below |
| A.08 | docs/api-choices.md, docs/migration.md | api-choices.md decision 8; see API changes and regression mapping below |
| A.09 | docs/api-choices.md, docs/migration.md | api-choices.md decision 9; see API changes and regression mapping below |
| A.10 | docs/api-choices.md, docs/migration.md | api-choices.md decision 10; see API changes and regression mapping below |
| A.11 | docs/api-choices.md, docs/migration.md | api-choices.md decision 11; see API changes and regression mapping below |
| A.12 | docs/api-choices.md, docs/migration.md | api-choices.md decision 12; see API changes and regression mapping below |
| A.13 | docs/api-choices.md, docs/migration.md | api-choices.md decision 13; see API changes and regression mapping below |
| A.14 | docs/api-choices.md, docs/migration.md | api-choices.md decision 14; see API changes and regression mapping below |
| A.15 | docs/api-choices.md, docs/migration.md | api-choices.md decision 15; see API changes and regression mapping below |
| V.01 | evidence/task13 | race.log, lint.log, fuzz.log, fuzz-tooling.log, consumer.log; task13-completeness-audit.md and task13-correctness-audit.md |
| V.02 | evidence/task13 | race.log, lint.log, fuzz.log, fuzz-tooling.log, consumer.log; task13-completeness-audit.md and task13-correctness-audit.md |
| V.03 | evidence/task13 | race.log, lint.log, fuzz.log, fuzz-tooling.log, consumer.log; task13-completeness-audit.md and task13-correctness-audit.md |
| V.04 | evidence/task13 | race.log, lint.log, fuzz.log, fuzz-tooling.log, consumer.log; task13-completeness-audit.md and task13-correctness-audit.md |
| V.05 | evidence/task13 | race.log, lint.log, fuzz.log, fuzz-tooling.log, consumer.log; task13-completeness-audit.md and task13-correctness-audit.md |

## API regression evidence

- Cleanup-complete hook registration: TestLateOnCloseWaitsForCleanupCompletion and TestLifetimeConcurrentCloseAndLateHooks.
- Half-open handler/classifier panic: TestHalfOpenPanicReleasesProbe.
- Detached observer paths: TestObserverCannotMutateCanonicalMetadata.
- Negative core configuration: TestNegativeMiddlewareConfigurationDoesNotExecute; negative unary/stream options: TestNegativeInterceptorOptionsRejectBeforeDispatch.
- Explicit empty selection: TestNoEligibleQualityAndStaleDescriptor; required affinity outcomes: existing selection tests.
- Exact S3 classification: TestIsTransientError includes lookalike codes and local error text.
- Redis BYOT invoker: adapter tests and executable ExampleNewStringRouteHandler_withRetryIf; external consumer imports the new signature.
- Handled caller types: both executable root examples. Redundant Async constructors and legacy names are removed.

## Verification commands

```sh
GOCACHE=/tmp/routery-review-gocache make test
GOCACHE=/tmp/routery-review-gocache GOLANGCI_LINT_CACHE=/tmp/routery-review-lintcache make lint
GOCACHE=/tmp/routery-review-gocache GOMAXPROCS=2 make fuzz FUZZTIME=1s
python3 scripts/test-fuzz.py
GOCACHE=/tmp/routery-review-gocache python3 scripts/consumer-smoke.py
```

The consumer script constructs unpublished v0.6.0-task13 ZIP/mod/info archives from this checkout in a local module proxy. It rewrites development root requirements to that version, removes local replaces, imports all nine modules, builds and executes a core dispatch with GOWORK=off in an isolated GOPATH/module cache. Public dependencies retain checksum verification. This verifies the corrected release layout; it is not installation evidence for published v0.5.0 or authorization to publish.

## Before/after evidence and limitations

Saved pre-fix failure logs cover HTTP/Mongo, partial admission/deadline callbacks, quota cancellation, hooks/panic/observer/config. Initial core cleanup/fingerprint/confidence tests also failed before fixing them. Mount cycles were not rerun against the unsafe baseline in-process; the source task records isolated fatal-stack reproduction, and current direct/indirect-cycle/DAG regression tests verify the correction.

Live cloud/storage deployments were not used. Adapter test doubles verify mapping/ownership, not production backend durability. Fuzz smoke is one second per target, not a sustained campaign. Backend reentry into Settle/Release and same-Lifetime recursive Close remain explicitly unsupported. Borrowed payloads/errors/resource pointers require host discipline and redaction.

## Incompatible changes

See migration.md v0.5.0 → task13: removed Async/BasicAsync, renamed FirstSuccessfulPayload and ErrBulkheadFull, changed Redis constructors/CommandInvoker, explicit ErrNoSelection, negative configuration rejection, finite confidence domain, new binding fingerprints, executor-owned HTTP cleanup and stricter cleanup-stop behavior. Admission/settlement, hook timing, explanation and effective-deadline behavior are corrected without a compatibility mode.

## Independent acceptance

- [Completeness audit](task13-completeness-audit.md): 60/60 (100%), every criterion independently mapped to evidence.
- [Correctness audit](task13-correctness-audit.md): no open confirmed defects in the checked scope.
- Review found an additional post-cleanup cancellation gap in both fallback paths. It was reproduced before correction, fixed, and independently rechecked against the final diff. The regression preserves partial payload/owner/metadata and both provider/cancellation errors; secondary never runs. Saved red/green evidence accompanies the final all-module results.
- Final parent evidence audit confirmed nine race suites, nine clean lint modules, eight passing fuzz targets, six discovery/failure-propagation fixtures, executable examples and the corrected release-layout consumer. No runtime source changes followed the auditors' final rechecks.

Reviewed source/config/contract SHA-256 (sorted path + NUL + bytes + NUL): `025b68da99c3eb053236ef8f6444c6ec2aef5f619c8cefaf7e4810a0314fe9f3`. Includes all Go sources/tests, module files, Makefile, go.work, README, CI, scripts and current routing/execution/API/migration/index docs; excludes logs, history and audit/closeout status documents.
