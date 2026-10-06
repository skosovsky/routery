# Task 12 acceptance — contract conformance

Independent readonly reviewers accepted the final tree: task12_completeness — 8/8
criteria, 100%; task12_correctness — no open confirmed defects within the reviewed
scope. Both inspected the closeout and final gates. This is a scoped review, not proof
of absence of every defect or production readiness.

| Criterion | Evidence |
| --- | --- |
| Public BYOT suite | External quotatest_test package uses host tenant/resource/ticket/refusal types; runtime packages do not import testing support |
| Correct and defective backends | TestHostBackendConformance passes nine scenarios; TestBrokenIndependentClientFinalizationIsDetected deterministically rejects independently cached Commit/Release; clients use shared ledger arbitration |
| Uncertainty and accounting | Lost reserve/commit ack, restart recovery, Pending/expiration, overage4 on estimate1, identity/settlement conflicts and Available/Retained assertions; TestUnknownAcknowledgmentRefundIsDetected rejects refunds in all three ack scenarios |
| Executable composition | Independent model value and ordinary resource lifecycle examples; Sequence reconciliation, fresh pinned facts, explicit outcomes, bounded cleanup and accounted/unobservable nested attempts |
| Ownership | Resource example and TestSafeObserversDoNotOwnPartialCleanupOrPermit preserve partial+error, canonical Close exactly once and permit until application Close; Receipt errors are handled separately |
| Observation | OTel lifecycle/correlation allowlist and secret projection tests; TestLifecycleRetainsFirstSettlementFailure preserves failure across late success; Race late Journal example preserves loser report without treating Snapshot as a barrier |
| Documentation | README disposition/entry point/projection cleanup, quota/execution/observability API comments, migration and conformance contracts describe the final clear break and host responsibilities |
| Gates and limitations | make lint and make test pass all nine modules; targeted race and repeated concurrency/fault tests pass; limitations below; no publication |

Correctness review reproduced two P2 issues: quota refunds on unknown ack could pass
the suite, and a late successful settlement could erase an earlier failure in the
OTel example. Both fixed and independently reproduced again: defective fixtures are
rejected and the first failure is retained. Regression tests cover both findings.
Completeness review also found missing partial cleanup in existing documentation and
an older resource example; canonical Close and receipt error handling are now explicit.

Final gates, all passed:

- make lint: nine modules, zero issues (/tmp/routery-task12-lint.log).
- make test: nine modules, Go tests/examples under -race (/tmp/routery-task12-test.log).
- Target go test -race: quota/quotatest, execution, observability and ext/otel
  (/tmp/routery-task12-target-race.log, /tmp/routery-task12-otel.log).
- Repeated -race -count=30: unknown-ack refund detection, concurrent finalization
  defect detection and late Journal example (/tmp/routery-task12-stress.log).

The reference backend is an in-process test fixture. Restart reconstructs a separate
ledger from copied state and creates fresh clients; it does not prove disk/process
crash durability. Host inspection and context cooperation are fixture preconditions.
Unsupported optional fault injection is reported explicitly and never counted as
Passed/Complete. Production hosts must run the same suite against real storage and
acknowledgment fault injection. No exactly-once remote effect is promised. Core stays
BYOT; no storage SDK, provider schema, agent loop or durable execution engine added.
No push or release performed.
