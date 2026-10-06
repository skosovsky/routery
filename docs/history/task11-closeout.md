# Task 11 acceptance — routing correctness

Independent readonly reviewers: task11_completeness (7/7, 100%, final freeze) and
 task11_correctness (no open confirmed defects, final repeat). This is a scoped
review result, not proof of absence of all defects.

| Criterion | Evidence |
| --- | --- |
| Breaker stale completions | CircuitOldCompletionCannotOwnNewProbe: success/failure, both completion orders |
| Probe release / classifier | CircuitExcludedProbeReleasesAdmission, CircuitClassifierRunsOnceOutsideLock |
| Full order | MixedRoutesHaveExplicitOrder, RouteOrderTransitiveStableAndExtremePriorities, existing fallthrough/memoization tests |
| Full topology | NestedTopologyChangesAreFingerprintVisible, StaticKeysAndDecisionThresholdHaveFramedIdentity, CompositeComparableTypeIdentityIsPortable |
| Immutable/fresh snapshots | CompiledNestedSnapshotAndBindingStayImmutable, RegistryPublicationRetainsOldSnapshot, existing concurrent registry tests |
| Task quality | OptionalAndDefaultQualityAreTaskScopedAtRank: current/other/empty task, defaults, stale/future/provenance, existing mandatory/temporal tests |
| Public contract | routing-contracts.md, README, migration.md and API comments; cyclic comparator removed |

Correctness review identified composite comparable-key type collisions and missing
classifier memoization topology. Both fixed; independent repro now shows distinct
fingerprints. RepeatedMountMemoizationUsesGlobalTopologyGroups verifies observable
Next/Handled, calls1/2 and deterministic independent builds. Built decision-table
ordered static cases are included; opaque host tables/functions remain excluded.

Final gates: make lint all nine modules, zero issues; make test all nine modules
under race; target go test -race . ./policy ./policy/model -count=1; git diff --check.
All passed. Logs: /tmp/routery-task11-lint.log, /tmp/routery-task11-test.log,
/tmp/routery-task11-target-race.log. No push/release. Scope stays BYOT routing and
local resilience; no automatic rebind, distributed health service or model catalog.
