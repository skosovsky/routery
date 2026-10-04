# Независимый аудит корректности task 8

Аудитор: отдельный субагент correctness_audit. Первый проход по текущему worktree;
HEAD исходного checkout — `c8de26ae53777571fdc91b177d7f172c982ae00a`.
Этот отчёт не является аудитом полноты и не использует выводы другого аудитора.
Production code и checklist аудитором не изменялись.

## Метод и границы

Прочитаны исходное `.cursor/docs/task8.md`, `docs/execution-contracts.md`,
миграция, изменённые production paths core и extensions, все production файлы
`policy`, provider-neutral examples/rebuild fixtures и ключевые regression suites.
Проверены ownership, fallthrough, invalid-result boundaries, phase/outcome/replay,
cancel-before-dispatch, quota session/acknowledgement/concurrency, timestamp
arithmetic, selection freshness и affinity scope.

Независимая проверка `GOCACHE=/private/tmp/routery-verification.37dM1A/go-cache
go test -race -count=1 ./...` прошла для root, observability и всех policy packages.
Это не all-extension gate и не доказательство отсутствия ошибок: четыре сценария
ниже воспроизвелись вне существующего suite.

Для четырёх репродукций создан только временный файл
`/private/tmp/routery-correctness-repro.go`; команда:

```sh
env GOCACHE=/private/tmp/routery-verification.37dM1A/go-cache \
  go run -race /private/tmp/routery-correctness-repro.go
```

Файл подтверждает ошибочное поведение, не заменяет постоянные regression tests.
Строки ниже относятся к коду первого прохода; при исправлениях будут сдвигаться.

## Подтверждённые дефекты первого прохода

### CA-001 / P1 — Router теряет discarded ActionNext ownership

Код: `router.go:68–89`, `dispatchTable`; `dispatchHandler` возвращает owned
ActionNext как nonterminal без cleanup.

Arrange: первый matching handler возвращает ActionNext с Lifetime, второй
возвращает handled. Act: Router.Dispatch. Assert фактически: payload winner,
nil error, cleanup count **0**, Lifetime первого результата недоступен caller.

Последствия: утечка stream/cursor/permit; если lifetime содержит settlement hook,
отложенный accounting не финализируется. Аналогично overwrite несколькими Next,
fallback и cancellation после Next. Chain уже закрывает discarded Next, Router
должен иметь тот же явно описанный ownership contract.

Нужные проверки: next → handled, next → next, next → fallback, next → cancel,
nested table fallthrough; cleanup ровно один раз, последующий handler не получает
владение discarded resource, cleanup error не теряется без явно принятой policy.

### CA-002 / P1 — Validation error уничтожает owned partial result

Код: `outcome.go:137–154`, `abortWithoutError` и `validateReturnedResult`.
Затронуты InvokeRouteHandler, Chain и Router.

Arrange: leaf возвращает owned result с ActionAbort и nil error (либо неизвестной
Action). Act: InvokeRouteHandler. Assert фактически: ErrInvalidConfig,
Lifetime=nil, cleanup count **0**, original payload/ownership потеряны.

Ошибочный route action не разрешает потерять ресурс. Нужно либо сохранить owned
partial result при переводе в Abort/error, либо гарантированно закрыть его и
сохранить cleanup error. Контракт должен быть одинаков для всех validation paths.

Нужные проверки: Abort-without-error и unknown Action через Invoke, Chain,
Router/fallback и FirstCompleted; cleanup/retained ownership, metadata и ошибки.

### CA-003 / P1 — Safe race продолжает dispatch после lifecycle contract failure

Код: `policy/execution/race.go:194–216`, worker exit condition.

Arrange: Workers=1, два plans, explicit duplication permissions, бюджет 2.
Первая Boundary.Dispatch возвращает attempt.ErrInvalidEvent. Act: Race.Run.
Assert фактически: **2** dispatch, RaceAccepted, nil error — второй результат
скрывает control-contract failure. Воспроизведение не зависит от concurrency.

Это противоречит §4.1 и acceptance §4.3/8: invalid phase/identity не должна
превращаться в разрешение нового dispatch. Sequence уже отдельно отклоняет
ErrInvalidEvent/ErrInvalidBoundary; Race не делает этого при Started=true.

Нужные проверки: ErrInvalidEvent и ErrInvalidBoundary, включая wrapped/joined
errors; один worker не берёт следующий plan. При нескольких workers уже начатые
attempts остаются accounted, но после обнаружения fatal control error новые
queued attempts не стартуют. Ошибка доступна host, результаты/cleanup сохранены.

### CA-004 / P1 — Projection error boundary теряет owned partial resource

Код: `projection.go:131–150`, `handleProjectionError:164–168`.

Arrange: Router возвращает owned partial result и provider error; policy=nil.
Act: DispatchAndProject. Assert фактически: zero projection, provider error,
cleanup count **0**. Public return не содержит canonical result/Lifetime,
projector вообще не вызван, caller физически не может закрыть ресурс.

То же происходит при projectErr и nil policy. Это оставшийся старый wrapper,
который не был согласован с новым explicit ownership contract. На success-path
также необходимо явно определить, кто получает Lifetime: projection может
сохранить payload без callback, но raw SQL/cursor close недостаточен для hooks.

Нужные проверки: dispatch error без policy, projector error без policy,
error mapping и successful resource projection. Решение — явная передача
canonical ownership либо гарантированный cleanup там, где ownership не
передаётся. Не вводить resource type assertions/compatibility shims.

## Дополнительное контрактное замечание

HTTP Timeout первого прохода сохранял fallback wrapping payload.Body при
Lifetime=nil и отдельный cancelTimerBody. Это второй старый ownership path.
У корректного resource leaf Lifetime обязателен, поэтому отдельно как дефект
корректного caller не засчитано. Однако clear break требует удалить заменённую
ветку/тип либо явно отклонить отсутствие ownership; generic Timeout уже
реализует новый единый контракт. Сообщено исполнителю отдельно от четырёх
подтверждённых дефектов.

## Итог первого прохода

Четыре подтверждённых дефекта требуют исправления и повторной независимой
проверки. Выполнение all-module lint/test исполнителем не заменяет эти regression.
Независимый аудитор не выпускал release, не публиковал изменения и не закрывал
issue. Production backend/SDK integration, durable atomicity и arbitrary caller
callback correctness не доказаны локальными fake fixtures и остаются в границах
host contract. Отсутствие дополнительных найденных ошибок не означает
абсолютного отсутствия ошибок.

## Повторный проход: исправления первого аудита

Исходное ТЗ и текущий контракт перечитаны независимо. Выводы аудитора полноты
не использовались. Проверены actual changes, не только assertions исполнителя.

| Находка | Статус повторного прохода | Проверенное доказательство |
|---|---|---|
| CA-001 | Resolved | Router закрывает каждый nonterminal owner перед продолжением, cleanup error возвращается; handled/next/fallback/cancel/nested/error scenarios в ownership_boundaries_test.go |
| CA-002 | Resolved | Единая canonical validation сохраняет partial payload/owner при Abort/error; Invoke/Chain/Router/fallback/FirstCompleted scenarios, неизвестная Action не публикуется |
| CA-003 | **Open** | Queue gate прекращает новые authorizations, но failure проверяется до checkWinner; finalization race ниже |
| CA-004 | Resolved | ProjectionMeta.Lifetime переносит canonical ownership через dispatch/projection/mapping/success paths; custom metadata не может удалить/заменить owner |
| HTTP legacy concern | Resolved | Timeout — generic specialization; cancelTimerBody и body-inference fallback удалены, custom resource fixtures передают explicit Lifetime |

Независимые проверки повторного прохода:

- root ownership/validation/projection regressions: `go test -race -count=100`
  с targeted `-run` — pass;
- race control tests и все execution examples: `go test -race -count=100`
  с targeted `-run` — pass;
- HTTP preparation ownership и timeout regressions: `go test -race -count=100`
  с targeted `-run` — pass;
- `go test -race -count=1 ./...` — pass root, observability и policy packages.

### CA-003 / P1, оставшийся finalization gap

Текущее Race.Run проверяет control.failure() сразу после FirstCompleted,
**до** checkWinner(ctx, winner.Payload). Fatal error может зарегистрироваться,
пока checkWinner выполняет context/receipt checks; последующего атомарного
success/failure решения нет. Returned success противоречит обещанию, что уже
наблюдённый fatal error отменяет success.

Репродукция: `/private/tmp/routery-race-finalization-repro.go`, `go run -race`.
Две ветки уже начали dispatch. Winner Accepted; parent context.Err внутри
checkWinner использует barrier. Bad ветка ждёт barrier и возвращает
ErrInvalidEvent; её cleanup подтверждает, что control.observe уже выполнен.
Проверка winner продолжается, Race.Run возвращает **RaceAccepted, nil error**.
Обе journal entries сохранены. Context не отменён, факт ошибки не зависит
от sleep; barrier раскрывает реальное допустимое interleaving.

Нужна единая линейная finalization точка после проверки winner, сериализованная
с observe. Контракт поздних ошибок следует привязать к этой точке, а не обещать
невозможную атомарность относительно машинной инструкции return. Late errors
после принятой finalization остаются в Journal и не изменяют прошлый return.

### CA-005 / P2 — Boundary пропускает malformed canonical route action

Код: `policy/execution/boundary.go`, invoke, прямой boundary.Dispatch без
canonical validation. Неизвестная Action и ActionAbort без error — неправильные
результаты существующего RouteResult contract, но Boundary возвращает nil error.
Sequence воспринимает их как завершённый local return; Race может продолжить
queued plans, хотя обнаружение control failure обязано остановить новые calls.

Репродукция: `/private/tmp/routery-boundary-action-repro.go`, `go run -race`:

```text
action=abort error=<nil> owned=true
action=unknown error=<nil> owned=true
```

Нужен reuse canonical validator через существующий invocation primitive или
один общий публичный validation port, не второй validation engine. Сохранить
partial payload/Lifetime/Receipt. ErrInvalidConfig от canonical validation
не является provider failure и не должен разрешать replay в Sequence/Race.
Regression: оба malformed actions в Boundary/Sequence/Race, nil error от
dispatch, retained ownership и ноль дополнительных provider calls.

Severity P2: сценарий требует неправильного callback, но публичный validation
contract должен явно отклонять этот случай, как уже делают остальные primitives.

Итог повторного прохода: CA-001/002/004 и legacy concern resolved;
CA-003 и CA-005 требуют исправления и повторного независимого подтверждения.

## Третий проход: finalization и единая validation

CA-003 и CA-005 **resolved** по самостоятельному чтению актуального кода и
повторному выполнению собственных репродукций. Race.finalize выполняет последний
fatal check/фиксацию acceptance под тем же mutex, что observe и next; фиксирует
stop-queue flag. Late journal contract теперь честно привязан к linearization
point. Boundary использует общий ValidateRouteResult без повторного invocation
или изменения контекста; Sequence/Race считают ErrInvalidConfig control failure.

Проверено независимо:

- прежняя finalization repro возвращает NoAccepted / ErrInvalidEvent / journal=2;
- прежняя malformed-action repro возвращает ErrInvalidConfig с сохранённым owner;
- execution canonical action/control/fatal matrix и executable examples —
  `go test -race -count=100` pass;
- core canonical validation/ownership/projection matrix —
  `go test -race -count=100` pass;
- root, observability и все policy packages — `go test -race -count=20 ./...` pass;
- `git diff --check` — pass.

### CA-006 / P1 — Observability wrappers теряют partial ownership

Найдена после закрытия прежних findings при повторной проверке старых wrapper
paths, а не по отчёту completeness auditor.

Код: `observability/logging.go:68`, `observability/metrics.go:60`,
`ext/otel/tracing.go:47`. Каждый заменяет result на новый пустой AbortResult
при nonnil callback error. Leaf с owned partial resource теряет Lifetime,
payload, typed kind/reason и исходный route match. Resource не закрывается.

Репродукция `/private/tmp/routery-observer-ownership-repro.go` с race detector:

```text
logging owned=false payload="" has=false closes=0 err=provider failure
metrics owned=false payload="" has=false closes=0 err=provider failure
tracing owned=false payload="" has=false closes=0 err=provider failure
```

Logging использует nonnil no-op observer, Metrics nonnil OnComplete,
Tracing SDK tracer fixture; наблюдение не должно менять execution ownership.
Если внутри leaf находятся Bulkhead/Boundary, также теряются release/settlement
hooks. Plain value happy-path tests не доказывают безопасность такой композиции.

Нужно reuse ValidateRouteResult и preserve canonical metadata/owner на error-path;
match fallback применяется только при отсутствии match. Не создавать второй
owner и не выводить payload contents в diagnostics. При malformed nil-error
result должен сохраняться тот же canonical error contract.

Нужные regressions: Logging/Metrics/Tracing, partial error и invalid action,
explicit owner, сохранённый match и caller metadata, observer shape-only, cleanup
ровно один раз; композиция с FirstCompleted/Bulkhead/Boundary и settlement hooks.

Итог третьего прохода: CA-001–005 и HTTP legacy concern resolved;
**CA-006 open**. Существующие зелёные root/policy tests не ловили этот путь.

## Четвёртый проход и актуальный итог

CA-006 **resolved**. Актуальные Logging, Metrics и Tracing используют
ValidateRouteResult и сохраняют canonical owner, partial payload и existing match.
Старые AbortResult replacement paths удалены. Observer не закрывает resource
и не создаёт второго owner; payload default metadata остаётся shape-only.

Независимо перечитаны actual observer code/diff, permanent action/error matrices
и композиции с Bulkhead, Boundary, FirstCompleted. Повторная собственная
репродукция теперь даёт для всех трёх middleware:

```text
owned=true payload="partial" has=true closes=1 err=provider failure
```

Независимые checks текущего checkpoint:

- observability action/error/ownership и permit/receipt composition:
  `go test -race -count=100` targeted — pass;
- tracing action/error/ownership и Boundary/Bulkhead/FirstCompleted composition:
  `go test -race -count=100` targeted — pass;
- root, observability и все policy packages:
  `go test -race -count=20 ./...` — pass;
- `git diff --check` — pass.

Повторный поиск production paths с result replacement и просмотр остальных
middleware не выявили другого подтверждённого partial ownership loss.
All-module lint/test выполняет основной агент; аудитор не выдаёт свои targeted
checks за gate всех extensions и не запускал параллельный lint.

| Находка | Актуальный статус |
|---|---|
| CA-001 / Router discarded Next | Resolved |
| CA-002 / invalid result partial ownership | Resolved |
| CA-003 / race control gate и finalization | Resolved |
| CA-004 / projection ownership | Resolved |
| CA-005 / Boundary canonical validation | Resolved |
| CA-006 / observability wrapper ownership | Resolved |
| HTTP Timeout legacy concern | Resolved |

**Открытых подтверждённых дефектов в согласованном объёме этого аудита: 0.**
Это независимый аудит корректности, не сертификат абсолютного отсутствия ошибок
и не аудит полноты требований. Ограничения сохраняются: нет exhaustive proof
всех возможных interleavings, реального distributed backend/remote SDK execution
и arbitrary caller callbacks; host-owned invariants подтверждены локальными
controlled fixtures в пределах опубликованного контракта. Reconciliation,
unknown remote effects и некооперативные providers по-прежнему не объявляются
exactly-once или zero-cost guarantees. Release/публикация/issue closure не делались.

## Дополнительная независимая проверка tracing privacy

После финального ownership review основной агент передал новую privacy-правку
tracing для отдельной проверки. Отчёт другого аудитора не читался. Проверены
актуальные `ext/otel/tracing.go`, helper `setProjectedAttributes`, privacy tests,
ownership/composition suites и обновлённый публичный/миграционный контракт.

Подтверждено по коду и фактическому SDK-exporter fixture:

- nil projection автоматически публикует только canonical action, фиксированное
  default имя и bounded error status; raw error events, Kind/Reason и Match
  больше не экспортируются автоматически;
- unknown route action сначала canonicalized в Abort/error, поэтому arbitrary
  caller action не превращается в диагностический label;
- explicit projection получает canonical metadata без отдельных request/payload/
  Lifetime полей; возвращаемые атрибуты принадлежат host allowlist contract;
- ключ `routery.action` от projection игнорируется, canonical attribute выставляется
  после projection и не заменяется сторонним значением;
- прежние partial ownership/receipt/permit fixes сохранились; third argument
  обязателен, двухаргументного compatibility path нет;
- вынесение фильтрации в helper и constants не изменило эту семантику.

Независимо выполнен **весь** `ext/otel` suite `go test -race -count=100 ./ext/otel/...`
на checkpoint после helper extraction — pass. Повторная собственная observer
репродукция с явным nil third argument сохраняет owner/payload и exactly-once
cleanup для Logging/Metrics/Tracing. `git diff --check` — pass.

Новых подтверждённых дефектов корректности/security в этой правке не найдено.
Текущий итог по-прежнему: **CA-001–006 resolved, открытых подтверждённых дефектов 0**.
Explicit projection/name и SDK exporter configuration — доверенная host policy:
библиотека не может доказать безопасность произвольного пользовательского
allowlist callback и не объявляет такой гарантии. Проекция заимствует metadata;
caller-owned pointer/opaque values внутри generic Kind/Reason/Match не становятся
автоматически безопасными и не должны автоматически stringified/exported host.
