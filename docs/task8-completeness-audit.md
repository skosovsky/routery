# Task 8: независимый аудит полноты — первый проход и повторные проверки

Источник требований: `.cursor/docs/task8.md` полностью, активный goal пользователя,
`docs/task8-checklist.md`. Проверены публичные контракты, migration/closeout,
текущие core/policy/HTTP реализации и acceptance fixtures. Отчёт другого аудитора
не использовался. Production code и исходный checklist этим аудитором не менялись.

Это оценка проверенного checkpoint, а не финальная приёмка. Во время аудита основной
агент начал исправлять переданные находки. Новые изменения не считаются выполненными
до повторного чтения и проверки. Статус «выполнено» означает доказанный весь clause
строки; частичные строки дают ноль в числителе.

## Результат и знаменатель

- Исходный знаменатель: 120 = 108 строк минус 5 исторических context-строк + 17 дополнительных.
- Добавлен **T8-C01**: самостоятельный финальный ответ пользователю с числителем/
  знаменателем, проверками, выводами обоих аудиторов и ограничениями. Его требует
  goal, но отчёт аудитора T8-A07 не является этим финальным handoff.
- Новый полный знаменатель: **121**. Ни одна RTR-карточка не исключена и не отложена.
- Первый baseline: **108 / 121 = 89.26%**; 9 частично, 4 не выполнено.
- После независимо проверенных fixes этого прохода: **112 / 121 = 92.56%**;
  5 частично, 4 не выполнено.
- Второй checkpoint: прежние reader/example gaps закрыты, но выявленные при
  продолжающемся независимом correctness audit finalization/route-action gaps
  требовали fixes/re-audit: **114 / 121 = 94.21%**; 3 частично, 4 не выполнено.
- Третий substantive re-audit: **117 / 121 = 96.69%**; 0 частично,
  4 процессных/final-delivery requirements ещё не интегрированы.
- Четвёртый re-audit (CA-006 ownership checkpoint): **116 / 121 = 95.87%**;
  1 частично (T8-009), 4 процессных/final-delivery requirements не выполнены.
  Новая privacy-находка ниже отменяет прежний substantive verdict, но не историю.
- Пятый re-audit после исправления tracing privacy: **117 / 121 = 96.69%**;
  0 частично, 4 процессных/final-delivery requirements остаются не выполнены.
- Pre-delivery integration review: **120 / 121 = 99.17%**, 0 частично,
  только фактический user handoff C01 ещё не выполнен.
- Implementation/documentation subset сейчас: **116 / 116 = 100%**. Из этого subset
  отдельно исключены только пять audit/final-handoff deliverables
  T8-A07/A08/A09/A10/C01; они остаются в полном знаменателе.
- Процессные deliverables сейчас: **4 / 5**; оба verdicts/finding closeout/gates
  интегрированы, фактический final response ещё не доставлен.

Полнота не равна утверждению об отсутствии ошибок. Числа надо пересчитать после
устранения пробелов и повторной проверки; нельзя переписать partial в complete
на основании плана или только зелёного общего suite.

## Конкретные пробелы

1. **T8-007 — invalid selection status.** В проверенном `policy.Dispatch`
   условие `selection.Status != Selected` возвращает nil и для неизвестного enum.
   Например, status 255 не является ни NoEligible, ни AffinityUnavailable, но
   ошибкой invalid contract не становится. Нужен ErrInvalidSelection до callback
   и dispatch и regression неизвестного значения. Подтверждение: во время моего
   `make test` новый `TestDispatchRejectsUnknownSelectionStatus` уже был добавлен
   основным агентом и воспроизвёл FAIL. Это одна находка, не два дефекта.
2. **T8-045 — replayable failure до dispatch.**
   `TestSequenceFallbackHintAndPerAttemptAdmission` записывает NotExecuted
   внутри Dispatch после local Dispatched. Это не требуемый pre-dispatch failure.
   `Sequence.Run` при `!last.Started` возвращает управление без повторения.
   Консервативное поведение неизвестного admission failure менять не требуется:
   нужна явная host-композиция классифицированного preflight error с Decide/Schedule,
   нулём первой отправки, новой physical identity, прежней operation и расходом
   общего бюджета. В простом decision unit test counters/dispatch не проверяются.
3. **T8-074 — второй quota admission при ещё открытом stream.**
   `TestBoundaryQuotaHeldThroughStreamAndPending` проверяет Reserved после headers,
   затем закрывает Lifetime и лишь затем делает вторую attempt. Это доказывает
   Pending после неизвестного close, но не полный заявленный acceptance scenario.
   Нужен второй admission до close и assert Denied/zero dispatch; отдельно сохранить
   pending-after-close и разрешение admission после подтверждённого settlement.
4. **T8-A01 — parent cancellation живого winner.**
   `TestFirstCompletedCleansLateResultsAndParentCancel` после parent cancel проверяет
   новый invocation на уже cancelled context. Живой winner context/body не измеряет.
   Race fixtures проверяют cancellation при собственном Lifetime.Close, не parent.
   Нужен захваченный live context или HTTP body с barrier: parent cancel до close
   отменяет именно уже возвращённый winner, cleanup остаётся exactly once.
5. **T8-A02 — счётчики request readers.** `auditReadCloser` считает только Close.
   Fanout fixture считает закрытия response bodies, не каждого request GetBody reader.
   Полные payloads и отсутствие race проверены, но требование однократного original
   read/consume и ровно одного close каждого attempt reader не измерено явно.
   Нужен instrumented original reader и независимая counted GetBody factory с
   отдельными request close counters (и ноль отправок при preparation failure).
6. **T8-100/T8-A16 — examples каждой карточки.** Публичные executable
   `ExampleBoundary_modelExecution` и `ExampleBoundary_ordinaryOperation` покрывают
   RTR-001/002. RTR-003/004 сейчас показаны внутренними Test fixtures с непубличными
   `quotaFixture`/`hostRebuild` helpers. Это conformance evidence, но не отдельные
   пользовательские runnable examples, которые §1 и §9.5 отличают от fixtures.
   Нужны public-API quota admission/settlement и affinity/authorized-rebuild examples
   (можно композиционные, без нового engine, SDK или production store).
7. **T8-101/T8-A10 — gates на текущем состоянии.** Исторические зелёные checkpoints
   задокументированы, но код меняется при аудите. Мой свежий root race suite прошёл;
   последующая all-module команда остановилась на red regression status 255.
   Нужны обе исходные make-команды для всех модулей после исправлений.

T8-A17 частичен вследствие неполной acceptance/example evidence, а не потому, что
условные карточки исключены. Сам applicability matrix корректно активирует все четыре.

Приведённые семь находок относятся к baseline первого прохода. После повторной
проверки reader counters и публичных examples прежние implementation gaps этого
аудитора закрыты. Новые подтверждённые finalization/Boundary route-action gaps
продолжающегося аудита не должны быть скрыты этим историческим списком; их проверка
описана ниже. Чужой отчёт не читался.

## Проверки аудитора

### Повторная проверка первых исправлений

Прочитаны новые `policy/execution/pre_dispatch_test.go`, explicit unknown-status
switch в Dispatch, `TestDispatchRejectsUnknownSelectionStatus`,
`TestFirstCompletedLiveWinnerStillObservesParentCancellation`, дополнение
`TestBoundaryQuotaHeldThroughStreamAndPending` с отдельным still-open scenario.
Четыре затронутых сценария независимо прошли **10 прогонов с race detector**:

`go test -race -count=10 -timeout 60s -run
'TestFirstCompletedLiveWinnerStillObservesParentCancellation|TestDispatchRejectsUnknownSelectionStatus|TestHostRetriesClassifiedPreDispatchFailure|TestBoundaryQuotaHeldThroughStreamAndPending' ./...`

По фактическому коду и результату закрыты T8-007/T8-045/T8-074/T8-A01.
Исходные находки выше оставлены как история аудита, не как открытые дефекты.
Readers/examples и fresh all-module gates ещё не закрыты.

### Второй checkpoint: reader ownership, examples и composition

Полностью повторно прочитано исходное ТЗ и актуальные публичные контракты,
migration/closeout и checklist. Новых semantic omissions помимо принятого C01
не выявлено; denominator121 сохранён, условные карточки не исключались.

Прочитаны и проверены:

- `ext/http/request_ownership_test.go`: original bytes/close count до/после fanout,
  independent GetBody readers, каждый отправляет полный payload и закрывается1 раз;
  ready GetBody не потребляет/не закрывает original stream в подготовке.
- `policy/execution/quota_example_test.go`: external package, public port/Boundary,
  shared stable settlement ID, неизвестное close→Pending, late verified overage7
  против estimate2, повторный report не дублирует usage. Host fake явно single-process.
- `policy/execution/affinity_example_test.go`: external package, trusted required
  affinity, no unauthorized transform/dispatch, host-authorized rebuilt state,
  fresh Select/ValidatePinned, new physical ID, lineage и отдельный usage3+5.
- `ownership_boundaries_test.go`, Router fallthrough и canonical validation:
  discard-owned Next до следующего handler; partial invalid result сохраняет owner.
- `projection.go`/`projection_ownership_test.go`: canonical owner возвращается
  через meta на success/dispatch/projector/mapping errors, custom meta не может
  подменить его; migration явно требует закрывать meta.Lifetime.
- Generic-specialized HTTP Timeout: нет cancelTimerBody/implicit body inference;
  owned partial result сохраняется, context жив до явного Close.
- Shared raceControl gate и `race_control_test.go`: invalid-event/config ошибки
  закрывают queue до cleanup; already-started accounting остаётся в journal.

Собственные проверки завершились terminal exit0:

- Полный root `go test -race -count=10 -timeout 90s ./...`.
- HTTP targeted reader/timeout/body ownership/FirstCompleted/default write safety/
  explicit dedup suite: 10 прогонов с race detector.
- Независимый `make test`: root и ext/s3, grpc, redis, otel, http, kafka, mongo, sql.
- Независимый `make lint`: те же9 модулей, каждый0 issues; отдельные TMPDIR и lint cache.

**Это checkpoint gates, не final после новых edits.** Основной агент сообщил о
подтверждённых воспроизведениях: fatal observed между control.failure() и checkWinner
может уступить accepted winner; Boundary пропускает invalid route action. По
прочитанному Run финальная проверка действительно предшествует checkWinner, а
Boundary пока не валидирует canonical action после callback. Новые fixes и
regressions будут проверены после их появления. T8-007 снова partial из-за этих
invalid-control-contract gaps (не из-за уже исправленного selection status).
T8-101 остаётся partial до fresh gates на изменённом состоянии, T8-A17 — partial
до соответствующей re-audit. Другой report для этого вывода не использовался.

### Третий substantive re-audit: canonical result и acceptance finalization

Повторно проверены текущие `outcome.go`, `invoke.go`, Boundary/Sequence/Race,
новые route-validation/finalization fixtures и изменения execution-contracts/
migration/closeout. Original specification и scope сохранены; новые semantic
omissions сверх C01 не обнаружены. Чужой аудиторский report не читался.

- `ValidateRouteResult` — shared generic core contract, а не новый provider engine:
  supplied error сохраняется как error identity, action становится Abort; unknown
  action/Abort с nil error даёт ErrInvalidConfig. Match/payload/Lifetime не теряются,
  cleanup не происходит скрыто; remote outcome не выводится из validation failure.
- Boundary вызывает shared validation сразу после Dispatch. Sequence/Race
  `isControlError` учитывает core config error вместе с lifecycle/boundary errors;
  не классифицирует invalid action как provider retry и не запускает queued branch.
- `route_validation_test.go`: Boundary/Sequence/Race × Abort/unknown action. Все
  случаи сохраняют partial/match/lifetime/Receipt, dispatch1/classification0,
  после явного close receipt Terminal/Unknown, не Completed/NotExecuted по догадке.
- `outcome_validation_test.go`: action×supplied-error matrix проверяет canonical
  ownership и error identity без premature cleanup.
- Race после `checkWinner` вызывает `control.finalize()` под shared gate mutex.
  Этот явный acceptance linearization point сериализован с fatal observation;
  уже observed fatal отклоняет/закрывает winner, а finalization блокирует дальнейшую
  queue authorization. Late errors после точки сохраняются в Journal, без обещания
  ретроактивно изменить результат или откатить уже начавшийся remote effect.
- `TestRaceFatalDuringWinnerValidationPreventsAcceptance` детерминированно
  удерживает checkWinner, в этом окне публикует fatal и assert no RaceAccepted,
  ErrInvalidEvent и winner cleanup1. Барьеры не заменены sleep.

Собственные проверки этого re-audit:

- **100 прогонов с race detector** shared validator matrix, canonical Boundary/
  Sequence/Race matrix, fatal-during-checkWinner/queued/cross-worker gate regressions
  и projection ownership matrix. Все прошли.
- Повторные независимые **make test** и **make lint** после новых production fixes:
  root + ext/s3, grpc, redis, otel, http, kafka, mongo, sql, terminal exit0;
  каждый lint модуль0 issues. Отдельный TMPDIR/lint cache, правила не ослаблялись.
- `git diff --check` terminal exit0.

На этом состоянии не осталось неподтверждённых substantive clauses или открытых
находок этого completeness audit. Все116 implementation/documentation requirements
доказаны в указанном scope. Это **не** финальное утверждение goal117/121→121/121:

| ID | Что остаётся главному агенту |
|---|---|
| T8-A08 | Получить и интегрировать независимый итог correctness-аудитора; я его report не читала и не подменяю своим verdict. |
| T8-A09 | Сопоставить оба набора findings, подтвердить все resolved/re-audited; мои первоначальные пробелы и проверенные новые fixes закрыты. |
| T8-A10 | Утвердить current successful all-module gates как final delivered-state evidence после обоих verdicts; при production edits повторить. Команды на проверенном состоянии уже прошли. |
| T8-C01 | Доставить финальный user handoff с полным числителем/знаменателем, gates, обоими выводами и host-owned limitations. |

Эти четыре requirements остаются в denominator121 и пока дают0. Они не excluded
requirements и не deferrals. Completion tooling/release/issue closure этим аудитором
не выполнялись. Goal completion решает основной агент по итоговому accepted state.

`GOCACHE=/private/tmp/routery-verification.37dM1A/go-cache go test -race -count=1
-timeout 60s ./...` прошёл: root, observability, policy, attempt, execution, model,
quota. Это не подтверждение extension gates.

`make test` с тем же cache был запущен для всех модулей и остановился в root policy
на новом `TestDispatchRejectsUnknownSelectionStatus`, добавленном основным агентом
для моей находки. Остальные root packages прошли. Статус конкретного процесса
проверен до terminal exit; команда не перезапускалась из-за observation timeout.
Новый lint не запускался: итоговый gate должен следовать за исправлениями и обоими
аудитами. Предыдущие pass-утверждения в progress.md не заменяют этот final gate.

### Четвёртый re-audit: CA-006 ownership и новый privacy counterexample

После CA-006 отдельно прочитаны текущие `observability/logging.go`, `metrics.go`,
`ext/otel/tracing.go`, обе `ownership_test.go` и `ownership_composition_test.go`,
а также исходный §2.9 и весь затронутый checklist. Отчёт другого аудитора не читался.

Logging/Metrics/Tracing теперь используют shared `ValidateRouteResult`, сохраняют
owned partial payload, Lifetime и существующий leaf Match, возвращают canonical
error/action без преждевременного cleanup. Typed error / Abort nil / unknown action
matrices проверяют error identity, cleanup error и hook, Shape-only metadata,
cleanup0 до Close и cleanup1 после двух Close. Boundary + Bulkhead + FirstCompleted
compositions доказывают удержание permit до Close, receipt Unknown (не inferred
NotExecuted), discarded-error cleanup и последующее освобождение permit. Root
observers дополнительно проверяют per-attempt Finish ровно один раз.

Собственные terminal-pass проверки, каждая **100× с race detector**:

- `./observability`: `TestObserversRetainOwnedPartialAndValidateActions` и
  `TestObserversPreservePermitAndReceiptThroughComposition`.
- `ext/otel`: `TestTracingRetainsOwnedPartialAndValidatesActions` и отдельный exact
  `TestTracingOwnershipThroughBoundaryBulkheadAndFirstCompleted`. Первый regex
  не включал composition test; поэтому он проверен отдельной командой, не по обещанию.

CA-006 ownership gap закрыт: T8-004/037 доказаны, прежние canonical contracts
не потеряны. Но **T8-009 остаётся partial** по независимой source-находке:

- `Tracing` вызывает `span.RecordError(err)` и `SetStatus(codes.Error, err.Error())`.
  Callback/provider error может содержать credential, prompt или opaque-state.
- `setResultAttributes` автоматически `fmt.Sprint` caller-owned comparable Kind/
  Reason, публикует request-derived Match.Key/Prefix/Remainder и caller route path;
  default span name тоже строится из caller Match.Path/RouteID. Нет safe bounded
  projection, redaction или явного host-controlled разрешения на эти поля.
- Opt-in tracer разрешает наблюдение, но не отменяет §2.9: произвольная строка
  caller request/error не становится безопасной низкокардинальной диагностикой.
  Новые shape-only tests проверяют payload, но не error/events/status/attributes.
- Нужен bounded default (canonical action/match kind/error class, без raw error и
  caller strings), дополнительная metadata только через явный host projection;
  regression с synthetic secret/high-cardinality во всех перечисленных местах.
  Clear break позволяет изменить старые default tracing expectations.

Logging/Metrics передают typed data в явно заданный host callback, сами никуда их
не экспортируют; это отличается от автоматического span publication Tracing.
T8-A17 остаётся выполненным по собственному узкому clause: все четыре карточки
активны, имеют public contracts/fixtures и не deferred. Privacy уже отдельный T8-009;
повторно штрафовать A17 за тот же clause или менять denominator оснований нет.

Main agent сообщает свежие all-module lint/test pass на CA-006 checkpoint и чистый
diff; это gate evidence, не proof privacy. Нужны fix/re-audit T8-009, при изменении
source fresh gates, затем интеграция audit/final deliverables A08/A09/A10/C01.
Текущие **116/121**, **115/116 implementation** сохраняют весь исходный scope.

### Пятый re-audit: safe tracing defaults и explicit projection

Повторно прочитаны текущие `ext/otel/tracing.go`, `privacy_test.go`, обновлённые
tracing/ownership/composition tests и публичный example, а также contract,
migration и closeout prose. Сверка с исходным §2.9 и affected checklist clauses
не выявила дополнительного semantic omission; denominator121 сохранён.

- Новый обязательный третий `AttributeProjection` аргумент — clear break, без
  compatibility wrapper. Nil projection экспортирует только canonical action,
  fixed empty-name `routery.handle` и bounded status `route failed`.
- Raw RecordError/err.Error и автоматические Kind/Reason/Match exports удалены.
  Перед публикацией action shared validator обеспечивает bounded canonical enum.
- Дополнительные labels только через явно переданную host-owned projection;
  TraceResult не содержит request/payload/Lifetime. Контракт требует allowlisting/
  redaction и bounded labels, safe explicit spanName/SDK configuration принадлежат
  host. Reserved `routery.action` нельзя заменить projection-атрибутом.
- Privacy fixtures (AAA) покрывают synthetic secret во всех caller result fields,
  request/payload/error; success/error assertions проверяют весь набор Attributes,
  Events, span.Name и Status.Description. Exact one canonical attribute, zero events
  исключают прежние raw error/string leaks. Safe projection fixture проверяет
  canonical metadata, error identity, unchanged Lifetime и reserved override guard.
- Ownership matrices/compositions после breaking signature продолжают доказывать
  owned partial/permit/receipt/cleanup. Migration и author closeout явно требуют
  third argument и объясняют удалённое automatic metadata publication.

Собственный **весь ext/otel suite100×race** (`go test -race -count=100 -timeout 90s
./...`) завершился terminal pass; `git diff --check` exit0. Чужой correctness report
не читался. Тесты остальных модулей на этом fresh privacy state выполняет основной
агент: их текущий terminal verdict сюда не подменяется прежним checkpoint.

После strict-lint refactor повторно прочитаны constants и выделенный
`setProjectedAttributes`: nil branch не публикует ничего, reserved action filter
сохранён. На этом точном source снова весь ext/otel suite100×race terminal pass.

T8-009 теперь выполнен. Все **116/116 implementation/documentation clauses**
доказаны в проверенном scope; не осталось открытых substantive findings моего
аудита. Полная оценка **117/121**: A07 выполнен этим отчётом; A08/A09/A10/C01
остаются final integration/delivery requirements. A09 требует интеграции и
re-audit также findings второго независимого аудитора, не только моих;
A10 требует final-state all-module gate verdict после обоих audits; C01 — реального
user handoff. Их нельзя исключить из121 или засчитать по обещанию.

### Pre-delivery integration review

Полностью прочитаны `docs/task8-handoff.md`, обновлённые additional deliverables
checklist и финальный closeout evidence block. Содержимое handoff соответствует
objective: numerator/denominator (включая C01), terminal all-module gates, отдельные
выводы обоих аудиторов, migration и host-owned limitations, запрет неподтверждённых
distributed/exactly-once claims, clear break/release guidance, отсутствие release/
publication/issue closure. Это готовый handoff artifact, не уже доставленный final.

Основной агент подтвердил получение независимого correctness verdict:
CA-001–006 resolved, confirmed open0, privacy/ownership100×race проверены.
Я не читала report другого аудитора: A08 здесь доказан root integration этого
независимого verdict, не моим пересказом чужого source review. A09 подтверждён
сведением обоих наборов findings и повторными проверками изменённых областей;
моих открытых substantive findings нет. A10 подтверждён main terminal gates
76405(lint)/11316(test): все9modules, race, zero lint issues, production source
после gates не менялся. Исторические failures сохраняются в отчёте как история.

Усиленная privacy fixture теперь включает secret также в call.Match и result
Match.Kind/DecisionReason. Повторно прочитана и независимо выполнена: весь otel
suite100×race terminal pass. Production contract не менялся. Нового semantic
omission нет; C01 остаётся единственным добавленным ID, denominator121 сохранён.

До фактической передачи пользователю **120/121=99.17%**, implementation116/116;
только C01 даёт0. Финальный ответ обязан содержать проверенные gates, оба verdicts,
долю и ограничения; его реальная отправка закрывает этот delivery requirement.
В этом отчёте преждевременное full100% не утверждается.

## Строки checklist: независимая оценка

Все пути ниже относительно repository root. Исторический порядок Contract-First
невозможно заново восстановить из одного dirty worktree; проверена согласованность
нынешнего публичного контракта и реализации, а не выдана новая временная гарантия.
Описание Contract-First сохранено как delivery evidence, без обещания production
atomicity или exactly-once. Тестовые fixtures имеют AAA-блоки и controlled clocks/
barriers; старые transport timeout smoke tests не являются доказательством новых
acceptance scenarios.

| ID | Статус | Проверенное доказательство / недостающий clause |
|---|---|---|
| T8-001 | выполнено | `RouteCall`, `RouteResult`, `RouteBinding`, generic `Selector`/`quota.Backend`; model поля только `policy/model`. |
| T8-002 | выполнено | Root `go.mod` без require; SDK остаются в nested ext; ordinary executable example без model/quota/observer. |
| T8-003 | выполнено | HTTP NewRouteHandler без options; PrepareRequest вместо lazy mutation; Dispatch требует validator; migration запрещает shims. |
| T8-004 | выполнено | Existing routing primitives сохранены; root race suite прошёл; исправленное lifetime/replay поведение имеет regression fixtures. |
| T8-005 | выполнено | execution-contracts описывает inputs/state/errors/ownership/order; экспортированные concrete API и AAA fixtures согласованы с ним. Временной порядок отдельно не реконструирован. |
| T8-006 | выполнено | Evaluation/Identity/Receipt/Admission — explicit arguments; policy не использует context metadata. |
| T8-007 | выполнено | Unknown status guard + shared ValidateRouteResult, Boundary validation, Sequence/Race config-control guard и atomic Race.finalize;100×race canonical/fatal matrices pass. |
| T8-008 | выполнено | Caller config/descriptors/rank/replay/backend; нет pricing/ledger/approval service. |
| T8-009 | выполнено | Selection/PinnedError/StatusError bounded; Tracing default только canonical action, fixed name и bounded status, без RecordError/raw caller fields. Explicit host AttributeProjection без request/payload/ownership и reserved override. Synthetic secret/full span output и safe projection AAA regressions, весь otel100×race pass; migration/closeout clear break documented. |
| T8-010 | выполнено | snapshot ownership doc; Selector.Freeze и model clone maps/pointers; independent References + ValidatePinned temporal tests. |
| T8-011 | выполнено | Два external-package ExampleBoundary с independent inferenceJob/stockQuery/warehouseFacts и Output assertions. |
| T8-012 | выполнено | Evaluation/Candidate/References, deadline, selector callbacks; selection_fallback fixture строит новый binding явно. |
| T8-013 | выполнено | Generic required Capability плюс window/residency/retention; model matrices; core предметные constraints не интерпретирует. |
| T8-014 | выполнено | Select Eligible/allows до Rank; unknown capability rejects; explicit fallback excludes preferred incompatible endpoint. |
| T8-015 | выполнено | Canonical binding, NoEligible/AffinityUnavailable, typed reasons/reference; no-eligible Dispatch calls=0. |
| T8-016 | выполнено | Cost units/currency + Measurement identity/source/time; task-scoped Quality; sanitized optional estimates. |
| T8-017 | выполнено | Metric enums FirstFragment/InterFragment/FullResponse/Throughput; percentile/window/sample/cache regime; metadata preservation test. |
| T8-018 | выполнено | Mandatory quality in Eligible before optional defaults; missing/wrong-task/stale/low fixtures; Ignore/Reject/DefaultOptional explicit. |
| T8-019 | выполнено | Evaluation fixed Now/Seed/SelectionID; stable declaration tie order; repeated binding/explanation fixture; host health facts. |
| T8-020 | выполнено | Invalid policy/floor/stale descriptor/estimate errors; required Dispatch validator; pinned TTL/scope/fingerprint rejection. |
| T8-021 | выполнено | SelectionHardEligibilityAndFreshDispatch rejects cheap schema candidate and exposes unsupportedReason. |
| T8-022 | выполнено | NoEligibleQualityAndStaleDescriptor asserts zero Dispatch calls and NoEligible. |
| T8-023 | выполнено | Changed References reject; pinned scope/fingerprint/TTL changes reject without silent reselect. |
| T8-024 | выполнено | Model hard constraint and temporal quality-window tests reject missing/stale score; defaults never satisfy mandatory floor. |
| T8-025 | выполнено | SequenceFallbackReselectsWithoutWeakeningHardConstraints capability/residency cases; incompatible preferred B never ranked/called. |
| T8-026 | выполнено | Explanations deterministic fixture compares binding/reasons/seed/selection ID and excludes credential/descriptor facts. |
| T8-027 | выполнено | Identity/Event/Replay/Failure/Decision/ScheduleInput carry phase/commit/outcome/class/budget/time/evidence explicitly. |
| T8-028 | выполнено | Coordinator monotonic phases and independent commit; skipped phases allowed; malformed/regressive transitions rejected. |
| T8-029 | выполнено | Boundary local return keeps Unknown unless explicit Record proof; LocalFailureRetainsUnknown and partial matrix. |
| T8-030 | выполнено | Decide/Schedule typed Retry/Fallback/Stop/Reconcile/Defer and Event/time; host Next supplies binding; no background scheduler. |
| T8-031 | выполнено | Duplicate/invalid identities/transitions/budget/context tests; canonical action errors не классифицируются/retry, shared fatal gate и finalization проверены100×race. |
| T8-032 | выполнено | committed gate; ConsumerResetDoesNotReplaceReplayEvidence no reset/reset alone/reset+duplicate evidence and original facts retained. |
| T8-033 | выполнено | Unknown default stop/reconcile; SafeDuplicate explicit; unknown quota Pending not automatic refund. |
| T8-034 | выполнено | Coordinator/Receipt/Journal mutexes; concurrent duplicate events; late unknown→Completed and idempotent settlement. |
| T8-035 | выполнено | Shared nested Coordinator budget/independent quota test; NestedAttemptsKnown false preserved; migration sole retry ownership. |
| T8-036 | выполнено | Boundary before/after admission, Sequence after Wait, Race before branch and core retry/fallback cancellation checks. |
| T8-037 | выполнено | Explicit Lifetime/Receipt/Trace; Router discarded ownership и projection meta owner; canonical malformed Boundary results сохраняют partial/match/lifetime/Unknown; regression matrices pass. |
| T8-038 | выполнено | Generic FirstCompleted remains terminal-payload race; caller semantic predicate in opt-in Race only. |
| T8-039 | выполнено | Race Permissions/Workers/shared Coordinator; independent Boundary admission; bounded resource fixture; no distributed guarantee. |
| T8-040 | выполнено | CompleteOnly rejects open handle; Accept caller callback; Committed rejected before/after callback; output hold host contract. |
| T8-041 | выполнено | CompleteOnly vs explicit EarlyOwned, caller buffering, no unbounded core buffer. |
| T8-042 | выполнено | Race early winner/late loser fixture cancellation, loser cleanup/Pending, winner alive until own close, late overage. |
| T8-043 | выполнено | Missing duplication evidence rejects multiple plans before admission/dispatch; read-only or DuplicateEffects contract. |
| T8-044 | выполнено | No delayed hedge implementation; exclusion documented. |
| T8-045 | выполнено | Новый pre_dispatch_test.go: classified preflight !Started/calls0/NotExecuted; explicit Decide/Schedule/Wait; новый physical ID same operation, бюджет2→1→0 и ровно одна отправка; 10 race прогонов. |
| T8-046 | выполнено | SequenceUnsafePartialMatrix visible partial calls=1, payload/Failure.Err retained, Unknown; reset no-reset fixture. |
| T8-047 | выполнено | BoundaryLocalFailureRetainsUnknown and unknown stop/reconcile matrix, no guessed NotExecuted. |
| T8-048 | выполнено | Buffered unknown committed=false does not authorize retry without SafeDuplicate; partial retained. |
| T8-049 | выполнено | CancellationAroundBackoff zero/minute Wait cases no Next; core zero/nonzero retry/fallback checks. |
| T8-050 | выполнено | Hint30/deadline5 Sequence and actual HTTP boundary fixture return Defer, never wait/second dispatch. |
| T8-051 | выполнено | Invalid hints reject/ignore-with-reason; explicit A/B cooldown isolation; uncertainty/time overflow tests. |
| T8-052 | выполнено | Coordinator malformed events no mutation; Boundary duplicate/budget; Sequence malformed lifecycle one dispatch. |
| T8-053 | выполнено | Race invalid result closed/settled before valid accepted; independent identity/reservations; acceptance doesn't publish. |
| T8-054 | выполнено | Race permissions zero calls; denial/budget zero dispatch; concurrency fixture max2 across4 plans. |
| T8-055 | выполнено | Early winner/late cancelled loser Pending with no refund, exactly-once cleanup and live winner until close. |
| T8-056 | выполнено | Concurrent duplicate Record and late definitive events; stable settlement/reconcile no double measured usage. |
| T8-057 | выполнено | ReserveRequest scope/identity/estimates/fingerprint/deadline; Reservation admitted/denied/deferred/reason/RetryAt. |
| T8-058 | выполнено | Settlement identity/actual/completeness; ReleaseProof; unknown/incomplete Pending; quota tests. |
| T8-059 | выполнено | Reserved/Committed/Released/Pending; local transition/conflict guards; independent-session finalization and lost ack fixtures. |
| T8-060 | выполнено | Generic comparable Unit maps, undeclared-unit error, no pricing/currency conversion engine. |
| T8-061 | выполнено | Duplicate Reserve same handle; changed facts conflict; concurrent Commit/Release exactly one finalize; changed complete values rejected. |
| T8-062 | выполнено | Backend atomicity/idempotency/TTL responsibility documented; ReserveError UnknownAck identity preserved; no automatic new reservation ID. |
| T8-063 | выполнено | Unknown/incomplete retains Pending; expiry returns UnknownHandle/Pending rather than refund; Release needs proof. |
| T8-064 | выполнено | Actual cloned unchanged; estimate1→actual4 and execution actual5 tests; no truncation. |
| T8-065 | выполнено | Predeclared FailClosed/FailOpen; classified definitive unavailable only may Unreserved; UnknownAck fail-open rejected. |
| T8-066 | выполнено | BackendUnavailable/Conflict/IncompatibleUnits/UnknownHandle error identity; atomic backend authority not observer. |
| T8-067 | выполнено | AtomicAdmissionAndDuplicateIdentity simultaneous A/B reservations capacity1 exactly one admitted. |
| T8-068 | выполнено | Applied commit lost ack, same ID/values retry, stored usage4 unchanged, two calls one effect. |
| T8-069 | выполнено | Unknown cancelled loser Pending; stream close without terminal usage retains quota; no automatic Release. |
| T8-070 | выполнено | BoundaryDeniedDeferredAndCancellation zero dispatch typed Denied/Deferred; race denial fixture. |
| T8-071 | выполнено | IndependentSessionsRejectConcurrentFinalization shared backend distinct session mutexes, one success/one Conflict. |
| T8-072 | выполнено | Lost-ack overage, complete/incomplete/release expired handle Pending, incomplete usage not zero. |
| T8-073 | выполнено | BoundaryQuotaFailurePolicyControlsDispatch checks fail-closed0, fail-open1/Unreserved, UnknownAck0 and same Receipt ID. |
| T8-074 | выполнено | Дополненная quota integration проверяет второй admission до close первого Lifetime: Denied/!Started и первая state Reserved/live1; прежний Pending scenario сохранён; 10 race прогонов. |
| T8-075 | выполнено | Affinity key/scope/trusted scope/strength/fingerprints/expiry/callback; cache estimates can remain caller descriptor/rank fields. |
| T8-076 | выполнено | Required compatibility callback mandatory, matching name not portability; temporal compatibility change rejected. |
| T8-077 | выполнено | Hard eligibility + required affinity before soft preference/rank; Boundary freshness/admission/cancellation not bypassed. |
| T8-078 | выполнено | Canonical binding and bounded Preserved/PreferenceBypassed/RequiredUnavailable. |
| T8-079 | выполнено | HostRebuild controlled authorization, re-eligibility/fresh refs/new physical ID/host lineage and separate usage. |
| T8-080 | выполнено | Unavailable preference falls to eligible candidate; no cache-hit/TTL promise. |
| T8-081 | выполнено | Preferred expiry bypass vs required expiry error; unknown required compatibility rejects; affinity weakening stamp rejected. |
| T8-082 | выполнено | Required trusted mismatch/missing compatible metadata error; pinned substitution zero dispatch; foreign host rebuild no transform. |
| T8-083 | выполнено | Host state/history only; no sessions/store; Explanation omits opaque descriptor and credentials. |
| T8-084 | выполнено | Required unavailable A cannot select B; canonical status AffinityUnavailable; explicit rebuild authorization needed. |
| T8-085 | выполнено | Unavailable preferred endpoint returns eligible binding + PreferenceBypassed. |
| T8-086 | выполнено | Preferred incompatible residency rejected before Rank in fallback matrix. |
| T8-087 | выполнено | Trusted scope mismatch rejects, current candidate scope substitution dispatch0; foreign rebuild transform0. |
| T8-088 | выполнено | Required/preferred expiry differentiated; temporal required expiry unchanged refs rejects; no stateless weakening. |
| T8-089 | выполнено | Authorized transform+Select+ValidatePinned+Boundary new ID/lineage/usage10; denied/foreign/ineligible cases don't dispatch rebuilt state. |
| T8-090 | исторический context | Предыдущая штатная проверка не implementation requirement; исключено ровно как исходный checklist. |
| T8-091 | исторический context | Предыдущая репродукция erroneous snapshot, не новая deliverable. |
| T8-092 | исторический context | Историческая DATA RACE репродукция, не требование получить FAIL сейчас. |
| T8-093 | выполнено | Permanent AAA lifetime/HTTP regressions assert corrected behavior, not defective baseline. |
| T8-094 | исторический context | Историческая неподтверждённая гипотеза не превращена в новый BUG. |
| T8-095 | исторический context | Описание состояния при создании ТЗ; реальные gates входят отдельно. |
| T8-096 | выполнено | Explicit delivery commit/failure classification/retry ownership/stale policy/cancel accounting/backend atomicity contracts. |
| T8-097 | выполнено | Main BUG fixes exist with permanent regressions; Boundary one-attempt phase/outcome/cancel fixtures. Детальные пробелы A01/A02 не замаскированы. |
| T8-098 | выполнено | Scheduling implemented; safe race per-branch admission/settlement fixtures. |
| T8-099 | выполнено | All four active applicability matrix; no silent conditional deferral. |
| T8-100 | выполнено | Contract/code/AAA/migration и теперь public quotaReconciliation/authorizedAffinityRebuild examples: external package, Output checked10×race. |
| T8-101 | выполнено | После shared validation/finalize production fixes независимые make test/make lint terminal-pass всех9модулей; Makefile discovery/race неизменны, controlled barriers/clocks. |
| T8-102 | выполнено | Ordinary caller-owned operation example and generic composition fixtures, no SDK/storage/transcript/observer requirement. |
| T8-103 | выполнено | Source sections/card coverage matrix retained; lifecycle/schedule/race/exclusions/release/closeout not silently omitted. |
| T8-104 | выполнено | Migration and author draft explain eligibility/ranking/caller facts/fingerprints/stale policy/NoEligible. |
| T8-105 | выполнено | Phase/outcome/commit, sole retry owner/budget, hints/scope, partial/unknown/defer, accepted predicate/Lifetime migration. |
| T8-106 | выполнено | Atomic host port/per-attempt reserve/actual/completeness/stable settlement/pending/overage caller migration. |
| T8-107 | выполнено | Required/preferred/trusted scope/freshness/explicitly authorized rebuild migration. |
| T8-108 | выполнено | Before→after HTTP signature/preparation, ownership/retry/cancel behavior; no recommended legacy shims. |
| T8-A01 | выполнено | One/multiple winners/errors/late cleanup; новый live-winner test доказывает parent cancellation до close при closes0, затем два Close дают cleanup1; 10 race прогонов. |
| T8-A02 | выполнено | request_ownership_test.go original bytes/closes и каждый independent attempt reader full bytes/exactly-one close; prepared stream/ready factory10×race; prior failure/limit zero-send guard retained. |
| T8-A03 | выполнено | POST/PATCH default 503 single write; explicit VerifiedDeduplication two attempts/one effect; unprepared repeat rejected. |
| T8-A04 | выполнено | Core initial/after-error zero/nonzero retry; Fallback/PredicateFallback; Boundary after reserve and Sequence after Wait no dispatch. |
| T8-A05 | выполнено | No cache/training/control plane/ledger/hedge/rollback/exactly-once engine; docs explicit exclusions. |
| T8-A06 | выполнено | Public ownership/error/order contract and executable fixtures aligned; historical chronology limitation stated above. |
| T8-A07 | выполнено | Этот самостоятельный отчёт: source/checklist/code/tests, omissions, every row, fractions and evidence. |
| T8-A08 | выполнено | Root получил и интегрировал независимый verdict: CA-001–006 resolved/open0, tracing privacy/ownership100×race. Его source/report намеренно не читался мной; handoff/checklist integration проверены. |
| T8-A09 | выполнено | Root свёл оба независимых verdicts, все confirmed findings resolved/re-audited; мои substantive findings closed. Handoff/checklist содержат final closeout без скрытых deferred cards. |
| T8-A10 | выполнено | Main final gates76405/11316 terminalpass root+8extensions lint0issues/test-race; no production edits afterward. Handoff содержит точные проверки/ограничения; strengthened otel fixture independently100×race pass. |
| T8-A11 | выполнено | Два external public API executable ExampleBoundary с unrelated caller types и checked Output. |
| T8-A12 | выполнено | Release-break существенный clear break; patch только отдельные minor; никаких release commands аудитор не запускал. |
| T8-A13 | выполнено | Unpublished closeout draft all cards/bugs/migration/evidence/limits; closure requires separate command. |
| T8-A14 | выполнено | HTTP hints and core time suites receipt/provenance/uncertainty/past/overflow/invalid/deadline/scope; replay independent. |
| T8-A15 | выполнено | Boundary before/after Fresh+cancel+admit, no-dispatch proof release, intermediate Lifetime close before Wait, stream permits owned. |
| T8-A16 | выполнено | Migration каждого RTR и теперь public-API quota/affinity runnable examples с checked Output и migration references;10×race pass. |
| T8-A17 | выполнено | Все4карточки active с public contracts/compositions/AAA/examples/migration; Boundary/Race control findings проверены corrected fixtures100×race; silent deferral отсутствует. |
| T8-C01 | не выполнено | Содержимое docs/task8-handoff.md проверено и готово; фактический final response пользователю ещё не отправлен, заранее не засчитывается. |

## Ограничения и итог первого прохода

Тестовые atomic backends — только conformance fixtures, не production store. Host
обеспечивает consistency, TTL, reconciliation, pricing, transport facts, consumer
reset и authorized rebuild. Это корректные архитектурные границы, а не deferred
RTR-003/004. Unknown/cancelled remote work остаётся потенциально billable/effectful.

Структурных пропусков roadmap или искусственного исключения условных карточек
не найдено. Baseline checklist содержал несколько более слабых acceptance proofs;
все они усилены прямыми fixtures и independently re-audited. Новые substantive
fixes и fresh all-module gates тоже проверены. Третий checkpoint116/116 был
историческим, не финальной приёмкой: privacy counterexample T8-009 затем исправлен
и независимо проверен. Текущие **implementation116/116**, **pre-delivery120/121**.
Интеграция обоих verdicts и final gates завершены. Только actual final user handoff
остаётся незавершённым и не засчитывается по обещанию. C01 — единственный
добавленный omission ID; privacy clause уже находится в исходной спецификации.
