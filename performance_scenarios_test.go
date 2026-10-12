package routery

import (
	"context"
	"errors"
	"strconv"
	"testing"
)

func benchmarkPerformanceRouter(
	tb testing.TB,
	scenario string,
) (Router[int, int, int, int], OutcomeSink[int, int, int], error) {
	tb.Helper()
	table := NewRouteTable[int, int, int, int]()
	handler := func(RouteCall[int]) (RouteResult[int, int, int], error) { return Handled(1, 1, 1), nil }
	var sink OutcomeSink[int, int, int]
	var expectedErr error
	switch scenario {
	case "last":
		const misses = 64
		for i := range misses {
			table.Route(RouteID(strconv.Itoa(i)), 0, func(int) bool { return false }, handler)
		}
		table.Route("last", 0, nil, handler)
	case "mixed":
		OnKey(table, func(i int) (int, bool) { return i, true }).Exact("exact", 3, 1, handler)
		OnStringKey(table, func(int) (string, bool) { return "other", true }).Prefix("prefix", 2, "match", handler)
		table.Route("predicate", 1, func(int) bool { return false }, handler).Route("last", 0, nil, handler)
	case "nested":
		child := NewRouteTable[int, int, int, int]().Route("miss", 1, func(int) bool { return false }, handler).
			Route("leaf", 0, nil, handler)
		table.Mount("mount", 0, nil, child)
	case "error":
		expectedErr = errors.New("classifier failure")
		OnDecision(table, func(context.Context, int) (RouteDecision[int, int], error) {
			return RouteDecision[int, int]{}, expectedErr
		}).Case("error", 0, 1, 0, handler)
	case "sink":
		table.Route("miss", 1, func(int) bool { return false }, handler).Route("leaf", 0, nil, handler)
		sink = OutcomeSinkFunc[int, int, int](func(event RouteEvent[int, int, int]) {
			event.Match.Path[0] = "sink"
			event.Result.Match.Path[0] = "sink-result"
		})
	default:
		tb.Fatal("unknown scenario")
	}
	r, err := table.Build()
	if err != nil {
		tb.Fatal(err)
	}
	return r, sink, expectedErr
}

func BenchmarkPerformanceDispatch(b *testing.B) {
	for _, scenario := range []string{"last", "mixed", "nested", "error", "sink"} {
		b.Run(scenario, func(b *testing.B) {
			r, sink, expectedErr := benchmarkPerformanceRouter(b, scenario)
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				result, err := r.DispatchWithSink(ctx, 0, sink)
				if !errors.Is(err, expectedErr) || (expectedErr == nil && result.Action != ActionStop) ||
					(expectedErr != nil && result.Action != ActionAbort) {
					b.Fatal(result, err)
				}
			}
		})
	}
}
