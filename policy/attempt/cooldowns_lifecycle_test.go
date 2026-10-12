package attempt

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestCooldownCleanupBoundaryAndReuse(t *testing.T) {
	// Arrange: caller time, including future dates, governs expiration exclusively.
	now := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	state := NewCooldowns[string]()
	state.Extend("past", now.Add(-time.Nanosecond))
	state.Extend("boundary", now)
	state.Extend("active", now.Add(time.Nanosecond))
	state.Extend("active", now)
	// Act.
	pastBefore := state.Until("past")
	removed := state.PruneExpired(now)
	active := state.Until("active")
	pastAfter := state.Until("past")
	boundaryAfter := state.Until("boundary")
	again := state.PruneExpired(now.Add(-time.Hour))
	state.Extend("past", now.Add(-time.Hour))
	reused := state.Until("past")
	// Assert: no automatic expiration, inclusive cutoff, reset only removed histories.
	if !pastBefore.Equal(now.Add(-time.Nanosecond)) || removed != 2 || !pastAfter.IsZero() || !boundaryAfter.IsZero() ||
		!active.Equal(now.Add(time.Nanosecond)) ||
		again != 0 ||
		!reused.Equal(now.Add(-time.Hour)) {
		t.Fatalf("removed=%d active=%v reused=%v", removed, active, reused)
	}
}

func TestCooldownCleanupCardinality(t *testing.T) {
	for _, n := range []int{1000, 10000, 100000} {
		for _, live := range []int{0, 7} {
			t.Run(fmt.Sprintf("%d/live=%d", n, live), func(t *testing.T) {
				checkCooldownCleanupCardinality(t, n, live)
			})
		}
	}
}

func checkCooldownCleanupCardinality(t *testing.T, n, live int) {
	t.Helper()
	// Arrange.
	state := NewCooldowns[int]()
	now := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	for i := range n {
		state.Extend(i, now)
	}
	for i := range live {
		state.Extend(n+i, now.Add(time.Hour))
	}
	// Act.
	removed := state.PruneExpired(now)
	runtime.GC()
	// Assert: only the live set is reachable, not historical entries/capacity.
	if removed != n || len(state.until) != live {
		t.Fatalf("removed=%d retained=%d", removed, len(state.until))
	}
	if live == 0 && state.until != nil {
		t.Fatal("empty map backing storage retained")
	}
	for i := range n {
		if !state.Until(i).IsZero() {
			t.Fatal("expired scope retained")
		}
	}
	for i := range live {
		if !state.Until(n + i).Equal(now.Add(time.Hour)) {
			t.Fatal("neighbor live scope lost")
		}
	}
}

func TestCooldownCleanupChurn(t *testing.T) {
	// Arrange.
	const cycles = 30
	const batch = 1000
	state := NewCooldowns[int]()
	now := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	state.Extend(-1, now.Add(time.Hour))
	// Act and assert each cycle.
	for cycle := range cycles {
		for i := range batch {
			state.Extend(cycle*batch+i, now)
		}
		removed := state.PruneExpired(now)
		// Assert.
		if removed != batch || len(state.until) != 1 || !state.Until(-1).Equal(now.Add(time.Hour)) {
			t.Fatalf("cycle=%d removed=%d retained=%d", cycle, removed, len(state.until))
		}
	}
}

func TestCooldownConcurrentCleanupPreservesExtension(t *testing.T) {
	// Arrange: each iteration permits either prune/extend serialization order.
	const iterations = 1000
	now := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	state := NewCooldowns[int]()
	for i := range iterations {
		state.Extend(i, now)
		start := make(chan struct{})
		var group sync.WaitGroup
		// Act.
		group.Go(func() { <-start; state.PruneExpired(now) })
		group.Go(func() { <-start; state.Extend(i, now.Add(time.Hour)); state.Extend(i, now.Add(time.Minute)) })
		group.Go(func() { <-start; state.Until(i) })
		close(start)
		group.Wait()
		// Assert: the later active extension survives both orders.
		if !state.Until(i).Equal(now.Add(time.Hour)) {
			t.Fatalf("lost live extension %d", i)
		}
	}
}

func TestCooldownEmptyCleanupAndZeroValue(t *testing.T) {
	// Arrange.
	var state Cooldowns[string]
	now := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	// Act.
	empty := state.PruneExpired(time.Time{})
	state.Extend("scope", now)
	first := state.PruneExpired(now)
	second := state.PruneExpired(now)
	state.Extend("scope", now.Add(time.Hour))
	// Assert.
	if empty != 0 || first != 1 || second != 0 || !state.Until("scope").Equal(now.Add(time.Hour)) {
		t.Fatal("empty cleanup/reuse failed")
	}
}

// TestCooldownRetainedHeap records heap bytes rather than making a noisy RSS gate.
// Structural/cardinality tests above provide deterministic storage-release gates.
func TestCooldownRetainedHeap(t *testing.T) {
	for _, live := range []int{0, 7} {
		t.Run(fmt.Sprintf("live=%d", live), func(t *testing.T) {
			// Arrange: compare cleanup to deleting entries in place from a large map.
			const entries = 100000
			now := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
			runtime.GC()
			var before, full, deleted, pruned runtime.MemStats
			runtime.ReadMemStats(&before)
			state := NewCooldowns[int]()
			for i := range entries {
				state.Extend(i, now)
			}
			for i := range live {
				state.Extend(entries+i, now.Add(time.Hour))
			}
			runtime.GC()
			runtime.ReadMemStats(&full)
			for i := range entries {
				delete(state.until, i)
			}
			runtime.GC()
			runtime.ReadMemStats(&deleted)
			// Restore expired entries into the same capacity, then invoke supported cleanup.
			for i := range entries {
				state.Extend(i, now)
			}
			// Act.
			removed := state.PruneExpired(now)
			runtime.GC()
			runtime.ReadMemStats(&pruned)
			runtime.KeepAlive(state)
			// Assert: deterministic count plus observational heap deltas, no fixed byte threshold.
			if removed != entries || len(state.until) != live {
				t.Fatal("cleanup retained historical entries")
			}
			t.Logf(
				"live=%d heap_full_delta=%d heap_delete_in_place_delta=%d heap_prune_delta=%d",
				live,
				int64(full.HeapAlloc)-int64(before.HeapAlloc),
				int64(deleted.HeapAlloc)-int64(before.HeapAlloc),
				int64(pruned.HeapAlloc)-int64(before.HeapAlloc),
			)
		})
	}
}
