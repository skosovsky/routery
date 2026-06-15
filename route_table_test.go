package routery

import (
	"context"
	"errors"
	"testing"
)

func TestRouteTableBuildNilTable(t *testing.T) {
	t.Parallel()

	var table *RouteTable[int, string]
	_, err := table.Build()
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestRouteTableBuildNoHandlers(t *testing.T) {
	t.Parallel()

	_, err := NewRouteTable[int, string]().Build()
	if !errors.Is(err, ErrNoHandlers) {
		t.Fatalf("expected ErrNoHandlers, got %v", err)
	}
}

func TestRouteTableBuildOnlyFallback(t *testing.T) {
	t.Parallel()

	router, err := NewRouteTable[int, string]().
		Fallback(FromFunc(func(context.Context, int) (string, error) {
			return "fallback", nil
		})).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	outcome, err := router.Dispatch(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.HasPayload || outcome.Payload != "fallback" {
		t.Fatalf("got %+v; want fallback", outcome)
	}
}

func TestRouteTableBuildNilHandler(t *testing.T) {
	t.Parallel()

	_, err := NewRouteTable[int, string]().
		Route("empty", 1, nil, nil).
		Build()
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestRouteTableBuildPriorityOrder(t *testing.T) {
	t.Parallel()

	router, err := NewRouteTable[int, string]().
		Route("low", 1, nil, FromFunc(func(context.Context, int) (string, error) {
			return "low", nil
		})).
		Route("high", 10, nil, FromFunc(func(context.Context, int) (string, error) {
			return "high", nil
		})).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	outcome, err := router.Dispatch(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.HasPayload || outcome.Payload != "high" {
		t.Fatalf("got %+v; want high priority route", outcome)
	}
}

func TestRouterSnapshotFingerprint(t *testing.T) {
	t.Parallel()

	router, err := NewRouteTable[int, string]().
		Route("a", 1, nil, FromFunc(func(context.Context, int) (string, error) {
			return "a", nil
		})).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	snapshot := router.Snapshot()
	if snapshot.Fingerprint == "" {
		t.Fatal("expected non-empty fingerprint")
	}
	if len(snapshot.State.RouteIDs) != 1 || snapshot.State.RouteIDs[0] != "a" {
		t.Fatalf("unexpected route ids: %v", snapshot.State.RouteIDs)
	}
}

func TestRouteTableEqualPriorityPreservesRegistrationOrder(t *testing.T) {
	t.Parallel()

	router, err := NewRouteTable[int, string]().
		Route("first", 10, nil, FromFunc(func(context.Context, int) (string, error) {
			return "first", nil
		})).
		Route("second", 10, nil, FromFunc(func(context.Context, int) (string, error) {
			return "second", nil
		})).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	outcome, err := router.Dispatch(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.HasPayload || outcome.Payload != "first" {
		t.Fatalf("got %+v; want first registered route at equal priority", outcome)
	}
}

func TestRouteTableMountNilRejected(t *testing.T) {
	t.Parallel()

	_, err := NewRouteTable[int, string]().
		Mount("nested", 1, nil, nil).
		Build()
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestRouteTableBuildRejectsHandlerAndNested(t *testing.T) {
	t.Parallel()

	table := NewRouteTable[int, string]().
		Route("valid", 1, nil, FromFunc(func(context.Context, int) (string, error) {
			return "ok", nil
		}))
	table.routes[0].handler = FromFunc(func(context.Context, int) (string, error) {
		return "handler", nil
	})
	table.routes[0].nested = &builtTable[int, string]{
		source: NewRouteTable[int, string](),
	}

	_, err := table.Build()
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestFromFuncNil(t *testing.T) {
	t.Parallel()

	_, err := InvokeRouteHandler(context.Background(), 0, FromFunc[int, string](nil))
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestRecorderAsyncAfterTerminalNoOp(t *testing.T) {
	t.Parallel()

	rec := NewResultRecorder[string]()
	rec.Stop("first", "")
	rec.Async("second", "async")

	if payload, ok := rec.Payload(); !ok || payload != "first" {
		t.Fatalf("got payload=%q ok=%v; want first", payload, ok)
	}
}
