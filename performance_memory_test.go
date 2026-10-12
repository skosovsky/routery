package routery

import (
	"context"
	"strconv"
	"testing"
)

func auditMissRouter(tb testing.TB, n int) Router[int, int, int, int] {
	tb.Helper()
	table := NewRouteTable[int, int, int, int]()
	for i := range n {
		table.Route(
			RouteID(strconv.Itoa(i)),
			0,
			func(int) bool { return false },
			func(RouteCall[int]) (RouteResult[int, int, int], error) { return Handled(1, 1, 1), nil },
		)
	}
	r, err := table.Build()
	if err != nil {
		tb.Fatal(err)
	}
	return r
}
func TestMissDispatchAllocationsBounded(t *testing.T) {
	// Arrange.
	small, large := auditMissRouter(t, 1), auditMissRouter(t, 1024)
	ctx := context.Background()
	// Act.
	measure := func(r Router[int, int, int, int]) float64 {
		return testing.AllocsPerRun(10, func() {
			result, err := r.Dispatch(ctx, 0)
			if err != nil || result.Action != ActionNext {
				panic("invalid dispatch")
			}
		})
	}
	one, many := measure(small), measure(large)
	// Assert: pure misses have only the constant dispatch allocation.
	t.Logf("routes=1 allocs=%g routes=1024 allocs=%g", one, many)
	if one > 1 || many > 1 {
		t.Fatalf("miss allocations must be constant and <=1: one=%g many=%g", one, many)
	}
}
func BenchmarkAuditMissDispatch(b *testing.B) {
	for _, n := range []int{1, 64, 1024} {
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			r := auditMissRouter(b, n)
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				result, err := r.Dispatch(ctx, 0)
				if err != nil || result.Action != ActionNext {
					b.Fatal(result, err)
				}
			}
		})
	}
}
