# Frozen acceptance requirements

Fixed before implementation, 2026-10-12. Denominator: **24**; PASS only, no partial credit or unsubstantiated N/A. A = 100 × PASS / 24. Both reviewers inspect the same immutable candidate SHA and evidence; new source changes invalidate affected verdicts.

| ID | Mandatory requirement |
| --- | --- |
| R01 | Changes limited to routery routing/ownership tests, cooldown lifecycle API/docs/tests and evidence; no dependencies, consumers, releases/pins or unrelated policy/adapters changes. |
| R02 | Cleanup contract and AAA regression tests precede implementation. |
| R03 | Frozen BenchmarkAuditMissDispatch invocation/setup preserved at 1/64/1024 routes. |
| R04 | All-miss <=1 alloc/op and <=8 B/op at every size, independent of route count. |
| R05 | Same machine/toolchain; >=3 raw before/after samples with benchmem, isolated from heavy work. |
| R06 | All-miss CPU medians <=110% baseline at every size; noisy failures rerun isolated. |
| R07 | Successful last-route benchmark: allocation/bytes <= baseline and CPU <=110%. |
| R08 | Mixed exact/prefix/predicate benchmark: allocation/bytes <= baseline and CPU <=110%. |
| R09 | Nested benchmark: allocation/bytes <= baseline and CPU <=110%. |
| R10 | Error matcher benchmark: allocation/bytes <= baseline and CPU <=110%. |
| R11 | Sink benchmark: allocation/bytes <= baseline and CPU <=110%. |
| R12 | Cleanup removes all expired 1k/10k/100k distinct scopes. |
| R13 | Mixed cleanup retains exactly active scopes and their deadlines, including neighbors. |
| R14 | Many sequential churn/cleanup cycles retain only the live set. |
| R15 | Retained heap measured after GC; backing storage reclaimed for empty and large partial shrink; no RSS claim/threshold. |
| R16 | Existing Until and Extend monotonic active-scope semantics preserved; no implicit wall clock. |
| R17 | Cleanup semantics documented: caller clock, inclusive boundary, removed-scope reset/reuse, concurrency and costs. |
| R18 | Concurrent Extend/Until/cleanup safe; later active extension cannot be lost. |
| R19 | Routing order/priority, matcher counts/side effects, pure misses/fallback and cancellation preserved. |
| R20 | Error metadata and decision reasons preserved, including false+error. |
| R21 | Nested handler/sink/result/caller Path isolation preserved under mutations. |
| R22 | All core tests and race tests PASS on final candidate; no weakening/disabled checks. |
| R23 | Permanent regression tests/benchmarks in normal packages; old diagnostics kept only as historical evidence, not gates. |
| R24 | A independently checks 24/24; B adversarial review and independent meaningful tests/race APPROVE with zero found unfixed bugs on same final candidate. |

## Cooldown lifecycle contract

Add `(*Cooldowns[Scope]).PruneExpired(now time.Time) int`. Caller supplies a cutoff in the same time domain as Extend deadlines. No wall clock, background goroutines or timers. Deadlines <= now are expired; all later deadlines remain unchanged. Return the removed entry count. Until returns zero for removed/missing scopes. Extend remains monotonic while a scope is retained; after removal, a new Extend starts a new history (even a stale deadline can be inserted and pruned again). PruneExpired, Extend and Until serialize on the same mutex. An extension later than the cutoff survives whichever operation acquires the lock first. Cutoff may move backwards but cannot restore deleted history. Cleanup scans O(n), and when anything expires rebuilds from the live set with O(live) temporary storage; an empty set releases the map completely. Nil maps are initialized lazily by Extend. No-copy-after-use mutex rule applies.
