# Task 10 acceptance — adapter replay

Independent reviewers, neither involved in implementation:

- task10_completeness: 8/8 criteria, 100%, final freeze review.
- task10_correctness: no open confirmed defects, final freeze review. The discovered
  existing S3 partial-output leak was fixed and regression-tested; Mongo partial
  cursors now retain their owner too. This is not a proof of absence of all defects.

| Criterion | Evidence |
| --- | --- |
| Lost response / dedup | LostResponseReplayEvidence fixtures in grpc/mongo/redis/kafka |
| Read-only / not-executed / control failures | attempt.ReplayPredicateDrivesRepeatFromHostFacts, ReplayPredicateEvidence |
| Independent S3 bodies | PreparedPutHasIndependentFullBodies, PutPreparationFailureOwnership, PutRejectsUnpreparedAndClosesFactoryFailures |
| Kafka indexed facts | PublishPreservesIndexedBatchFacts, MalformedBatchAndSingleErrors |
| Sync acknowledgments | ConcreteWriterRejectsEnqueueAndNoAck, checked kafka-go Writer source, explicit custom-writer precondition |
| Resource ownership | existing streaming/cursor lifetime tests, PartialDownloadKeepsOwnershipOnError, PartialCursorRetainsLifetimeOnError, EstablishedStreamFailureNeverRestartsRPC |
| Cancellation | HTTP deterministic Done barrier/worker completion, root RetryIfFailedCallCancellationPrecedence |
| Public contracts | adapter-replay-contracts.md, execution-contracts.md, migration.md, package docs and executable Sequence ordinary/resource examples |

Final gates: make lint passed all nine modules with zero issues; make test passed
all nine modules under race. Target race in root/attempt/execution passed. HTTP
cancellation barrier stress repeated 30 times under race passed. git diff --check passed.
Lint wrapper only allows concurrent runners; no check is disabled.

Logs: /tmp/routery-task10-lint.log, /tmp/routery-task10-test.log,
/tmp/routery-task10-target-race.log, /tmp/routery-task10-http-stress.log.

Scope remains universal routing/resilience, BYOT and optional execution composition.
Hidden physical SDK retries, verified remote deduplication and custom-writer acknowledgment
are explicit host contracts; no planner, memory, producer/outbox or durable ledger was added.
