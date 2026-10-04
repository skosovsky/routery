package routery

import (
	"context"
	"errors"
	"testing"

	a "github.com/skosovsky/routery/testdata/topology/a"
	b "github.com/skosovsky/routery/testdata/topology/b"
)

type topologyVariant struct {
	id                RouteID
	priority          int
	key               string
	mount             RouteID
	fallback, longest bool
}

func topologyFixture(test topologyVariant) *BasicRouteTable[string, string] {
	child := NewBasicRouteTable[string, string]()
	keys := OnStringKey(child, routeKey)
	keys.Prefix(test.id, test.priority, test.key, labelHandler("child"))
	if test.longest {
		keys.LongestPrefixWins()
	}
	if test.fallback {
		child.Fallback(labelHandler("fallback"))
	}
	return NewBasicRouteTable[string, string]().Mount(test.mount, 0, nil, child)
}
func TestNestedTopologyChangesAreFingerprintVisible(t *testing.T) {
	t.Parallel()
	// Arrange.
	base := topologyVariant{id: "child", priority: 1, key: "a", mount: "mount"}
	original, err := topologyFixture(base).Build()
	if err != nil {
		t.Fatal(err)
	}
	variants := []topologyVariant{base, base, base, base, base, base}
	variants[0].id = "new-child"
	variants[1].priority = 2
	variants[2].key = "abc"
	variants[3].mount = "new-mount"
	variants[4].fallback = true
	variants[5].longest = true
	// Act/Assert: public topology snapshots, no copied hash algorithm.
	for _, changed := range variants {
		router, buildErr := topologyFixture(changed).Build()
		if buildErr != nil {
			t.Fatal(buildErr)
		}
		if router.Snapshot().Fingerprint == original.Snapshot().Fingerprint {
			t.Fatalf("unchanged topology hash for %+v", changed)
		}
	}
	independent, err := topologyFixture(base).Build()
	if err != nil {
		t.Fatal(err)
	}
	if independent.Snapshot().Fingerprint != original.Snapshot().Fingerprint {
		t.Fatal("independent topology differs")
	}
}
func TestCompiledNestedSnapshotAndBindingStayImmutable(t *testing.T) {
	t.Parallel()
	// Arrange.
	child := NewBasicRouteTable[string, string]().Route("old-child", 1, nil, labelHandler("old"))
	root := NewBasicRouteTable[string, string]().Mount("mount", 0, nil, child)
	old, err := root.Build()
	if err != nil {
		t.Fatal(err)
	}
	previous := old.Snapshot()
	oldResult, err := old.Dispatch(context.Background(), "abc")
	if err != nil {
		t.Fatal(err)
	}
	binding := NewRouteBinding("branch", "payload", oldResult.Match, "input", previous.Fingerprint)
	stale := errors.New("stale topology")
	check := SnapshotFreshnessPolicyFunc[RouteBindingSnapshot, string](
		func(snapshot RouteSnapshot[RouteBindingSnapshot], current string) error {
			if snapshot.State.Revision != current {
				return stale
			}
			return nil
		},
	)
	// Act: mutate both builders; neither remains in the compiled tree.
	child.Route("new-child", 100, nil, labelHandler("new"))
	OnStringKey(root, routeKey).LongestPrefixWins()
	current, err := root.Build()
	if err != nil {
		t.Fatal(err)
	}
	oldAgain, oldErr := old.Dispatch(context.Background(), "abc")
	updated, newErr := current.Dispatch(context.Background(), "abc")
	freshnessErr := ValidateSnapshotFreshness(binding.Snapshot, current.Snapshot().Fingerprint, check)
	// Assert.
	if oldErr != nil || newErr != nil || oldAgain.Payload != "old" || updated.Payload != "new" ||
		old.Snapshot().Fingerprint != previous.Fingerprint ||
		!previous.IsStale(current.Snapshot().Fingerprint) ||
		!errors.Is(freshnessErr, stale) {
		t.Fatal(oldAgain, updated, oldErr, newErr, freshnessErr)
	}
	table, _ := routerTable(old)
	if table.subtreeHasBuilder() {
		t.Fatal("compiled snapshot retains a mutable builder")
	}
}
func (table *builtTable[Req, Kind, Reason, Payload]) subtreeHasBuilder() bool {
	for _, entry := range table.routes {
		if entry.sub != nil || (entry.nested != nil && entry.nested.subtreeHasBuilder()) {
			return true
		}
	}
	return false
}
func TestRegistryPublicationRetainsOldSnapshot(t *testing.T) {
	t.Parallel()
	// Arrange.
	registry := NewRouteRegistry[string, BasicKind, BasicReason, string]()
	if err := registry.Register(NewRouteSpec("old", 0, nil, labelHandler("old"))); err != nil {
		t.Fatal(err)
	}
	old := registry.Snapshot()
	// Act.
	if err := registry.Register(NewRouteSpec("new", 100, nil, labelHandler("new"))); err != nil {
		t.Fatal(err)
	}
	current := registry.Snapshot()
	oldResult, oldErr := old.Dispatch(context.Background(), "abc")
	newResult, newErr := current.Dispatch(context.Background(), "abc")
	// Assert.
	if oldErr != nil || newErr != nil || oldResult.Payload != "old" || newResult.Payload != "new" ||
		old.Fingerprint == current.Fingerprint {
		t.Fatal(oldResult, newResult, oldErr, newErr)
	}
}

type sameDisplayKey struct{ first, second string }

func (sameDisplayKey) String() string { return "same display" }
func TestStaticKeysAndDecisionThresholdHaveFramedIdentity(t *testing.T) {
	t.Parallel()
	// Arrange: different comparable keys with identical Stringer display.
	build := func(key sameDisplayKey) BasicRouter[sameDisplayKey, string] {
		table := NewBasicRouteTable[sameDisplayKey, string]()
		OnKey(
			table,
			func(input sameDisplayKey) (sameDisplayKey, bool) { return input, true },
		).Exact("id:with:delimiters", 0, key, func(RouteCall[sameDisplayKey]) (BasicRouteResult[string], error) { return BasicHandled("key"), nil })
		router, err := table.Build()
		if err != nil {
			t.Fatal(err)
		}
		return router
	}
	// Act.
	one := build(sameDisplayKey{first: "a", second: "bc"})
	two := build(sameDisplayKey{first: "ab", second: "c"})
	// Assert.
	if one.Snapshot().Fingerprint == two.Snapshot().Fingerprint {
		t.Fatal("different typed keys collided")
	}
	threshold := func(minimum float64) string {
		table := NewBasicRouteTable[string, string]()
		OnDecision(table, func(context.Context, string) (RouteDecision[string, BasicReason], error) {
			return RouteDecision[string, BasicReason]{Key: "yes", Matched: true, Confidence: 0.5}, nil
		}).Case("case", 0, "yes", minimum, labelHandler("decision"))
		router, err := table.Build()
		if err != nil {
			t.Fatal(err)
		}
		return router.Snapshot().Fingerprint
	}
	if threshold(0.1) == threshold(0.9) {
		t.Fatal("confidence topology absent")
	}
}

func TestCompositeComparableTypeIdentityIsPortable(t *testing.T) {
	t.Parallel()
	for _, pair := range [][2]any{{[0]a.Token{}, [0]b.Token{}}, {(*a.Token)(nil), (*b.Token)(nil)}} {
		// Arrange.
		build := func(key any) BasicRouter[any, string] {
			table := NewBasicRouteTable[any, string]()
			OnKey(
				table,
				func(input any) (any, bool) { return input, true },
			).Exact("typed", 0, key, func(RouteCall[any]) (BasicRouteResult[string], error) { return BasicHandled("hit"), nil })
			router, err := table.Build()
			if err != nil {
				t.Fatal(err)
			}
			return router
		}
		// Act.
		one, two := build(pair[0]), build(pair[1])
		hit, hitErr := one.Dispatch(context.Background(), pair[0])
		miss, missErr := two.Dispatch(context.Background(), pair[0])
		// Assert.
		if hitErr != nil || missErr != nil || !hit.HasPayload || miss.HasPayload ||
			one.Snapshot().Fingerprint == two.Snapshot().Fingerprint {
			t.Fatal(hit, miss, hitErr, missErr, "type identity collision")
		}
	}
}
func TestClassifierMemoizationGroupsAreTopology(t *testing.T) {
	t.Parallel()
	build := func(shared bool) BasicRouter[string, string] {
		table := NewBasicRouteTable[string, string]()
		classify := func(context.Context, string) (RouteDecision[string, BasicReason], error) {
			return RouteDecision[string, BasicReason]{Key: "yes", Matched: true, Confidence: 1}, nil
		}
		first := OnDecision(table, classify)
		first.Case("first", 10, "yes", 0, func(RouteCall[string]) (BasicRouteResult[string], error) {
			return BasicNext[string](BasicReasonNone), nil
		})
		second := first
		if !shared {
			second = OnDecision(table, classify)
		}
		second.Case("second", 0, "yes", 0, labelHandler("second"))
		router, err := table.Build()
		if err != nil {
			t.Fatal(err)
		}
		return router
	}
	// Arrange/Act.
	shared, independent := build(true), build(false)
	// Assert.
	if shared.Snapshot().Fingerprint == independent.Snapshot().Fingerprint {
		t.Fatal("cache group topology absent")
	}
	if build(true).Snapshot().Fingerprint != shared.Snapshot().Fingerprint {
		t.Fatal("group identity used addresses")
	}
}

func TestRepeatedMountMemoizationUsesGlobalTopologyGroups(t *testing.T) {
	t.Parallel()
	build := func(shared bool) (BasicRouter[string, string], *int) {
		calls := new(int)
		classify := func(ctx context.Context, _ string) (RouteDecision[string, BasicReason], error) {
			*calls++
			key := "a"
			if *calls > 1 {
				key = "b"
			}
			return RouteDecision[string, BasicReason]{Key: key, Matched: true, Confidence: 1}, ctx.Err()
		}
		child := func() *BasicRouteTable[string, string] {
			table := NewBasicRouteTable[string, string]()
			OnDecision(
				table,
				classify,
			).Case("a", 10, "a", 0, func(RouteCall[string]) (BasicRouteResult[string], error) {
				return BasicNext[string](BasicReasonNone), nil
			}).
				Case("b", 0, "b", 0, labelHandler("hit"))
			return table
		}
		first := child()
		second := first
		if !shared {
			second = child()
		}
		root := NewBasicRouteTable[string, string]().Mount("first", 0, nil, first).Mount("second", 0, nil, second)
		router, err := root.Build()
		if err != nil {
			t.Fatal(err)
		}
		return router, calls
	}
	// Arrange.
	shared, sharedCalls := build(true)
	split, splitCalls := build(false)
	// Act.
	miss, missErr := shared.Dispatch(context.Background(), "request")
	hit, hitErr := split.Dispatch(context.Background(), "request")
	// Assert: group sharing spans nested dispatch, without pointer-address hashes.
	if missErr != nil || hitErr != nil || miss.HasPayload || hit.Payload != "hit" || *sharedCalls != 1 ||
		*splitCalls != 2 ||
		shared.Snapshot().Fingerprint == split.Snapshot().Fingerprint {
		t.Fatal(miss, hit, missErr, hitErr, *sharedCalls, *splitCalls)
	}
	independent, _ := build(true)
	if independent.Snapshot().Fingerprint != shared.Snapshot().Fingerprint {
		t.Fatal("non-portable memoization group identity")
	}
}
