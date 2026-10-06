# API choices after task13

The core remains transport-independent and accepts caller types. Policy is optional;
workflow checkpoints, model wire protocols, business permission, pricing ledgers,
evaluation and telemetry export remain host or sibling-library responsibilities.

## Picking an entry point

| API | Trigger | Ownership/replay contract |
| --- | --- | --- |
| RouteTable fallthrough / Chain | ActionNext with nil error | Close discarded owner; stop on cleanup failure |
| Fallback | Any primary system error | Unconditional error fallback; host must ensure secondary is permitted; cleanup failure stops |
| PredicateFallback | System error and explicit predicate | Preferred low-level error fallback gate; predicate has no lifecycle side effects |
| RetryIf | System error, remaining total attempts and explicit predicate | Host replay permission; close intermediate owner only after all gates; stop on cleanup failure |
| execution.Sequence | Caller failure classification plus explicit replay/scheduling evidence | Physical-attempt accounting, partial/unknown facts, fresh next binding, no implicit refunds |
| FirstSuccessfulPayload | First nil-error terminal payload in a parallel group | May accept an open stream; caller supplies permission for duplication and owns its lifetime |
| execution.Race CompleteOnly | Explicit accepted-result validation and complete attempt facts | Bounded attempts, replay/effect/cost permission, receipts and late-result journal |

Table fallback means no route terminated; it does not rescue handler errors.
`FirstSuccessfulPayload` means a successful payload arrival, not completed remote computation.
For phase-aware safety and late accounting use Race rather than inferring completion from a stream handle.

## Review decisions

| # | Decision | Rationale / resulting contract |
| --- | --- | --- |
| 1 | KEEP sealed Router | Compiled snapshots are library-owned; extensions use handlers/middleware or a caller Dispatch interface. Making snapshots externally implementable adds an unsupported ownership surface. |
| 2 | CHANGE Async | Remove Async/BasicAsync; use Handled with caller Kind (or BasicKindAsync). Constructors never schedule work. |
| 3 | CHANGE FirstCompleted | Rename to FirstSuccessfulPayload; its predicate accepts terminal payloads including streams, unlike Race CompleteOnly. |
| 4 | KEEP fallback mechanisms | They have different triggers and guarantees; the selection table above makes each explicit. Recommend PredicateFallback for low-level error gates. |
| 5 | CHANGE OnClose | Hooks run after cleanup finishes, including registrations during cleanup. Already-completed cleanup permits immediate hooks. Hooks and cleanup must not recursively Close the same lifetime. |
| 6 | CHANGE breaker panic | Handler/classifier panic releases a current half-open probe; panic propagates unchanged. Panic never becomes a fabricated success. |
| 7 | CHANGE observer metadata | Detach Match paths in both event fields. Payload, errors, decision reason and Lifetime remain borrowed read-only references; host projects/redacts for telemetry. Arbitrary deep-copy is impossible with BYOT. |
| 8 | KEEP strict model snapshot | Stale mandatory facts reject selection as a whole, including unrelated candidates. Host refreshes the snapshot or supplies a deliberately scoped candidate set; no implicit fail-open exclusion. |
| 9 | CHANGE empty Dispatch | Require Selected; NoEligible/AffinityUnavailable return ErrNoSelection without invocation. Caller handles expected absence using Status. |
| 10 | KEEP affinity identity | Required affinity pins exact Key and Scope. Compatible is an additional restriction, not permission to change endpoint; provider continuation portability remains host evidence. |
| 11 | CHANGE Redis invoker | Remove unused *redis.Client parameter and rename CommandExtractor to CommandInvoker. Callback executes via caller Client/ClusterClient/Ring; adapter checks handle error and maps result. |
| 12 | CHANGE invalid values | RetryIf and gRPC zero attempts mean one total call; Timeout zero disables it. Negative attempts/backoff/timeout fail before execution. Nil gRPC predicate still denies repetition. |
| 13 | CHANGE bulkhead error | ErrBulkheadFull names occupied concurrency slots; it does not represent provider rate limiting. |
| 14 | CHANGE S3 classification | Exact Smithy error codes, HTTP statuses and network timeout types only; free text stays unknown. Classification never supplies replay permission. |
| 15 | CHANGE model prefix | Errors use routery/policy/model, matching the actual package location. |

No replacement scheduler, agent type, quota store or combined adapter/core module is introduced.
