package routery

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
)

func TestCombinatorsStopOnCleanupFailure(t *testing.T) {
	for _, name := range []string{"chain", "fallback", "predicate", "retry"} {
		t.Run(name, func(t *testing.T) {
			// Arrange.
			cleanupErr, primaryErr := errors.New("cleanup"), errors.New("primary")
			calls := 0
			primaryCalls := 0
			owner := NewLifetime(func() error { return cleanupErr })
			primary := func(RouteCall[int]) (BasicRouteResult[string], error) {
				primaryCalls++
				r := BasicHandled("partial")
				r.Lifetime = owner
				r.Match.RouteID = "original"
				if name == "chain" {
					r = Next[BasicKind, BasicReason, string]("")
					r.Lifetime = owner
					r.Match.RouteID = "original"
					return r, nil
				}
				return r, primaryErr
			}
			secondary := func(RouteCall[int]) (BasicRouteResult[string], error) { calls++; return BasicHandled("secondary"), nil }
			var h BasicRouteHandler[int, string]
			switch name {
			case "chain":
				h = Chain(primary, secondary)
			case "fallback":
				h = Fallback(primary, secondary)
			case "predicate":
				h = PredicateFallback(primary, secondary, func(error) bool { return true })
			case "retry":
				h = RetryIf[int, BasicKind, BasicReason, string](
					2,
					0,
					func(context.Context, int, error) bool { return true },
				)(
					primary,
				)
			}
			// Act.
			r, err := h(NewRouteCall(t.Context(), 0))
			// Assert.
			if calls != 0 || primaryCalls != 1 || !errors.Is(err, cleanupErr) || r.Action != ActionAbort ||
				r.Lifetime != owner ||
				r.Match.RouteID != "original" {
				t.Fatalf("calls=%d result=%+v error=%v", calls, r, err)
			}
			if name != "chain" && (!errors.Is(err, primaryErr) || r.Payload != "partial") {
				t.Fatalf("partial facts lost: %+v %v", r, err)
			}
		})
	}
}

func TestBindingPathSegmentIdentity(t *testing.T) {
	// Arrange.
	left := RouteMatch{RouteID: "leaf", Path: []RouteID{"a/b", "c", "leaf"}}
	right := left
	right.Path = []RouteID{"a", "b/c", "leaf"}
	// Act.
	a := NewRouteBinding("branch", 1, left, "input", "rev")
	b := NewRouteBinding("branch", 1, right, "input", "rev")
	again := NewRouteBinding("branch", 1, left, "input", "rev")
	// Assert.
	if a.Snapshot.Fingerprint == b.Snapshot.Fingerprint || a.Snapshot.Fingerprint != again.Snapshot.Fingerprint {
		t.Fatal("path boundaries lost or identity unstable")
	}
}

func TestDecisionConfidenceValidation(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), -1, 2, 0, 1} {
		t.Run("threshold/"+floatName(value), func(t *testing.T) {
			// Arrange.
			table := NewBasicRouteTable[int, string]()
			OnDecision(table, func(context.Context, int) (RouteDecision[string, BasicReason], error) {
				return RouteDecision[string, BasicReason]{Key: "yes", Matched: true, Confidence: 1}, nil
			}).Case("yes", 0, "yes", value, func(RouteCall[int]) (BasicRouteResult[string], error) { return BasicHandled("ok"), nil })
			// Act.
			_, err := table.Build()
			// Assert.
			invalid := math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1
			if invalid != errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("value=%v err=%v", value, err)
			}
		})
		t.Run("decision/"+floatName(value), func(t *testing.T) {
			// Arrange.
			table := NewBasicRouteTable[int, string]()
			calls := 0
			OnDecision(table, func(context.Context, int) (RouteDecision[string, BasicReason], error) {
				return RouteDecision[string, BasicReason]{Key: "yes", Matched: true, Confidence: value}, nil
			}).Case("yes", 0, "yes", 0, func(RouteCall[int]) (BasicRouteResult[string], error) { calls++; return BasicHandled("ok"), nil })
			router, err := table.Build()
			if err != nil {
				t.Fatal(err)
			}
			// Act.
			_, err = router.Dispatch(t.Context(), 0)
			// Assert.
			invalid := math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1
			var confidenceErr *InvalidConfidenceError
			if invalid && (!errors.As(err, &confidenceErr) || calls != 0) {
				t.Fatalf("malformed decision executed: calls=%d err=%v", calls, err)
			}
			if !invalid && (err != nil || calls != 1) {
				t.Fatalf("valid decision rejected: calls=%d err=%v", calls, err)
			}
		})
	}
}

func floatName(v float64) string {
	if math.IsNaN(v) {
		return "nan"
	}
	if math.IsInf(v, 1) {
		return "+inf"
	}
	if math.IsInf(v, -1) {
		return "-inf"
	}
	if v < 0 {
		return "negative"
	}
	if v > 1 {
		return "above-one"
	}
	if v == 0 {
		return "zero"
	}
	return "one"
}

func TestMountCyclesAndSharedSubtree(t *testing.T) {
	for _, indirect := range []bool{false, true} {
		// Arrange.
		a := NewBasicRouteTable[int, string]()
		b := a
		if indirect {
			b = NewBasicRouteTable[int, string]()
			b.Mount("back", 0, nil, a)
		}
		a.Mount("cycle", 0, nil, b)
		// Act.
		_, err := a.Build()
		// Assert.
		if !errors.Is(err, ErrInvalidConfig) || !strings.Contains(err.Error(), "cycle") {
			t.Fatalf("cycle error=%v", err)
		}
	}
	// Arrange.
	child := NewBasicRouteTable[int, string]().Route("leaf", 0, nil, func(RouteCall[int]) (BasicRouteResult[string], error) { return BasicHandled("ok"), nil })
	root := NewBasicRouteTable[int, string]().Mount("a", 0, nil, child).Mount("b", 0, nil, child)
	// Act.
	router, err := root.Build()
	if err != nil {
		t.Fatal(err)
	}
	r, err := router.Dispatch(t.Context(), 0)
	// Assert.
	if err != nil || r.Payload != "ok" {
		t.Fatalf("DAG result=%+v err=%v", r, err)
	}
}

func TestLateOnCloseWaitsForCleanupCompletion(t *testing.T) {
	// Arrange.
	entered, finish := make(chan struct{}), make(chan struct{})
	life := NewLifetime(func() error { close(entered); <-finish; return nil })
	done := make(chan error, 1)
	calls := make(chan struct{}, 2)
	life.OnClose(func() { calls <- struct{}{} })
	// Act.
	go func() { done <- life.Close() }()
	<-entered
	life.OnClose(func() { calls <- struct{}{} })
	// Assert.
	select {
	case <-calls:
		t.Error("release hook ran before cleanup completed")
	default:
	}
	close(finish)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 {
		t.Fatalf("hooks=%d want=2", len(calls))
	}
}

func TestHalfOpenPanicReleasesProbe(t *testing.T) {
	for _, classifier := range []bool{false, true} {
		// Arrange.
		calls := 0
		fault := errors.New("fault")
		handler := CircuitBreaker[int, BasicKind, BasicReason, string](1, 0, func(error) bool {
			if classifier && calls == 2 {
				panic("classifier")
			}
			return true
		})(func(RouteCall[int]) (BasicRouteResult[string], error) {
			calls++
			if calls == 1 || (classifier && calls == 2) {
				return AbortResult[BasicKind, BasicReason, string](), fault
			}
			if calls == 2 {
				panic("handler")
			}
			return BasicHandled("ok"), nil
		})
		_, _ = handler(NewRouteCall(t.Context(), 0))
		// Act.
		recovered := false
		func() { defer func() { recovered = recover() != nil }(); _, _ = handler(NewRouteCall(t.Context(), 0)) }()
		r, err := handler(NewRouteCall(t.Context(), 0))
		// Assert.
		if !recovered || err != nil || r.Payload != "ok" || calls != 3 {
			t.Fatalf("panic=%v calls=%d result=%+v err=%v", recovered, calls, r, err)
		}
	}
}

func TestObserverCannotMutateCanonicalMetadata(t *testing.T) {
	// Arrange.
	child := NewBasicRouteTable[int, string]().Route("leaf", 0, nil, func(RouteCall[int]) (BasicRouteResult[string], error) { return BasicHandled("ok"), nil })
	router, err := NewBasicRouteTable[int, string]().Mount("parent", 0, nil, child).Build()
	if err != nil {
		t.Fatal(err)
	}
	sink := OutcomeSinkFunc[BasicKind, BasicReason, string](func(event RouteEvent[BasicKind, BasicReason, string]) {
		event.Match.Path[0] = "changed"
		event.Result.Match.Path[1] = "changed"
	})
	// Act.
	result, err := router.DispatchWithSink(t.Context(), 0, sink)
	// Assert.
	if err != nil || result.Match.Path[0] != "parent" || result.Match.Path[1] != "leaf" {
		t.Fatalf("observer changed dispatch: %+v %v", result, err)
	}
}

func TestNegativeMiddlewareConfigurationDoesNotExecute(t *testing.T) {
	for _, middleware := range []BasicRouteMiddleware[int, string]{RetryIf[int, BasicKind, BasicReason, string](-1, 0, func(context.Context, int, error) bool { return true }), RetryIf[int, BasicKind, BasicReason, string](1, -1, func(context.Context, int, error) bool { return true }), Timeout[int, BasicKind, BasicReason, string](-1)} {
		// Arrange.
		calls := 0
		handler := middleware(
			func(RouteCall[int]) (BasicRouteResult[string], error) { calls++; return BasicHandled("ok"), nil },
		)
		// Act.
		_, err := handler(NewRouteCall(t.Context(), 0))
		// Assert.
		if !errors.Is(err, ErrInvalidConfig) || calls != 0 {
			t.Fatalf("calls=%d err=%v", calls, err)
		}
	}
}

func TestFallbackCancellationRetainsCanonicalOwner(t *testing.T) {
	for _, predicate := range []bool{false, true} {
		// Arrange.
		ctx, cancel := context.WithCancel(t.Context())
		providerErr := errors.New("provider")
		closes, calls := 0, 0
		owner := NewLifetime(func() error { closes++; return nil })
		primary := func(RouteCall[int]) (BasicRouteResult[string], error) {
			cancel()
			result := BasicHandled("partial")
			result.Lifetime = owner
			return result, providerErr
		}
		secondary := func(RouteCall[int]) (BasicRouteResult[string], error) { calls++; return BasicHandled("forbidden"), nil }
		handler := Fallback(primary, secondary)
		if predicate {
			handler = PredicateFallback(primary, secondary, func(error) bool { return true })
		}
		// Act.
		result, err := InvokeRouteHandler(ctx, 0, handler)
		// Assert.
		if !errors.Is(err, providerErr) || !errors.Is(err, context.Canceled) || calls != 0 || closes != 0 ||
			result.Lifetime != owner ||
			result.Payload != "partial" {
			t.Fatalf("calls=%d closes=%d result=%+v err=%v", calls, closes, result, err)
		}
		if err := result.Lifetime.Close(); err != nil || closes != 1 {
			t.Fatal("owner not retained", err, closes)
		}
	}
}

func TestFallbackCleanupCancellationStopsSecondary(t *testing.T) {
	for _, predicate := range []bool{false, true} {
		// Arrange.
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		providerErr := errors.New("provider")
		calls := 0
		owner := NewLifetime(nil)
		owner.OnClose(cancel)
		primary := func(RouteCall[int]) (BasicRouteResult[string], error) {
			result := BasicHandled("partial")
			result.Lifetime = owner
			result.Match.RouteID = "primary"
			return result, providerErr
		}
		secondary := func(RouteCall[int]) (BasicRouteResult[string], error) { calls++; return BasicHandled("forbidden"), nil }
		handler := Fallback(primary, secondary)
		if predicate {
			handler = PredicateFallback(primary, secondary, func(error) bool { return true })
		}
		// Act.
		result, err := InvokeRouteHandler(ctx, 0, handler)
		// Assert.
		if !errors.Is(err, providerErr) || !errors.Is(err, context.Canceled) || calls != 0 ||
			result.Lifetime != owner || result.Payload != "partial" || result.Match.RouteID != "primary" {
			t.Fatalf("calls=%d result=%+v err=%v", calls, result, err)
		}
	}
}
