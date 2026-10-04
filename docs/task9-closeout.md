# Task 09: execution safety acceptance

Status: accepted by both independent reviewers; final gates passed. Scope: `.cursor/task/task9-execution.md`, based on `b9a4777`. This document records the accepted scope; it does not authorize release or publication.

## Implementation

- `Coordinator.AuthorizeRepeat` atomically compares prior facts, observes cancellation and marks an already allocated next identity Dispatched. A changed event forces evidence refresh outside locks; denied authorization retains identity and consumed budget.
- Sequence refreshes Replay after Next/Fresh/admission and serializes final authorization with previous Receipt settlement. Retry/Fallback cannot change after the request for that action has been prepared.
- The preceding partial result and failure remain final when a prepared repeat cannot start. Trace retains the separate NotExecuted attempt and its settlement.
- Sequence binds child contexts to the earliest operation/context/scheduling deadline. Synthetic clocks supply an explicit DeadlineContext. Values release contexts at return; owned partial/success results transfer cancellation to Lifetime.
- Receipt serializes settlement without holding its mutex while executing host cleanup/Finish. Final authorization waits for settlement with cancellation and rechecks time after waiting; Coordinator checks cancellation under its own lock.
- Public contracts and migration explain the breaking Replay callback semantics, linearization point, clock domain and lifetime. No compatibility path or mandatory SDK/storage/observer dependency was added.

## Independent acceptance

Two separate reviewers started without implementation conversation context and did not change implementation files.

| Reviewer | Scope | Final result |
| --- | --- | --- |
| `/root/task9_completeness` | All requirements, code/test evidence and eight acceptance criteria | 8/8, 100%; no partial/missing criteria |
| `/root/task9_correctness` | Errors, concurrency, ownership, deadlines, replay, adjacent regressions | No open confirmed findings after corrections |

Both independently identified authorization after cancellation during a settlement wait. The defect was fixed and covered by a regression. Completeness initially reported 87.5% due to missing concurrent Receipt.Record integration; the added before/after fixture closed that gap. No requirement was removed or weakened.

| Criterion | Executable evidence |
| --- | --- |
| Late commits across preparation | `TestSequenceCommitAcrossRepeatPreparation`, including Wait, Next, both Fresh checks, Admit and evidence evaluation |
| Reset and independent safe replay | Phase matrix plus `TestSequenceConsumerResetDoesNotReplaceReplayEvidence` and `TestSequenceRefreshesReplayAfterAdmission` |
| Concurrent Record before/after authorization | `TestSequenceConcurrentRecordAroundAuthorization`: dispatch counts, late unknown/committed facts, reconciliation and trace |
| Deadline across initial/repeat preparation | `TestSequenceDeadlineAcrossPreparation`, `TestSequenceDeadlineCrossedDuringAdmissionBlocksDispatch`, `TestAuthorizationRechecksAfterSettlementWait` |
| Non-extended deadlines and cancellation | `TestSequenceCannotExtendContextDeadline`, `TestSequenceRejectsExtendingDeadlineFactory`, existing zero-delay/backoff and Schedule deadline tests |
| Owned context and bounded cleanup | `TestSequenceOwnedContextRetainedUntilCloseOrDeadline`, `TestReceiptSettlementCallbacksRunOutsideMutex`, existing Boundary ownership tests |
| Concurrent budget, unique identities, retained partial result | `TestConcurrentRepeatAllocationsRespectBudgetAndIdentity`, late commit/deadline matrices |
| Adjacent Race/quota/ownership contracts | Race tests and complete `policy/attempt`, `policy/execution`, `policy/quota` race suites |

The original two regressions were executed before the implementation and failed with two dispatches after late commit, and one dispatch after admission crossed the deadline. The completeness reviewer independently reproduced both against an archived original HEAD.

## Verification

- Targeted `go test -race ./policy/attempt ./policy/execution ./policy/quota -count=1`: exit 0.
- Final `make lint` across all nine modules: exit 0, each module reported 0 issues.
- Final `make test` across all nine modules: exit 0, all module race suites passed.
- `git diff --check`: passed before final acceptance.

Go and lint caches are isolated under `/tmp` because the sandbox cannot write the normal Go cache. Lint uses the same installed binary, configuration and checks with `--allow-parallel-runners` to avoid a global runner lock conflict; the operational flag does not disable a check. Earlier failed lint runs and the corrected formatting/complexity findings are not counted as successful gates.

Synthetic fixtures establish local clock/authorization contracts; they do not establish remote exactly-once, rollback or production storage durability. Review found no remaining confirmed defects in the reviewed scope, which is not a proof of absolute absence of bugs.
