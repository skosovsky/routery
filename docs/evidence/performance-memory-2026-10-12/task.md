# Routery: убрать allocations на rejected routes и дать управление expired cooldown state

Статус: реализация выполнена. Контракт, regression tests, команды и raw evidence: `docs/evidence/performance-memory-2026-10-12/`. Окончательные независимые verdict фиксируются отдельно на immutable candidate SHA; после приёмки исходники не изменяются.

## Цель задачи

Снизить transient allocations dispatch при большом числе несовпавших маршрутов и обеспечить освобождение устаревших cooldown scopes в долгоживущих экземплярах. Сохранить BYOT, результаты маршрутизации, изоляцию metadata и действующие временные ограничения.

## Scope

Только routery: `route_table.go`, связанные dispatch/ownership tests; `policy/attempt/schedule.go`, lifecycle API/документация/test suite. Не изменять consumers, зависимости, release/pins; не переписывать policy selection, retry, quota и adapters без нового подтверждённого дефекта. Начать с контракта допустимого cleanup cooldown state и регрессионных AAA тестов.

## Подтверждённые причины

1. `routeEntry.matchRoute` создаёт `Path` через `appendRouteID` после каждого вызова matcher, даже при `ok == false && err == nil`. `dispatchEntry` такой match сразу отбрасывает. Объём временных выделений растёт с числом rejected routes, хотя ни handler, ни sink их не наблюдает. Устранить материализацию metadata для чистого miss; при ошибке matcher metadata должна сохраниться.
2. `Cooldowns` содержит map scope → deadline, `Extend` только увеличивает bound, `Until` только читает. Даже уже истёкшие deadlines для более не используемых scopes остаются достижимыми до уничтожения всего объекта. В API нет удаления/очистки. Это ограничение lifecycle при высокой кардинальности scopes, а не нарушение обещанного TTL eviction и не доказательство роста памяти на фиксированном наборе ключей. Нужен явный механизм безопасной очистки истёкших scopes с caller-owned временем; автоматический eviction активных deadlines недопустим.

## Evidence

Source SHA: `1b167e69d9bb1c4d04b3db306393e94ba22484ed`, исходный checkout чистый. Evidence: `ai-libs/reviews/library-performance-2026-10-12/routery/` рядом с репозиторием: README с точной командой, два AAA diagnostic test files и `baseline.log` с raw results. Изолированный исходник `/tmp/routery-performance-audit-20261012`; setup построения таблиц вне benchmark timer. Core tests не покрывают вложенные ext modules.

### Измеренный baseline

Go 1.27.2, darwin/arm64, Apple M1 Max; три samples каждого размера:

| Несовпавших routes | median ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| 1 | 157.9 | 24 | 2 |
| 64 | 4609 | 1032 | 65 |
| 1024 | 72769 | 16392 | 1025 |

Диагностические AAA тесты прошли: 1→1024 маршрута дают 2→1025 allocs, то есть +1 allocation на rejected route. Все 1k/10k/100k expired scopes остались в map после полного прохода `Until` и GC. Все девять packages корневого `go test ./... -count=1` прошли; race на этом этапе не запускался. Проверены individual PASS/ok секции, а не только exit code последней shell-команды.

## Измеримые критерии

- В frozen `BenchmarkAuditMissDispatch` на 1/64/1024 predicate routes после setup чистый all-miss dispatch: не более 1 alloc/op, 8 B/op (существующая постоянная стоимость сверх `Path`); число маршрутов не должно увеличивать allocations. Линейный CPU scan допустим: индексирование predicate callbacks вне scope.
- Одна машина/toolchain, три или более samples before/after, `-benchmem`. Не допустить устойчивого >10% ухудшения median ns/op для любого размера; при шуме повторить изолированно. Добавить successful last-route, mixed exact/prefix/predicate, nested, error matcher и sink benchmarks: до изменения снять baseline, после сравнить; allocations/B не выше baseline, CPU median не хуже 10%.
- Для 1k/10k/100k distinct expired scopes после предусмотренного контрактом cleanup reachable entry count = 0; для смеси expired/active count = число активных scopes. Это критерий освобождения записей, не требование немедленного падения RSS. Проверить много последовательных циклов churn/cleanup: число retained entries определяется живым набором, не всем историческим числом ключей.
- Если cleanup сохраняет большую capacity пустой Go map, отдельно измерить retained heap после GC и предусмотреть reclaim backing storage при большом shrink; не выдавать уменьшение `len` за освобождение всей capacity. Не вводить фиксированный RSS threshold.
- Сохранить существующий контракт `Until`/монотонности для активных scopes; явно документировать semantics после cleanup, expiration boundary и источник времени. До cleanup старые публичные методы не должны неожиданно начать зависеть от wall clock. Scoped isolation; конкурентные Extend/Until/cleanup; активное более позднее продление не теряется из-за cleanup race.
- Все core tests и `go test -race ./...` на одном candidate проходят. Нет отключённых проверок, ослабления ownership/authority/lifetime контракта ради цифр. Diagnostics, которые утверждают наличие старого дефекта, заменить regression expectations; не считать их падение доказательством ошибки новой реализации.

## Инварианты

Порядок и приоритеты маршрутов, cancellation, fallback, decision reasons и error metadata неизменны. Caller/handler/sink/result не должны разделять изменяемый `Path` там, где контракт обещает изоляцию. Сохранять matcher execution count/side effects; чистый miss не превращать в terminal result. Cleanup не снимает активные cooldown ограничения, не удаляет соседний активный scope, не создаёт фоновых goroutines/timers без необходимости и явного lifecycle.

## Приёмка двумя независимыми субагентами

Проверять один immutable candidate SHA с идентичными evidence и командами.

- A — полнота: отдельный checklist каждого критерия; процент = выполнено / применимо ×100; требуется **100%**, каждый пункт подтверждён тестом/замером/контрактом. Не исключать пункты как N/A без обоснования.
- B — отсутствие ошибок: независимое adversarial review routing misses/errors/nesting/sink mutation и cleanup monotonicity/expiry/concurrency; повторить meaningful tests/race. Требуется **0 найденных неисправленных ошибок**; это результат проверки, не абсолютная гарантия отсутствия всех возможных ошибок.

После исправлений оба reviewer-а перепроверяют итоговый SHA. Не закрывать задачу только по снижению allocs, только по unit tests или по чужому benchmark summary.


## Локальный пакет доказательств и правила завершения

Самодостаточный пакет исходного аудита расположен рядом с задачей: `.cursor/tasks/evidence/performance-memory-2026-10-12/` от корня этой библиотеки. Он содержит probes, raw logs и описание исследования; упоминания `ai-libs` и `/tmp` выше обозначают исходное место аудита, а не обязательные зависимости будущих проверок. Репозиторий библиотеки является единственным scope реализации.

Для воспроизведения создать изолированный checkout указанного baseline SHA, перенести probe-файлы в пакеты, указанные в report/README, и выполнить сохранённые команды с `GOWORK=off` и writable `GOCACHE`. Сохранённый `audit-run.py` сериализует Go-процессы, если воспроизводятся несколько аудитов: вызвать `python3 <путь-к-evidence>/audit-run.py go test ...` из нужного пакета/модуля. Установки cache/lock сами по себе не влияют на контракты библиотеки. Benchmark запускать отдельно от компиляции/race/другой тяжёлой нагрузки. Если baseline требует свою Go toolchain, сохранить её для candidate comparison.

Исходные characterization tests намеренно фиксируют старую неэффективность/retention. Их PASS подтверждает finding, а не завершение задачи; после исправления создать regression assertions на целевой контракт, не сохранять ожидание старого дефекта. Перенести постоянные regression tests/benchmarks в обычные test packages библиотеки. Не переносить затраты measured invocation в fixture setup ради gate.

Для окончательной приёмки заранее зафиксировать атомарный список обязательных требований. Reviewer A считает `100 × PASS / total`; FAIL, NOT RUN и частичные результаты не засчитываются. Reviewer B независимо ищет correctness/concurrency/compatibility ошибки и контрпримеры. DONE требует 100%/APPROVE A, APPROVE B без найденных неисправленных ошибок, все обязательные gates/checks PASS на одном final commit/tree. Новые изменения после verdict требуют повторной проверки затронутых требований обоими. Не менять знаменатель/пороги задним числом ради приёмки. Указать ограничения проверки: отсутствие найденных ошибок не является абсолютной гарантией.

Эта постановка не разрешает публикацию релиза, обновление consumer pins, изменения соседних библиотек или ослабление защит ради производительности. Исходная постановка была составлена до реализации. Результат выполнения и ограничения проверки описаны в `docs/evidence/performance-memory-2026-10-12/README.md`; публикация релиза и обновление consumers не входят в эту задачу.

## Goal

```text
Выполни .cursor/tasks/performance-memory-2026-10-12.md. Доведи до приёмки двумя независимыми субагентами: полнота 100%, отсутствие найденных неисправленных ошибок.
```
