package execution

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/skosovsky/routery"
	"github.com/skosovsky/routery/policy/attempt"
)

func TestExecutionValidatesCanonicalRouteActions(t *testing.T) {
	for _, mode := range []string{"boundary", "sequence", "race"} {
		for _, action := range []routery.RouteAction{routery.ActionAbort, "unknown"} {
			t.Run(mode+"/"+string(action), func(t *testing.T) { checkExecutionRouteValidation(t, mode, action) })
		}
	}
}

func checkExecutionRouteValidation(t *testing.T, mode string, action routery.RouteAction) {
	t.Helper()
	// Arrange.
	coordinator, identity := setup(t)
	var calls, closes, classifications atomic.Int32
	life := routery.NewLifetime(func() error { closes.Add(1); return nil })
	t.Cleanup(func() { _ = life.Close() })
	boundary := testBoundary{Fresh: func(context.Context, string) error { return nil },
		Dispatch: func(routery.RouteCall[string], *Receipt) (routery.BasicRouteResult[string], error) {
			calls.Add(1)
			result := routery.BasicHandled("partial")
			result.Action, result.Lifetime = action, life
			result.Match.RouteID = "partial-route"
			return result, nil
		}}
	// Act.
	result, err := runInvalidRouteBoundary(t, mode, boundary, coordinator, identity, &classifications)
	// Assert.
	if !errors.Is(err, routery.ErrInvalidConfig) || calls.Load() != 1 || classifications.Load() != 0 {
		t.Fatalf("err=%v calls=%d classifications=%d", err, calls.Load(), classifications.Load())
	}
	if result.Route.Action != routery.ActionAbort || result.Route.Lifetime != life ||
		!result.Route.HasPayload || result.Route.Payload != "partial" || result.Route.Match.RouteID != "partial-route" {
		t.Fatal("invalid route action lost canonical owned partial metadata")
	}
	wantClosed := int32(0)
	if mode == "race" {
		wantClosed = 1
	}
	if closes.Load() != wantClosed || result.Receipt == nil {
		t.Fatal("ownership or attempt accounting lost")
	}
	if closeErr := result.Route.Lifetime.Close(); closeErr != nil || closes.Load() != 1 {
		t.Fatal("partial resource was not closed exactly once")
	}
	event, _, snapshotErr := result.Receipt.Snapshot()
	if snapshotErr != nil || event.Phase != attempt.Terminal || event.Outcome != attempt.Unknown {
		t.Fatal("malformed result invented completion or not-executed facts")
	}
}

func runInvalidRouteBoundary(t *testing.T, mode string, boundary testBoundary, coordinator *attempt.Coordinator,
	identity attempt.Identity, classifications *atomic.Int32,
) (Result[routery.BasicKind, routery.BasicReason, string], error) {
	t.Helper()
	switch mode {
	case "boundary":
		return boundary.Run(routery.NewRouteCall(t.Context(), "input"), coordinator, identity)
	case "sequence":
		now := time.Unix(100, 0)
		sequence := sequenceFixture(t, &now)
		sequence.Boundary = boundary
		sequence.Classify = func(Result[routery.BasicKind, routery.BasicReason, string], error) (failureClass, error) {
			classifications.Add(1)
			return transientFailure, nil
		}
		result, err := sequence.Run(t.Context(), coordinator, Step[string]{Request: "input", Identity: identity})
		return result.Last, err
	default:
		race := testRace{Boundary: boundary, Workers: 1, Permissions: readOnlyPermissions(),
			Accept: func(context.Context, Result[routery.BasicKind, routery.BasicReason, string]) (bool, error) {
				t.Error("invalid route action reached acceptance callback")
				return true, nil
			}}
		result, err := race.Run(t.Context(), coordinator, racePlans("invalid", "queued"))
		entries := result.Journal.Snapshot()
		if len(entries) == 0 {
			t.Fatal("invalid physical attempt disappeared from journal")
		}
		return entries[0].Result, err
	}
}
