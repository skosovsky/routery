# Deterministic routing and health contracts

Route tables have one stable lexicographic order. Default: descending priority,
then prefix group before other kinds at equal priority, then descending prefix
length inside that group, then declaration order. LongestPrefixWins: all prefix
routes precede the other group, then descending prefix length, descending priority,
declaration order. The other group (exact/decision/predicate/mount) remains ordered
by descending priority then declaration order. Fallback runs after all eligible
routes fall through. These groups remove the cyclic pairwise comparator; no legacy
ordering mode remains. Extreme int priorities use comparison without subtraction.

Built topology fingerprints include framed typed static keys, priorities, IDs,
matcher kind, decision confidence, relative classifier memoization groups, built decision-table actions/reasons/terminal flags, sorted route order, recursive mount topology,
fallback presence and routing options. Opaque host DecisionTable implementations, matcher/handler function identity and mutable
caller descriptors are excluded. Hash is not proof of arbitrary code behavior.
Pointer/channel keys include process identity; use stable value keys for portable
fingerprints. Build freezes options and recursively compiles children; compiled
tables retain no mutable builder. Registry atomically publishes new snapshots and
never rewrites an old snapshot. Host binding revision/freshness must use the current
topology fingerprint explicitly; no automatic rebind is added.

Circuit breaker admissions carry a generation plus probe ownership. State transitions
invalidate previous admissions; old completion cannot change a new probe. Exactly one
half-open probe runs. Success closes it; classified failure opens it; cancellation or
excluded errors release the probe and leave half-open for another trial. Classifiers
run once per failed invocation outside the mutex. Handler return measures local health,
not terminal remote health of an open stream. Cancellation never counts as a failure.

Optional model quality and defaults reach ranking only for the nonempty current Task,
with valid measurement provenance/freshness. Defaults never satisfy mandatory quality.
A stale mandatory descriptor still fails the whole selection; ranking weights and
external evaluation/datasets remain host-owned.
