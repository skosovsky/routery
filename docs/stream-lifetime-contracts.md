# Lazy stream ownership

`stream.Owner[Event]` is an opt-in, dependency-free bridge between a caller-owned
`iter.Seq2[Event, error]` and the canonical `routery.Lifetime`. It owns consumption;
never consume the underlying source separately after handing it to the owner.
The root routing package and its existing Lifetime contract are unchanged.

## Ports and state

`stream.New(events, cancel, discard)` requires non-nil ports. `cancel` requests
cancellation without waiting for consumption, is callback-safe and must not panic.
`discard` releases resources allocated before consumption; it is synchronous,
never starts useful work, and must not panic. If creation allocates no resource,
pass an explicit no-op. Source cleanup must execute before the events iterator
returns, including error, early break and panic paths. These are caller obligations,
not properties inferred from an arbitrary Close method.

Created → running → completed is the normal path. `Cancel` atomically prevents a
created owner from starting, or marks a running owner stopping. A created owner
then executes cancel and discard and completes without starting the iterator.
A running owner requests cancellation; completion waits for iterator unwind.
Start versus cancellation is serialized under a mutex. Only one consumption is
allowed; every second or canceled-before-start consumption yields `ErrConsumed`.
No producer goroutine, background drain, buffering, retry or fallback is created.

`Cancel` is safe from an event callback because it never waits for an active
iterator. Its caller-supplied cancel port must also be nonblocking and must not
reenter the owner. Created-owner discard can block; there is no callback in that
state. Cancellation is invoked exactly once. `Close` delegates to `Lifetime.Close`,
requests cancellation, waits for actual completion and returns joined cancel and
discard errors. **Never call Close or Lifetime.Close from a consumer callback,
source cleanup, discard, cancel port or lifetime hook.** They could wait for their
own stack. Use Cancel from event callbacks, return from iteration, and Close outside.

`Done` closes after source iterator return and cancel/discard completion. It proves
resource cleanup, not execution of all Lifetime hooks. On normal return, early
break, consumption error or source panic, the owner automatically closes its
Lifetime after publishing Done. An external Close can wait on Done concurrently
without blocking source completion. Use Close to wait for hooks and cleanup errors.
A source panic propagates after source defers and ownership cleanup run; cleanup
ports and Lifetime hooks must not panic. An uncooperative iterator can keep Close
blocked and its permit occupied indefinitely; no timer fabricates cleanup completion.

Errors yielded by the iterator remain consumption errors; Close reports cleanup
errors. The caller joins dispatch, consumption and cleanup failures. Result copies
share the same owner and Lifetime. Attach `owner.Lifetime()` to every owned result,
including payload plus handler error. Intermediate results discarded by routing
are closed before another attempt; final partial results belong to the caller.

## Routing composition

`Timeout` retains its context when the handler returns an owned result. The
original deadline and parent cancellation remain effective; values and explicit
request requirements and match metadata are preserved. The source must honor this
context, and the consumer must finish or explicitly close the owner. Parent
cancellation alone cannot prove cleanup of an iterator that has not returned.

`Bulkhead` keeps its permit until cleanup completion, in either nesting order with
Timeout. A terminal frame or cancel-only Close does not release the permit. A bare
lazy payload is interpreted as a value, so its timed context is canceled on handler
return. `NewLifetime(source.Close)` is suitable only if Close proves completed cleanup.

`RetryIf` sees handler-return errors only. A consumption failure after stream handle
return never triggers an implicit retry/fallback. The host chooses one retry owner;
unknown remote outcome, cancellation and no visible output do not prove safe replay.
The invocation tracing span ends when the handler returns. A host may add a separate
stream lifecycle span; resource ownership does not extend invocation tracing.

## Optional concrete bridge and conformance

`ext/prompty.New` accepts a created, unused real stream and binds its Events and
cancellation port. A used/canceled stream is rejected before transfer. A newly
created stream dispatches lazily and allocates no source transport resources, so
its unused discard port is a no-op. Any separately acquired host resource needs a
host-owned discard port instead. The concrete bridge intentionally does not expose
a second path to consume the source; retain its Status only for partial snapshots.

Run `make stream-conformance` against the checked-out router and published stream
source. To test local source changes too, run
`make stream-conformance STREAM_SOURCE_DIR=/path/to/source`.
The script builds isolated temporary modules/workspaces, never modifies the source
checkout, and runs the same semantic fixtures with race in both modes. Published
source mode sets GOWORK=off, downloads the declared released dependency and checks
that it has no replacement; the router and concrete bridge under test are copied
into that module so the new code is actually tested before release. It does not
claim the new bridge is already published. CI fetches the explicit audited source commit from
`ext/prompty/conformance-source-ref.txt` into a temporary workspace and also tests
the published source in isolation. Update that ref after auditing a new local
source revision, or pass `STREAM_SOURCE_REF` to test another remotely available
commit. The remote default branch currently predates the Stream API; checking it
is a different, unsupported contract and fails compilation rather than falling
back. Local source mode always uses the caller-supplied checkout, including local
changes; it does not replace them with the CI snapshot.
