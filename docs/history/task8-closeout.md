# Issue #3: delivery and closeout draft

Source: https://github.com/skosovsky/routery/issues/3.

This file is a draft, not a posted comment or permission to close the issue. Final
acceptance requires both independent audits, resolved findings and fresh full-module
gates. Do not publish the draft with missing audit evidence or label it complete
merely because implementation tests passed.

## Applicability and implementation matrix

All four cards are active in this implementation scope; none is silently deferred.
Applications opt into only the mechanisms whose applicability conditions they meet.

| Card | Applicability | Public contract and implementation | Executable evidence |
|---|---|---|---|
| RTR-001 | Candidates differ in hard capabilities/constraints or host ranking objectives | `policy.Selector`, `Candidate`, `Evaluation`, `Selection`, `ValidatePinned`; optional `policy/model` | `policy/selection_test.go`, `policy/temporal_test.go`, `policy/model/*_test.go`, model boundary example and explicit fallback composition |
| RTR-002 | Repeats/fallbacks/concurrent execution require phase, replay, scheduling or streaming ownership | `policy/attempt`, `execution.Boundary`, `Sequence`, `Race`, explicit `Lifetime` | Boundary/sequence/race fixtures, HTTP normalized-hint integration, reset/nested-budget fixtures, ordinary-operation example |
| RTR-003 | Concurrent operations compete for a shared limit and host supplies an atomic backend | Optional generic `quota.Backend` and `Session`; per-physical-attempt admission/settlement | Atomic admission/concurrent finalization/lost-ack/expiry fixtures and execution quota lifecycle/failure-policy integration |
| RTR-004 | Required continuation portability or optional cache locality constrains selection | `policy.Affinity` with trusted scope and explicit compatibility; host-owned rebuild | Required/preferred/scope/expiry tests, pinned temporal validation, authorized rebuild and separate lineage/usage fixture |

The local quota fixtures are test backends, not a shipped durable store. Applications
must supply their own atomicity, idempotency, TTL and retention guarantees. Policy
packages do not require model facts, streams, storage, transcripts or observation.
The root module remains dependency-free and caller types remain authoritative.

## Author-facing comment draft

Реализация подготовлена для приёмки. Полная карта требований и доказательств:
`docs/history/task8-checklist.md`; публичные контракты: `docs/execution-contracts.md`;
миграция с примерами «было → стало»: `docs/migration.md`.

Что требуется изменить в вашем коде:

- RTR-001: разделить hard eligibility и ranking. Передавать свои immutable
  descriptors, projections, estimates и отдельные fingerprints/epochs. Обрабатывать
  `NoEligible` без provider call. Перед dispatch валидировать текущие clock, TTL,
  quality и binding через `ValidatePinned`, а не только сравнивать epochs.
- RTR-002: передавать явные operation/physical-attempt identities, phase, remote
  outcome и consumer commit. Назначить единственного владельца retry и общий
  attempt budget. Нормализовать hints с clock/provenance, соблюдать not-before,
  обрабатывать typed stop/reconcile/defer и partial/unknown. Reset после output
  должен завершаться на стороне host и не заменяет replay evidence. В race держать
  output до accepted-result predicate, задавать duplication permissions и закрывать
  owned результаты. Hidden nested attempts отмечать как ненаблюдаемые.
- RTR-003: подключить atomic reservation port; reserve на каждую физическую
  попытку, в том числе конкурентную. Передавать actual usage/completeness и
  стабильную settlement identity. Unknown/incomplete остаётся pending; cancellation
  или TTL не дают права на refund. Учитывать overage без обрезания до estimate.
  Fail-open явно теряет гарантию квоты и запрещён при unknown acknowledgement.
- RTR-004: передавать required/preferred affinity с trusted scope, fingerprints,
  expiry и compatibility callback. Не переносить continuation по совпадению имени
  endpoint и не превращать required state в stateless request. Rebuild разрешается
  только host callback; нужны fresh eligibility, новый physical ID, lineage и
  отдельный usage. История, state и credentials остаются у host.

Исправления требуют следующих изменений:

- BUG-001: owned stream/result закрывать через `Lifetime` или связанный adapter
  body close; победитель `FirstCompleted` сохраняет context до собственного close,
  losers отменяются и очищаются. Query rows/cursors закрывать через result lifetime,
  чтобы освободились cancellation hooks и permits.
- BUG-002: вместо `NewRouteHandler(client, options...)` использовать явную
  `PrepareRequest(request, options...)` до fan-out, затем `NewRouteHandler(client)`.
  Не полагаться на lazy mutation общего body. Preparation failure означает ноль
  отправок; каждая attempt владеет собственным reader.
- BUG-003: default POST/PATCH retry после 503 больше не разрешён. Для повторов нужны
  replayable body и явное proven-not-executed/verified-deduplication evidence.
  Произвольный header не является доказательством. `UnsafeReplay` не разрешает retry.
- BUG-004: cancellation проверяется независимо от backoff/predicate перед новой
  попыткой и fallback. Уже запущенная remote operation всё ещё может завершиться.

Независимый аудит также потребовал изменить обработку ownership/control errors:

- `DispatchAndProject` и `ProjectRouteResult` возвращают исходный owner через
  `ProjectionMeta.Lifetime`, в том числе при dispatch/projection error и mapping.
  После чтения или отказа от projection закрывайте этот lifetime и проверяйте
  cleanup error. Projector/error policy заимствуют ресурс, не создают второго owner.
- Custom HTTP resource handlers обязаны прикреплять `RouteResult.Lifetime` и
  связывать body close с ним. HTTP `Timeout` больше не определяет ownership по
  наличию body: он использует generic middleware. Без lifetime результат считается
  value и timed context отменяется при возврате.
- Router закрывает discarded `ActionNext` до следующего handler/fallback; cleanup
  failure останавливает fallthrough. Invalid route action сохраняет owned partial
  result, который caller обязан закрыть, даже при `ErrInvalidConfig`.
- Custom Boundary dispatch теперь проходит общий `ValidateRouteResult`. Abort без
  error или неизвестная action возвращают `ErrInvalidConfig`, не успешный outcome;
  partial/receipt не теряются. Эта ошибка не допускает нового dispatch через
  Sequence/Race, даже при permissive provider classifier/replay policy.
- `Sequence` и `Race` не превращают `ErrInvalidEvent`/`ErrInvalidBoundary` в provider
  retry. Race закрывает общий queued-plan gate; уже начатые attempts остаются в
  accounting. Проверяйте поздние errors/receipts в Journal: snapshot не является
  completion barrier, отмена не означает rollback или нулевой расход.
- Logging/Metrics/Tracing сохраняют owned partial при error вместо пустого Abort.
  Закрывайте возвращённый lifetime и при ошибке; observer не освобождает permit
  или reservation сам по себе и не подтверждает terminal completion.

Публичные runnable examples для host composition:
`policy/execution/quota_example_test.go` (pending, late usage, overage) и
`policy/execution/affinity_example_test.go` (required-unavailable, explicit rebuild,
новая selection/identity, lineage и отдельный usage). Их backend/provider fixtures
не заменяют production host contracts.

Заменённые небезопасные paths не сохраняются compatibility wrappers. Новые
execution contracts не означают exactly-once remote effects, автоматическое
восстановление потока, zero cost после cancellation или distributed guarantee без
проверенного host backend. Delayed hedging, training, semantic cache, ledger и
control plane не входят в согласованный scope.

Дополнительный regression выявил и исправил ошибку композиции: `ErrInvalidEvent`
нельзя классифицировать как retryable provider failure. `Sequence` возвращает этот
control error без повторного dispatch даже при permissive replay policy.

## Acceptance evidence to append before publication

Tracing callers must supply the new explicit third argument: nil for bounded
defaults or a host-owned safe attribute projection. Default output no longer
contains raw errors, Kind/Reason, Match fields or route-derived span names.
Additional labels require explicit allowlisting/redaction; `routery.action` is
reserved. Result metadata and ownership remain unchanged. This is a clear break,
not an optional compatibility shim.

Append the final all-module `make lint` and `make test` results for the delivered
state, the completeness report with numerator/denominator and checklist omission
audit, and the independent correctness report with all confirmed findings resolved.
Report any host-owned limitations explicitly. Do not remove requirements to improve
the completeness fraction; explicit user decisions are required for any deferral.

After acceptance, separate authorization is required to publish or close issue #3.

Final implementation evidence is recorded in `task8-handoff.md`: independent
substantive completeness116/116, both audit verdicts integrated, CA-001–006 and
tracing privacy resolved, final all9module lint/test gates passed. Full goal before
actual user handoff is120/121; the final response supplies C01 for121/121. Host-owned
limitations are retained there and in migration/contracts. This draft is not posted.
Substantial public API/behavior changes use `make release-break` after release
authorization. `make release-patch` applies only to a separately delivered minor
fix, not to this clear break. Neither release command has been executed here.
