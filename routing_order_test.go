package routery

import (
	"context"
	"math"
	"testing"
)

func routeKey(request string) (string, bool) { return request, true }
func labelHandler(label string) BasicRouteHandler[string, string] {
	return func(RouteCall[string]) (BasicRouteResult[string], error) { return BasicHandled(label), nil }
}
func TestMixedRoutesHaveExplicitOrder(t *testing.T) {
	t.Parallel()
	for _, longest := range []bool{false, true} {
		// Arrange.
		table := NewBasicRouteTable[string, string]()
		keys := OnStringKey(table, routeKey)
		keys.Prefix("short", 100, "a", labelHandler("short"))
		table.Route(
			"middle",
			50,
			nil,
			func(RouteCall[string]) (BasicRouteResult[string], error) {
				return BasicNext[string](BasicReasonNone), nil
			},
		)
		keys.Prefix("long", 0, "abc", labelHandler("long"))
		if longest {
			keys.LongestPrefixWins()
		}
		router, err := table.Build()
		if err != nil {
			t.Fatal(err)
		}
		// Act.
		result, dispatchErr := router.Dispatch(context.Background(), "abc")
		// Assert.
		want := "short"
		if longest {
			want = "long"
		}
		if dispatchErr != nil || result.Payload != want {
			t.Fatal(result, dispatchErr, want)
		}
	}
}

//nolint:gocognit // Exhaustive triple-order laws are clearer in one property fixture.
func TestRouteOrderTransitiveStableAndExtremePriorities(t *testing.T) {
	t.Parallel()
	// Arrange: real routes of each kind, including both int boundaries.
	table := NewBasicRouteTable[string, string]()
	keys := OnStringKey(table, routeKey)
	for _, priority := range []int{math.MinInt, -1, 0, 1, math.MaxInt} {
		table.Route("predicate", priority, nil, labelHandler("predicate"))
		keys.Exact("exact", priority, "abc", labelHandler("exact"))
		table.Mount(
			"mount",
			priority,
			nil,
			NewBasicRouteTable[string, string]().Route("child", 0, nil, labelHandler("mount")),
		)
		OnDecision(table, func(context.Context, string) (RouteDecision[string, BasicReason], error) {
			return RouteDecision[string, BasicReason]{Key: "abc", Matched: true, Confidence: 1}, nil
		}).Case("decision", priority, "abc", 0, labelHandler("decision"))
		keys.Prefix("short", priority, "a", labelHandler("short"))
		keys.Prefix("long", priority, "abc", labelHandler("long"))
	}
	for _, longest := range []bool{false, true} {
		// Act/Assert: antisymmetry, strict transitivity and equivalence transitivity.
		for _, left := range table.routes {
			for _, middle := range table.routes {
				lm := compareRoutes(left, middle, longest)
				ml := compareRoutes(middle, left, longest)
				if lm != -ml {
					t.Fatal("antisymmetry")
				}
				for _, right := range table.routes {
					mr := compareRoutes(middle, right, longest)
					lr := compareRoutes(left, right, longest)
					if lm < 0 && mr < 0 && lr >= 0 {
						t.Fatal("strict transitivity")
					}
					if lm == 0 && mr == 0 && lr != 0 {
						t.Fatal("tie transitivity")
					}
				}
			}
		}
	}
	// Observable extremes and declaration ties, without repeating comparator in assertions.
	ordered := NewBasicRouteTable[string, string]()
	ordered.Route("min", math.MinInt, nil, labelHandler("min")).
		Route("max-first", math.MaxInt, nil, labelHandler("first")).
		Route("max-second", math.MaxInt, nil, labelHandler("second"))
	router, err := ordered.Build()
	if err != nil {
		t.Fatal(err)
	}
	result, err := router.Dispatch(context.Background(), "any")
	if err != nil || result.Payload != "first" {
		t.Fatal(result, err)
	}
}
