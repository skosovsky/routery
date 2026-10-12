package routery

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestPureMissMatcherExecutesOnceWithoutMetadata(t *testing.T) {
	// Arrange.
	calls := 0
	entry := routeEntry[int, int, int, int]{
		id:      "miss",
		matcher: predicateMatcher(func(int) bool { calls++; return false }),
	}
	parent := []RouteID{"parent"}
	// Act.
	match, ok, err := entry.matchRoute(NewRouteCall(context.Background(), 0), parent, 1)
	// Assert.
	if calls != 1 || ok || err != nil || !reflect.DeepEqual(match, RouteMatch{}) || parent[0] != "parent" {
		t.Fatalf("calls=%d match=%+v ok=%v err=%v", calls, match, ok, err)
	}
}

func TestMatcherErrorRetainsDetachedMetadata(t *testing.T) {
	for _, ok := range []bool{false, true} {
		t.Run(map[bool]string{false: "false", true: "true"}[ok], func(t *testing.T) {
			// Arrange.
			expectedErr := errors.New("matcher")
			calls := 0
			entry := routeEntry[int, int, int, int]{
				id:       "error",
				priority: 9,
				matcher: routeMatcher[int]{
					kind: MatchKindDecision,
					match: func(RouteCall[int]) (routeMatchData, bool, error) {
						calls++
						return routeMatchData{
							key:               "key",
							prefix:            "pre",
							remainder:         "rest",
							decisionReason:    42,
							hasDecisionReason: true,
						}, ok, expectedErr
					},
				},
			}
			parent := make([]RouteID, 1, 4)
			parent[0] = "parent"
			// Act.
			match, gotOK, err := entry.matchRoute(NewRouteCall(context.Background(), 0), parent, 1)
			original := cloneRouteMatch(match)
			match.Path[0] = "mutated"
			// Assert: errors retain all metadata regardless of boolean, and own their path.
			if calls != 1 || gotOK != ok || !errors.Is(err, expectedErr) || parent[0] != "parent" ||
				!reflect.DeepEqual(
					original,
					RouteMatch{
						RouteID:           "error",
						Path:              []RouteID{"parent", "error"},
						Priority:          9,
						Depth:             1,
						Kind:              MatchKindDecision,
						Key:               "key",
						Prefix:            "pre",
						Remainder:         "rest",
						DecisionReason:    42,
						HasDecisionReason: true,
					},
				) {
				t.Fatalf("match=%+v calls=%d ok=%v err=%v", original, calls, gotOK, err)
			}
		})
	}
}

func TestNestedMissErrorAndSinkPathIsolation(t *testing.T) {
	// Arrange.
	expectedErr := errors.New("nested classifier")
	misses, classifications, handlers, events := 0, 0, 0, 0
	handler := RouteHandler[int, int, int, int](func(RouteCall[int]) (RouteResult[int, int, int], error) {
		handlers++
		return Handled(1, 1, 1), nil
	})
	child := NewRouteTable[int, int, int, int]().Route("miss", 2, func(int) bool { misses++; return false }, handler)
	OnDecision(child, func(context.Context, int) (RouteDecision[int, int], error) {
		classifications++
		return RouteDecision[int, int]{}, expectedErr
	}).Case("error", 1, 1, 0, handler)
	parent := NewRouteTable[int, int, int, int]().Mount("mount", 0, nil, child)
	r, err := parent.Build()
	if err != nil {
		t.Fatal(err)
	}
	var observed RouteEvent[int, int, int]
	sink := OutcomeSinkFunc[int, int, int](func(event RouteEvent[int, int, int]) {
		events++
		observed = event
		event.Match.Path[0] = "sink"
		event.Result.Match.Path[1] = "sink-result"
	})
	// Act.
	result, err := r.DispatchWithSink(context.Background(), 0, sink)
	// Assert.
	if !errors.Is(err, expectedErr) || misses != 1 || classifications != 1 || handlers != 0 || events != 1 ||
		result.Action != ActionAbort ||
		result.Match.Depth != 1 ||
		result.Match.Kind != MatchKindDecision ||
		!reflect.DeepEqual(result.Match.Path, []RouteID{"mount", "error"}) ||
		observed.Match.Path[1] != "error" ||
		observed.Result.Match.Path[0] != "mount" {
		t.Fatalf("result=%+v err=%v counts=%d/%d/%d/%d", result, err, misses, classifications, handlers, events)
	}
}

func TestNestedMissSuccessSinkAndDispatchPathIsolation(t *testing.T) {
	// Arrange.
	misses := 0
	var retained RouteCall[int]
	child := NewRouteTable[int, int, int, int]().Route("miss", 1, func(int) bool { misses++; return false }, func(RouteCall[int]) (RouteResult[int, int, int], error) {
		t.Fatal("miss handler called")
		return RouteResult[int, int, int]{}, nil
	}).
		Route("leaf", 0, nil, func(call RouteCall[int]) (RouteResult[int, int, int], error) {
			retained = call
			// Retain the handler view; mutate it after dispatch returns.
			return Handled(1, 1, 1), nil
		})
	parent := NewRouteTable[int, int, int, int]().Mount("mount", 0, nil, child)
	r, err := parent.Build()
	if err != nil {
		t.Fatal(err)
	}
	sink := OutcomeSinkFunc[int, int, int](func(event RouteEvent[int, int, int]) {
		event.Match.Path[0] = "sink"
		event.Result.Match.Path[1] = "sink-result"
	})
	// Act.
	result, err := r.DispatchWithSink(context.Background(), 0, sink)
	// Assert: sink paths are detached from canonical handler/result metadata.
	if err != nil || misses != 1 || result.Action != ActionStop ||
		!reflect.DeepEqual(result.Match.Path, []RouteID{"mount", "leaf"}) {
		t.Fatalf("result=%+v err=%v misses=%d", result, err, misses)
	}
	if !reflect.DeepEqual(retained.Match.Path, []RouteID{"mount", "leaf"}) {
		t.Fatal("sink changed the handler view")
	}
	// Act: handler and result views are borrowed within one invocation; neither
	// may change metadata belonging to another dispatch or the compiled table.
	retained.Match.Path[0] = "handler"
	result.Match.Path[1] = "caller-result"
	again, againErr := r.DispatchWithSink(context.Background(), 0, sink)
	// Assert.
	if againErr != nil || misses != 2 || !reflect.DeepEqual(again.Match.Path, []RouteID{"mount", "leaf"}) {
		t.Fatalf("next dispatch inherited mutation: result=%+v err=%v", again, againErr)
	}
}

func TestRejectedMatcherSideEffectsOrderFallbackAndCancellation(t *testing.T) {
	for _, cancelOnMiss := range []bool{false, true} {
		t.Run(map[bool]string{false: "fallback", true: "cancel"}[cancelOnMiss], func(t *testing.T) {
			// Arrange.
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			order := []string{}
			handlers := 0
			table := NewRouteTable[int, int, int, int]()
			handler := RouteHandler[int, int, int, int](func(RouteCall[int]) (RouteResult[int, int, int], error) {
				handlers++
				return Handled(1, 1, 1), nil
			})
			table.Route("low", 0, func(int) bool { order = append(order, "low"); return false }, handler)
			table.Route("high", 1, func(int) bool {
				order = append(order, "high")
				if cancelOnMiss {
					cancel()
				}
				return false
			}, handler)
			table.Fallback(func(call RouteCall[int]) (RouteResult[int, int, int], error) {
				order = append(order, "fallback")
				return handler(call)
			})
			r, err := table.Build()
			if err != nil {
				t.Fatal(err)
			}
			events := 0
			sink := OutcomeSinkFunc[int, int, int](func(RouteEvent[int, int, int]) { events++ })
			// Act.
			result, err := r.DispatchWithSink(ctx, 0, sink)
			// Assert: misses stay nonterminal, execute once in priority order, emit nothing.
			if cancelOnMiss {
				if !errors.Is(err, context.Canceled) || handlers != 0 || events != 0 || result.Action != ActionAbort ||
					!reflect.DeepEqual(order, []string{"high"}) {
					t.Fatalf("order=%v result=%+v err=%v handlers=%d events=%d", order, result, err, handlers, events)
				}
			} else if err != nil || handlers != 1 || events != 1 || result.Action != ActionStop || result.Match.Kind != MatchKindFallback || !reflect.DeepEqual(order, []string{"high", "low", "fallback"}) {
				t.Fatalf("order=%v result=%+v err=%v handlers=%d events=%d", order, result, err, handlers, events)
			}
		})
	}
}
