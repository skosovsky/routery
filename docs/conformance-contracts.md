# Host integration conformance

policy/quota/quotatest is opt-in testing support, isolated from runtime packages.
Run accepts host-owned generic scope/unit/handle/reason types and a fresh Fixture
factory per scenario. Clients must be independent local handles sharing one durable
backend; fixture inspection must read authoritative state rather than local Session
caches. Each callback must honor its context and finish within the test deadline.
The suite cannot forcibly stop arbitrary uncooperative Go code. Close must join all
fixture-owned workers/connections. The request reserves one declared unit at quantity1;
the fixture has capacity for at least four distinct reservations. Scope is trusted.

Mandatory scenarios check concurrent deduplication, identity separation, conflicting
reserve inputs, exclusive concurrent Commit/Release, idempotent settlement, complete
overage, Pending and reconciliation. Scope reassignment may reject a globally unique
operation identity or reserve independently; it must never borrow another scope's hold.
Capability scenarios require explicit callbacks for applied-but-lost reserve/commit
acknowledgments, expiration and restart. Missing capability is Unsupported, never Passed.
Report.Complete distinguishes full coverage from RequiredPassed. Run logs unsupported
scenarios distinctly and fails confirmed errors; inspect the returned report when full
fault coverage is required. No skipped scenario is counted as evidence.

Before Reserve, inspection returns scope credits with Found=false (no error); this
provides the baseline for lost-ack hold accounting. Inspection retains identity, actual usage, durable application/finalization counts and
whether an unresolved hold is retained and available quota for the isolated test scope.
Pending/expiration cannot increase availability; complete usage4 on estimate1 charges
the additional3 units (available units floor at zero, actual usage never truncated). TTL alone must not appear as Released/zero usage.
Unknown acknowledgments retry the same identity/settlement and must not double-apply.
Expiration may tombstone a handle, but it must retain unresolved usage/hold knowledge.
Defaults are not a backend implementation and the reference fixture proves no crash
or production durability. Hosts can connect the factory to actual storage and supply
Restart that recreates independent clients after a real process/storage restart. Keep
fault injection at the acknowledgment boundary, after durable mutation, rather than
simply failing before write. Inspect actual persisted ledger records after recovery.

Router/RetryIf suffice for local synchronous routing with explicit replay evidence.
Boundary/Sequence add optional identities, admission, live outcome and coordinated
replay/reconciliation. No quota backend, OTel, SDK or agent domain schema is mandatory.
A returned stream handle is not completion. The application closes its canonical
Lifetime even on partial+error, then separately handles cleanup and receipt settlement
errors according to its own retry/reconciliation policy. Observability never closes
owners or releases concurrency permits. Cleanup uses an independent bounded context.

Logging and metrics callbacks run synchronously and may run concurrently. They must be
bounded, concurrency-safe and non-panicking. Logging Event contains raw Request, Err
and arbitrary caller metadata; PayloadMeta does not redact the whole event. Project
only explicit bounded fields; never serialize Event wholesale. OTel defaults remain
canonical action and bounded error status, excluding request/raw errors/IDs. Host
allowlists may correlate trusted operation/attempt/selection IDs as trace attributes,
never arbitrary IDs in metric labels. Invocation spans end at handler return; separate
lifecycle events/duration come from Receipt/Trace/Journal/Lifetime facts. Journal snapshots
are observations, not completion barriers; wait/join owned workers and retain late events.
