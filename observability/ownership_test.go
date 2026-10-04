package observability

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/skosovsky/routery"
)

type observedPartial struct {
	action routery.RouteAction
	match  routery.RouteID
	meta   PayloadMeta
	err    error
}

func partialObserver(name string, events chan<- observedPartial) routery.BasicRouteMiddleware[int, string] {
	if name == "logging" {
		return Logging(
			"owned",
			func(_ context.Context, event Event[int, routery.BasicKind, routery.BasicReason, string]) {
				events <- observedPartial{action: event.Outcome.Action, match: event.Outcome.Match.RouteID,
					meta: event.PayloadMeta, err: event.Err}
			},
			nil,
		)
	}
	return Metrics[int]("owned", MetricsHooks[routery.BasicKind, routery.BasicReason, string]{
		OnComplete: func(_ context.Context, _ string, _ time.Duration, result ResultMeta[routery.BasicKind, routery.BasicReason], meta PayloadMeta, err error) {
			events <- observedPartial{action: result.Action, match: result.Match.RouteID, meta: meta, err: err}
		},
	})
}

func TestObserversRetainOwnedPartialAndValidateActions(t *testing.T) {
	for _, name := range []string{"logging", "metrics"} {
		for _, scenario := range []string{"provider error", "abort without error", "unknown action"} {
			t.Run(name+"/"+scenario, func(t *testing.T) { checkObservedPartial(t, name, scenario) })
		}
	}
}

func checkObservedPartial(t *testing.T, name, scenario string) {
	t.Helper()
	// Arrange.
	failure, cleanupErr := errors.New("provider failure"), errors.New("cleanup failure")
	var closes, hooks atomic.Int32
	life := routery.NewLifetime(func() error { closes.Add(1); return cleanupErr })
	life.OnClose(func() { hooks.Add(1) })
	t.Cleanup(func() { _ = life.Close() })
	events := make(chan observedPartial, 1)
	leaf := func(routery.RouteCall[int]) (routery.BasicRouteResult[string], error) {
		result := routery.BasicHandled("private partial")
		result.Lifetime, result.Match.RouteID = life, "leaf-route"
		switch scenario {
		case "abort without error":
			result.Action = routery.ActionAbort
			return result, nil
		case "unknown action":
			result.Action = "unknown"
			return result, nil
		default:
			return result, failure
		}
	}
	call := routery.NewRouteCall(t.Context(), 42)
	call.Match.RouteID = "caller-route"
	// Act: invoke the middleware directly so it must expose the canonical error itself.
	result, err := partialObserver(name, events)(leaf)(call)
	// Assert.
	wantErr := failure
	if scenario != "provider error" {
		wantErr = routery.ErrInvalidConfig
	}
	if !errors.Is(err, wantErr) || result.Lifetime != life || !result.HasPayload ||
		result.Payload != "private partial" || result.Action != routery.ActionAbort || result.Match.RouteID != "leaf-route" {
		t.Fatal("observer discarded owned partial or bypassed canonical validation")
	}
	observed := <-events
	if !errors.Is(observed.err, wantErr) || observed.action != routery.ActionAbort || observed.match != "leaf-route" ||
		observed.meta.Shape != "string" || observed.meta.Fingerprint != "" || closes.Load() != 0 {
		t.Fatal("observer changed ownership/match or lost shape-only error metadata")
	}
	firstErr, secondErr := result.Lifetime.Close(), result.Lifetime.Close()
	if !errors.Is(firstErr, cleanupErr) || !errors.Is(secondErr, cleanupErr) || closes.Load() != 1 ||
		hooks.Load() != 1 {
		t.Fatal("observer lost exactly-once cleanup/hooks/error")
	}
}
