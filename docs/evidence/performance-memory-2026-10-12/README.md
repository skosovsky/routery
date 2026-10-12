# Performance and cooldown lifecycle evidence, 2026-10-12

Baseline source: `1b167e69d9bb1c4d04b3db306393e94ba22484ed`. Candidate is the
commit containing this evidence, named explicitly in the two external reviewer reports.
Frozen denominator: **24** requirements in `requirements.md`; no thresholds changed.

## Sequence and isolation

1. Read the task and original probes. Wrote the lifecycle contract and fixed 24-item
   acceptance list before implementation. Added AAA lifecycle and routing regression
   tests before changing production source.
2. Created baseline with `git archive` at the source SHA into
   `/tmp/routery-performance-baseline-20261012-final`. Copied the permanent benchmark
   files into this checkout, preserving the original BenchmarkAuditMissDispatch
   invocation/setup (only formatting changed). `benchmark-files.sha256` identifies
   the two exact benchmark files used by both before/after runs.
3. Copied baseline plus pre-implementation regression tests to
   `/tmp/routery-performance-red-20261012`. This captures the intended failing tests
   against original production code: missing PruneExpired plus per-miss metadata and
   allocation regressions. `red.log` records expected failures, not candidate defects.
4. Applied the three-line pure-miss fast path and explicit cooldown cleanup.
   Source compilation/tests and benchmarks serialize through the original
   `.cursor/tasks/evidence/performance-memory-2026-10-12/audit-run.py` and its shared
   `/tmp/library-performance-audit-20261012.lock`. Another library held that lock:
   queued baseline/red/checks ran only when it was released. Thus red tests were
   **authored and snapshotted before implementation**, but executed later against
   their preserved pre-implementation checkout. No competing Go workloads were
   started to bypass the lock. Benchmark timing excludes fixture construction but
   includes each dispatch invocation and outcome/error check.
5. Before/after each collect five samples per scenario, with benchmem and 500ms
   benchtime, same machine/toolchain/cache and no simultaneous tests/race/compilation
   within the shared audit lock. Raw toolchain appears in logs; machine in machine.txt.

Runner exports GOWORK=off and writable GOCACHE=/tmp/library-performance-gocache-20261012
unless already supplied by the caller. Commands below run from the repository root;
benchmark baseline first changes cwd to the preserved baseline checkout.

## Exact commands

```sh
python3 .cursor/tasks/evidence/performance-memory-2026-10-12/audit-run.py sh -ec 'cd /tmp/routery-performance-baseline-20261012-final; go version; go test . -run "^$" -bench "^(BenchmarkAuditMissDispatch|BenchmarkPerformanceDispatch)$" -benchmem -count=5 -benchtime=500ms'

python3 .cursor/tasks/evidence/performance-memory-2026-10-12/audit-run.py sh -ec 'cd /tmp/routery-performance-red-20261012; if go test ./policy/attempt -run "^TestCooldown" -count=1; then exit 1; fi; if go test . -run "^(TestMissDispatchAllocationsBounded|TestPureMissMatcherExecutesOnceWithoutMetadata)$" -count=1 -v; then exit 1; fi'

python3 .cursor/tasks/evidence/performance-memory-2026-10-12/audit-run.py sh -ec 'go version; go test ./... -count=1; go test . ./policy/attempt -run "^(TestCooldown|TestMissDispatch|TestPureMiss|TestMatcherError|TestNestedMiss)" -count=1 -v; go test -race ./... -count=1'

python3 .cursor/tasks/evidence/performance-memory-2026-10-12/audit-run.py sh -ec 'go version; go test . -run "^$" -bench "^(BenchmarkAuditMissDispatch|BenchmarkPerformanceDispatch)$" -benchmem -count=5 -benchtime=500ms'

python3 docs/evidence/performance-memory-2026-10-12/compare.py docs/evidence/performance-memory-2026-10-12/before.log docs/evidence/performance-memory-2026-10-12/after.log
```

For reproduction elsewhere, reconstruct baseline from its source SHA and copy the two
benchmark files from the candidate; absolute /tmp paths above are evidence locations,
not a runtime dependency. Original historical diagnostic probes remain historical
only. Permanent tests assert fixed behavior in normal root/attempt test packages.

## Storage measurement and limits

TestCooldownRetainedHeap performs GC and logs HeapAlloc deltas for a 100k-entry map,
after in-place deletion (control), and after PruneExpired, both empty and seven live
scopes. KeepAlive preserves the measured state. No fixed heap/RSS threshold is used:
GC/runtime noise can change small deltas. Deterministic tests require exact reachable
entry counts for 1k/10k/100k, live-neighbor preservation, 30 churn cycles and empty
map=nil. Source review verifies partial shrink rebuilds a new live-sized map rather
than retaining the old backing storage. No claim of immediate RSS reduction is made.

Core ./... excludes nested ext modules. This task changes no adapters/consumers or
module dependencies and requires core tests/race; it does not claim extension live
provider coverage. Two independent reviews with no found unfixed bugs are a bounded
verification result, not an absolute proof that no possible bug exists.

## Corrections during verification

An initial test harness named a nonexistent ActionHandled; the API uses ActionStop.
It was corrected in both baseline and candidate before the successful measurements.
Initial failed harness logs are preserved in /tmp/routery-performance-first-attempt;
they are not acceptance gates. Initial successful measurements (before-initial.log /
after-initial.log) also passed all eight gates. Lint then requested integer range syntax
and strconv.Itoa for fixture names/setup. The measured dispatch/check work stayed
unchanged; identical final benchmark files were installed in both checkouts and all
five samples repeated. The original frozen probe's semantics remain unchanged.

A new test initially assumed handler/result Path detachment. Baseline reproduction
in baseline-borrowing.log shows this was already unsupported: withMatch and WithMatch
assign Match shallowly. The original task qualifies ownership by existing promises;
only the observer paths explicitly promise detachment (sink.go, docs/api-choices.md #7).
Both independent reviewers confirmed that boundary. The corrected regression checks
sink isolation from both canonical views, then mutates borrowed handler/result metadata
and verifies fresh dispatch paths remain unaffected. No pre-existing test/assertion,
public ownership guarantee or frozen acceptance gate was weakened. This task does not
add a new handler/result detachment guarantee or its extra successful-path allocation.

Supplemental diff lint uses the repository configuration without disabling checks,
with GOLANGCI_LINT_CACHE=/tmp/routery-performance-golangci-cache and --new-from-rev
set to the original source SHA (HEAD before the candidate commit). It is serialized
through the same audit runner. Final per-package core/race and focused logs are in
checks.log; aggregate medians and gate results are in comparison.md.

## Final benchmark gates

See [comparison.md](comparison.md) and the complete [before.log](before.log) /
[after.log](after.log). Five final samples per size/scenario: every gate PASS. All-miss
1/64/1024 retains exactly 8 B and 1 allocation per invocation; latency changes
-13.30%/-25.61%/-23.69%. Other scenario allocation/byte counts never increase;
maximum CPU median regression is +2.35% (error matcher), below the frozen 10% limit.

The local .cursor task is globally gitignored. For a self-contained candidate,
[task.md](task.md) preserves the task, [original-miss-probe.go.txt](original-miss-probe.go.txt)
preserves the historical frozen benchmark for inspection, and [audit-run.py](audit-run.py)
is a byte-identical copy of the original runner. Standalone reproduction can substitute
`docs/evidence/performance-memory-2026-10-12/audit-run.py` for the historical .cursor
runner path in the exact commands above. The lock/cache/environment behavior is identical.
The .txt probe is archival and is never compiled as a candidate test.

Final core normal/race gates and supplemental diff lint: PASS. A final cosmetic
line-break correction in one test callback followed the successful core run; B repeats
normal/race on the immutable SHA, and A repeats the focused suite on that same SHA.
Both independent reviewer reports are external acceptance artifacts tied to that SHA;
no report-writing changes are made to the accepted commit.
