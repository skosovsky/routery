# Independent stream ownership correctness review

Review date: 2026-10-07. Scope: `.cursor/docs/task9.md`, current working-tree
`stream/owner.go`, semantic fixtures, optional concrete bridge, tracing regression,
portable conformance script and CI. Release and issue closeout remain outside this
review and were pending when reviewed.

Final infrastructure follow-up: the portable source gate now fetches the audited
commit recorded in `ext/prompty/conformance-source-ref.txt`. This is an explicit
supported source snapshot, not a claim that the remote default branch currently
contains the same API. Local checkout and remote-ref overrides remain explicit.

## Verdict

Ошибок не найдено в проверенной области. No confirmed production defect remains
from this review. This is a bounded review of the contracts and exercised schedules,
not a guarantee for arbitrary caller-provided iterators or callbacks.

## Code and contract assessment

- Consumption/start and cancellation are serialized under the owner mutex.
  Only the winning consumer starts the source; cancellation that wins before start
  performs synchronous discard without invoking the iterator. Result copies share
  the same owner and Lifetime.
- Cancellation is requested once. A running source closes `Done` only after its
  iterator and source defers have returned. The owner's `finish` publishes `Done`
  before calling Lifetime.Close: an external Close waiting for source unwind can
  therefore complete without deadlocking against the consumer's final Close.
- Cancel/discard error writes are ordered before `Done` publication; cleanup reads
  them after receiving Done. The cancel port's once operation also serializes
  competing cancellation and finish operations. Repeated Close observes Lifetime's
  once-protected joined cleanup error.
- Hooks and Bulkhead release follow actual cleanup, including source panic unwind.
  Callback-safe Cancel does not wait for active consumption. Close from the same
  callback, source cleanup, cancel/discard port or Lifetime hook is explicitly
  prohibited; the implementation does not pretend to support this recursive wait.
- A noncooperative source retains ownership until it returns. Cancellation and
  terminal frames do not fabricate completion. Partial handler results retain
  their owner, consumption errors remain separate from cleanup errors, and routing
  retry/fallback decisions operate on handler errors rather than later events.
- Root/core introduces no concrete source dependency. The optional adapter binds
  the real lazy API and rejects nil/non-created sources. Its no-op unused discard
  matches the reviewed real NewStream constructor: dispatch/transport acquisition
  occurs at consumption. Caller-created resources outside that constructor remain
  caller-owned.
- The existing tracing adapter ends its invocation span at handler return. The
  executable regression observes an open owner then confirms later cleanup does
  not end or export a second invocation span.

## Independent executable evidence

All commands below completed successfully using writable Go caches under
`/tmp/routery-task9-cache`.

1. `go test -race -count=30 -timeout=90s ./stream/... ./ext/prompty/... ./ext/otel/...`
   passed. This repeats both generic and real-source semantics and existing OTel
   tests under the race detector. Repeated against the final strengthened fixtures:
   stream passed in 31.476s, concrete bridge in 31.686s, OTel in 1.518s; no race
   report, panic or timeout occurred.
2. `STREAM_SOURCE_DIR=../prompty python3 scripts/stream-conformance.py` passed
   the isolated published-source mode (`GOWORK=off`, no source replacement,
   dependency `v0.15.0`) and the local current-source workspace mode (source commit
   `5607832ee869f63bc3c6768a02ed60a019cd691e`). Both modes actually execute the
   checked-out bridge and all copied root packages with
   `go test -race -count=1 -timeout=90s ./...`. The final expanded script was
   independently rerun successfully in both modes. The source implementations
   of NewStream, Close, Events, and lifecycle observation were inspected in
   `../prompty/stream.go` and the downloaded published module.
   After the audited-source pin was added, the default command
   `python3 scripts/stream-conformance.py` was independently rerun without either
   source override and exited 0. It fetched and checked out the recorded full SHA
   from the remote and passed full root/bridge race suites in both modes, proving
   portable execution does not require the author's sibling checkout.
3. Independent temporary probes in `/tmp/routery-correctness-probe` passed
   `GOWORK=off go test -race -count=100 -timeout=90s ./...`:
   - external Close after confirmed consumption start remains blocked while source
     defer cleanup is gated; Done and hooks remain unpublished until unwind, and
     both consumer and closer are explicitly joined;
   - a source deliberately ignoring cancellation keeps Done unclosed until a
     separate release barrier allows its iterator to return;
   - cancellation-before-start with gated discard rejects concurrent consumption,
     retains incomplete Done, and exposes the discard error to concurrent Close.

## Limits and coverage observations

The final permanent fixtures address the initial active-deadline and
concurrent-close coverage observations. `timed_active_cancellation` uses an active
source, captures its live context, asserts the exact earlier parent deadline under
Timeout, observes parent cancellation or actual deadline expiration, joins its
consumer, and verifies Bulkhead admission resumes. The concurrent path now starts
20 closer goroutines only after the source-start barrier. All termination modes
(including consumption error) and the start/close race use a permit probe and
verify that completion does not leak its permit. The real terminal/closeout gate
is exercised in both Timeout/Bulkhead nesting orders. These final fixtures were
reviewed directly; no new confirmed defect was found.

The CI/default script's audited-source pin was also reviewed: published mode stays
isolated with GOWORK=off and rejects a source replacement; the second mode clones
without checkout, fetches the exact configured ref and detaches at FETCH_HEAD.
`STREAM_SOURCE_DIR` keeps the actual caller checkout (including changes), while
`STREAM_SOURCE_REF` selects a different remote ref only when a local directory is
not supplied. There is no compatibility shim or fallback to a different API when
the requested ref is unsupported. Documentation and the CI job name accurately
state audited snapshot plus published source, rather than latest remote main.
The observed remote default branch predates the required Stream API; its failure
is a known unsupported-source constraint, not a passing conformance result.

The shared partial retry/fallback fixture uses lazy, never-consumed intermediate
results. It proves synchronous discard before the next invocation; active source
unwind ordering is separately exercised by the gated cleanup fixtures/probes and
the implementation of Lifetime.Close. No real network provider, arbitrary
noncooperative external SDK, callback panic or ownership transfer performed twice
by a contract-violating caller is certified by these tests.

This review did not execute release targets, verify published bridge tags, post an
issue comment or close the issue. Those actions require separate final evidence.
