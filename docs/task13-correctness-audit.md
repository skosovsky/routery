# Task13 independent correctness audit

Baseline: `516396ba5400ad61fdbfb5e4f4870c50d06e422d`. Reviewed the current uncommitted production/test/API diff, task13 source requirements, acceptance contract, API choices, migration and closeout. This auditor did not change production code or add tests to the tree.

**Final verdict: нет открытых подтверждённых дефектов в проверенном scope.** One independently reproduced P2 was corrected and reverified before this verdict.

## Initial finding — P2: fallback starts secondary after cleanup cancels context

**Status: fixed and independently reverified.**

Both fallback variants now recheck Context.Err after successful discard. The original public repro now reports secondary=0, retained partial result and joined provider/cancellation errors. The added TestFallbackCleanupCancellationStopsSecondary exercises cancellation through an OnClose hook in both variants; a fresh uncached root/policy race suite passes after the fix. No further production changes were observed after that correction.

Affected paths: `routers.go:36–39` (Fallback), `smart.go:48–51` (PredicateFallback).

A primary error returns an owned partial result. The context is live at the cancellation check. Lifetime cleanup or a registered OnClose hook then cancels that context. After successful discard both combinators immediately invoke secondary without rechecking cancellation. A secondary that does not check context executes and returns success, losing the primary error and reporting nil despite known cancellation. This requires no data race or broken backend; synchronous cleanup can deterministically cancel context.

Public-API repro saved at `/tmp/routery-fallback-cleanup-cancel.go`, run with:

```sh
GOCACHE=/tmp/routery-review-gocache go run /tmp/routery-fallback-cleanup-cancel.go
```

Observed for both unconditional and predicate fallback: `secondary=1 payload="secondary" err=<nil> context=context canceled`.

Expected: no secondary call after cancellation has become visible during discard; return the canonical primary metadata/closed owner with ActionAbort and join provider error plus context cancellation. RetryIf already performs the corresponding post-discard check. Add a regression for cancellation from cleanup/OnClose in both fallback variants. Rechecking cannot eliminate all asynchronous cancellation races, but it closes this deterministic invocation after a known cancelled context.

## Checked paths and evidence

- R01 cleanup stop and error joining across all four combinators; final/early RetryIf cancellation and veto; primary metadata and owner retention. The finding above is additional coverage beyond the current pre-discard cancellation test.
- R02 both HTTP classification predicates are side-effect free; actual retry closes Lifetime once; final/veto/cancellation paths retain ownership.
- R03 all three Mongo writes preserve exact nonnil result and write-concern error; nil-result errors abort.
- R04 framed count and individual path segments prevent slash-boundary ambiguity, with repeatable identity.
- R05 threshold Build rejects nonfinite/out-of-domain values; dispatch confidence validates before matched/key gates and execution.
- R06 builder-identity ancestry rejects direct/indirect cycles while allowing repeated shared DAG subtrees.
- R07 context-aware serialization remains separate from State mutex; backend State callback is safe; concurrent commit serialized; unknown acknowledgement retains Pending and stable settlement identity; Release cannot finalize a complete settlement.
- R08 domain eligibility/reason is retained separately from library affinity disposition.
- R09 shell discovery checks exact anchored fuzz names, all failure statuses propagate, no-target is success.
- C01 Finish transfers before admission error handling; NotExecuted settlement joins failure and Receipt permits retry; unknown quota reserve acknowledgement creates no refundable Session.
- C02 Select/ValidatePinned normalize once before validation and all callback types to earliest nonzero deadline.
- API review: removed Async aliases and legacy names, typed ErrNoSelection, Redis invoker BYOT signature, negative config pre-execution failure, exact S3 classification, model prefix. OnClose publishes cleanup completion under mutex; concurrent Close synchronizes through sync.Once. Half-open handler/classifier panic defer releases only current-generation probe. OutcomeSink independently clones both Match paths while retaining documented borrowed payload/resource values.
- Consumer script inspected: local ZIP/mod/info proxy built from this checkout, rewritten matching module requirements, no local replaces/go.work, isolated module cache, all nine modules imported, core dispatch executed. This is meaningful evidence for corrected source release layout.

## Independent execution

- `GOCACHE=/tmp/routery-review-gocache make test`: PASS, all nine modules use `-race`; executable examples run as part of suites.
- `python3 scripts/test-fuzz.py`: PASS (none, one, multiple, list failure, build failure, target failure).
- `GOCACHE=/tmp/routery-review-gocache python3 scripts/consumer-smoke.py`: PASS; build and execution with GOWORK=off and all nine unpublished release-layout modules.
- `GOCACHE=/tmp/routery-review-gocache GOMAXPROCS=2 make fuzz FUZZTIME=1s`: PASS, all eight separately anchored targets including both HTTP and SQL targets.
- `GOCACHE=/tmp/routery-review-gocache go test -race -count=1 ./...`: PASS on the final corrected core/policy tree; the original nine-module race run covered unchanged adapters.
- `GOCACHE=/tmp/routery-review-gocache GOLANGCI_LINT_CACHE=/tmp/routery-review-lintcache make lint`: final fresh PASS, 0 issues in every one of nine modules. Earlier attempts encountered a concurrent-linter lock and then the new assertion before formatting; both are resolved.
- `git diff --check`: PASS.
- Historical task8–12 documents preserve original content after normalizing only moved relative/document references.

## Limits

No live cloud/storage services or durability proof; adapter doubles exercise mapping/ownership. One-second fuzz smoke is not a sustained campaign. Race suites cover executed interleavings, not arbitrary scheduling. Backend synchronous reentry into Session.Settle/Release and same-Lifetime recursive Close remain documented unsupported. Borrowed payload/errors/reasons/Lifetime cannot be made safe against host mutation with generic deep copies. This audit verifies current changed contracts and affected implementation paths; it does not prove absolute absence of defects throughout the library.
