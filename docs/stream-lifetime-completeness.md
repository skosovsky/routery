# Task9 independent completeness acceptance

Review date: 2026-10-07. Final implementation review after correction of initial
coverage gaps. Scope: `.cursor/docs/task9.md`, tracked diff, new `stream/`,
`ext/prompty/`, OTel fixture, portable conformance script, contract and migration.
This reviewer did not change production code. Every AC counts only when its whole
semantic requirement and execution gate are supported.

**Accepted implementation: 9 / 9 = 100%.** Release and issue closeout are separate
pending operations; this percentage does not claim the overall goal complete.

| Criterion | Semantic evidence inspected | Execution evidence / verdict |
| --- | --- | --- |
| AC1 negative fixtures | `TestIncorrectBareAndCancelOnlyComposition` reproduces handler-return cancellation and cancel-only premature release, explicitly labeled incorrect ownership. `TestRealBareAndCancelOnlyNegative` reproduces both with real Stream. | Generic/real shared conformance and all-module race runs PASS. **PASS** |
| AC2 correct ownership | `contextFacts` checks live post-return context, values, caller-owned explicit requirements and chosen route metadata. `timedCancellationCase` starts consumption after return through Timeout, checks exact original earlier deadline and active parent/deadline cancellation. `paths` asserts one hook, balanced source starts/cleanup; copied result contains same owner/Lifetime pointer by construction. | Both source dependency modes with race PASS. **PASS** |
| AC3 gated cleanup | Shared `gated` holds source defer after cancel, asserts Bulkhead full and zero hooks in both Timeout/Bulkhead orders, then probes successful admission after unwind. Shared `terminal` denies Done/hooks during last callback. `realTerminalCloseout` holds real source lifecycle closeout after delivered StreamTerminal, asserts Bulkhead full in both orders. | Both dependency modes with race PASS. **PASS** |
| AC4 all endings | `paths` covers EOF, consumption error, unused cancel/close without source start, early break, callback Cancel, parent cancel, deadline and 20 concurrent Close workers behind actual source-start barrier; cleanup/hook counters, repeated Close, second consumption rejection and permit probes. `startRace` races Events/Close/Cancel 100 times behind a barrier, joins all workers and probes permit admission. Active parent/deadline tests join the consumer explicitly. Generic unused discard test proves allocated unused cleanup executes once without source start. | All fixtures complete with race, all explicitly started workers joined. Both dependency modes PASS. **PASS** |
| AC5 partial/error ownership | Shared `partial` verifies RetryIf and Fallback cleanup before next attempt, retains final payload/owner with handler error, then closes final owner. Generic unused/running cleanup error tests join dispatch/consumption/cleanup errors without losing originals. Existing core cleanup-failure regression prevents retry after failed disposal. | Conformance plus standalone core/all-module race PASS. **PASS** |
| AC6 no hidden replay | `noReplay` composes RetryIf+Fallback and asserts one handler, zero fallback after yielded consumption failure. Executable ExampleOwner and contract assign one host retry owner; cancellation/no output/unknown outcome do not authorize replay. | Shared generic/real tests and ExampleOwner PASS. **PASS** |
| AC7 tracing boundary | `TestLazyStreamSpanEndsAtHandlerReturn` invokes existing tracing adapter with synchronous exporter, observes one completed span while owner Done remains open, then consumes/closes and still sees one span. | OTel test in complete `make test` race gate PASS. **PASS** |
| AC8 real integration + independence | Optional real Stream bridge executes shared semantic suite and a real lifecycle-observer gate; no fake-only substitution. Portable script copies checked-out router/bridge, tests full copied module `./...` with `-race -count=1` against published source under GOWORK=off, explicitly rejects replacement, then tests caller-supplied actual local source in isolated workspace. Root go.mod remains dependency-free; core does not import optional module/source types. | Complete published+local conformance exit 0. Independent root GOWORK=off `go test -race -count=1 ./...` exit 0. **PASS** |
| AC9 quality/documentation | Contract specifies creation/running/stopping/completion, exclusive source ownership, non-panicking non-reentrant cleanup ports, Done vs hook completion, callback-safe Cancel vs blocking Close, unused discard, uncooperative-source limits, distinct errors, context/retry/tracing boundaries. Migration/index/README and executable generic ExampleOwner match inspected APIs. | `make lint` exit 0 across all ten modules, zero issues; complete race-enabled `make test` exit 0. **PASS** |

## Proof and scope

- `/tmp/routery-task9-conformance.log`: full `go test -race -count=1 -timeout=90s
  ./...` copied-module runs, published source and actual current local source;
  root confirmed process session 74060 exit 0, and reviewer inspected both outputs.
- `/tmp/routery-task9-lint.log`: all ten modules report `0 issues.`; root confirmed
  final process session 23384 exit 0. Earlier failed logs were superseded by a full
  successful rerun after helper extraction/context argument order fixes.
- `/tmp/routery-task9-test.log`: all-module race-enabled `make test`; root confirmed
  process session 21798 exit 0; reviewer inspected final successful module output.
- Independent reviewer verification: task cache paths and `GOWORK=off go test
  -race -count=1 ./...` in root, process 72854 exit 0 across all root packages.

The published mode tests the checked-out new code against released source, not an
already published new bridge. Root is independently buildable; concrete runtime
imports remain in the optional module. `STREAM_SOURCE_DIR` tests actual local
source including its uncommitted changes. CI uses the explicit audited source ref
in `ext/prompty/conformance-source-ref.txt`, with `STREAM_SOURCE_REF` override;
it does not promise compatibility with unsupported remote default-branch APIs.
Default remote-fetch smoke also passed: `/tmp/routery-task9-conformance-portable.log` shows fetching and checking out the audited snapshot, published-source PASS and full snapshot-source PASS; root confirmed process 83918 exit 0, and reviewer inspected its output.

Initial review was 4/9 (44.44%): exact active deadline preservation, terminal
Bulkhead nesting, all-path permit probes, full conformance scope and lint success
were insufficient. Subsequent fixtures/full reruns resolved those findings;
criteria were not removed or weakened to obtain the final fraction.

## Release and closeout

Pending outside implementation AC: substantial public owner/optional bridge API
calls for `make release-break`. Preserve author-facing migration comment with
real API examples, successful release evidence, closed issue and comment/closure
URLs in `docs/stream-lifetime-closeout.md`. This review does not mark those external
operations complete or claim that the whole goal is achieved.
