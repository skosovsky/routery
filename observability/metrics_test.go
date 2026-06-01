package observability

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/skosovsky/routery"
)

func TestMetricsNoOpWhenHooksAreEmpty(t *testing.T) {
	t.Parallel()

	calls := 0
	base := routery.HandlerFunc[int, int](func(context.Context, int) (routery.RouteResult[int], error) {
		calls++
		return routery.Handled(1), nil
	})

	result, err := Metrics[int, int]("operation", MetricsHooks[int]{})(base).Handle(context.Background(), 0)
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if result.Payload != 1 {
		t.Fatalf("unexpected result: got %d, want 1", result.Payload)
	}
	if calls != 1 {
		t.Fatalf("unexpected call count: got %d, want 1", calls)
	}
}

func TestMetricsCallsHooks(t *testing.T) {
	t.Parallel()

	startCalls := atomic.Int32{}
	completeCalls := atomic.Int32{}
	var (
		gotName        string
		gotDuration    time.Duration
		gotErr         error
		gotStatus      routery.RouteStatus
		gotReasonCode  string
		gotPayloadMeta PayloadMeta
	)

	expectedErr := errors.New("failure")
	base := routery.HandlerFunc[int, int](func(context.Context, int) (routery.RouteResult[int], error) {
		return routery.Ignored[int]("not_found"), expectedErr
	})

	executor := Metrics[int, int]("primary", MetricsHooks[int]{
		OnStart: func(context.Context, string) {
			startCalls.Add(1)
		},
		OnComplete: func(_ context.Context, name string, duration time.Duration, result ResultMeta, payloadMeta PayloadMeta, err error) {
			completeCalls.Add(1)
			gotName = name
			gotDuration = duration
			gotErr = err
			gotStatus = result.Status
			gotReasonCode = result.ReasonCode
			gotPayloadMeta = payloadMeta
		},
	})(base)

	_, err := executor.Handle(context.Background(), 0)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected wrapped error, got %v", err)
	}

	if startCalls.Load() != 1 {
		t.Fatalf("unexpected start calls: got %d, want 1", startCalls.Load())
	}
	if completeCalls.Load() != 1 {
		t.Fatalf("unexpected complete calls: got %d, want 1", completeCalls.Load())
	}
	if gotName != "primary" {
		t.Fatalf("unexpected name: got %q, want %q", gotName, "primary")
	}
	if gotDuration < 0 {
		t.Fatalf("expected non-negative duration, got %v", gotDuration)
	}
	if !errors.Is(gotErr, expectedErr) {
		t.Fatalf("unexpected completion error: got %v, want %v", gotErr, expectedErr)
	}
	if gotStatus != routery.StatusIgnored {
		t.Fatalf("unexpected status: got %q, want ignored", gotStatus)
	}
	if gotReasonCode != "not_found" {
		t.Fatalf("unexpected reason code: got %q, want not_found", gotReasonCode)
	}
	if gotPayloadMeta.Shape != shapeEmpty {
		t.Fatalf("unexpected payload shape: got %q, want empty", gotPayloadMeta.Shape)
	}
}

func TestMetricsUsesCustomPayloadMeta(t *testing.T) {
	t.Parallel()

	var gotMeta PayloadMeta
	base := routery.HandlerFunc[int, int](func(context.Context, int) (routery.RouteResult[int], error) {
		return routery.Handled(7), nil
	})

	executor := Metrics[int, int]("primary", MetricsHooks[int]{
		OnComplete: func(_ context.Context, _ string, _ time.Duration, _ ResultMeta, payloadMeta PayloadMeta, _ error) {
			gotMeta = payloadMeta
		},
		PayloadMeta: func(_ context.Context, _ routery.RouteResult[int]) PayloadMeta {
			return PayloadMeta{
				Shape:       "custom",
				Fingerprint: routery.FingerprintSHA256([]byte("7")),
			}
		},
	})(base)

	_, err := executor.Handle(context.Background(), 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMeta.Shape != "custom" {
		t.Fatalf("got shape=%q, want custom", gotMeta.Shape)
	}
	if gotMeta.Fingerprint == "" {
		t.Fatal("expected non-empty fingerprint")
	}
}

func TestMetricsReturnsConfigErrorWhenNextExecutorIsNil(t *testing.T) {
	t.Parallel()

	executor := Metrics[int, int]("primary", MetricsHooks[int]{
		OnStart: func(context.Context, string) {},
	})(nil)

	_, err := executor.Handle(context.Background(), 0)
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}
