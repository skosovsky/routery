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
	base := routery.FromFunc(func(context.Context, int) (int, error) {
		calls++
		return 1, nil
	})

	handler := routery.ApplyRoute(base, Metrics[int, int]("operation", MetricsHooks[int]{}))
	outcome, err := routery.InvokeRouteHandler(context.Background(), 0, handler)
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if !outcome.HasPayload || outcome.Payload != 1 {
		t.Fatalf("unexpected result: got %d, want 1", outcome.Payload)
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
		gotAction      routery.RouteAction
		gotReasonCode  string
		gotPayloadMeta PayloadMeta
	)

	expectedErr := errors.New("failure")
	base := func(_ context.Context, _ int, rec routery.ResultRecorder[int]) error {
		rec.Ignore("not_found")
		return expectedErr
	}

	handler := routery.ApplyRoute(base, Metrics[int, int]("primary", MetricsHooks[int]{
		OnStart: func(context.Context, string) {
			startCalls.Add(1)
		},
		OnComplete: func(_ context.Context, name string, duration time.Duration, result ResultMeta, payloadMeta PayloadMeta, err error) {
			completeCalls.Add(1)
			gotName = name
			gotDuration = duration
			gotErr = err
			gotAction = result.Action
			gotReasonCode = result.ReasonCode
			gotPayloadMeta = payloadMeta
		},
	}))

	_, err := routery.InvokeRouteHandler(context.Background(), 0, handler)
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
	if gotAction != routery.ActionAbort {
		t.Fatalf("unexpected action: got %q, want abort", gotAction)
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
	base := routery.FromFunc(func(context.Context, int) (int, error) {
		return 7, nil
	})

	handler := routery.ApplyRoute(base, Metrics[int, int]("primary", MetricsHooks[int]{
		OnComplete: func(_ context.Context, _ string, _ time.Duration, _ ResultMeta, payloadMeta PayloadMeta, _ error) {
			gotMeta = payloadMeta
		},
		PayloadMeta: func(_ context.Context, _ routery.ResultRecorder[int]) PayloadMeta {
			return PayloadMeta{
				Shape:       "custom",
				Fingerprint: routery.FingerprintSHA256([]byte("7")),
			}
		},
	}))

	_, err := routery.InvokeRouteHandler(context.Background(), 0, handler)
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

func TestMetricsEmitsActionNext(t *testing.T) {
	t.Parallel()

	var gotAction routery.RouteAction
	base := func(_ context.Context, _ int, rec routery.ResultRecorder[int]) error {
		rec.Next("delegate")
		return nil
	}

	handler := routery.ApplyRoute(base, Metrics[int, int]("next", MetricsHooks[int]{
		OnComplete: func(_ context.Context, _ string, _ time.Duration, result ResultMeta, _ PayloadMeta, _ error) {
			gotAction = result.Action
		},
	}))

	outcome, err := routery.InvokeRouteHandler(context.Background(), 0, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Action != routery.ActionNext {
		t.Fatalf("got outcome action %q; want next", outcome.Action)
	}
	if gotAction != routery.ActionNext {
		t.Fatalf("got metrics action %q; want next", gotAction)
	}
}

func TestMetricsEmitsOutcomeOnError(t *testing.T) {
	t.Parallel()

	var gotAction routery.RouteAction
	handlerErr := errors.New("handler failed")
	base := func(_ context.Context, _ int, _ routery.ResultRecorder[int]) error {
		return handlerErr
	}

	handler := routery.ApplyRoute(base, Metrics[int, int]("error", MetricsHooks[int]{
		OnComplete: func(_ context.Context, _ string, _ time.Duration, result ResultMeta, payloadMeta PayloadMeta, err error) {
			gotAction = result.Action
			if payloadMeta.Shape != shapeEmpty {
				t.Errorf("got shape %q; want empty", payloadMeta.Shape)
			}
			if !errors.Is(err, handlerErr) {
				t.Errorf("got err %v; want %v", err, handlerErr)
			}
		},
	}))

	_, err := routery.InvokeRouteHandler(context.Background(), 0, handler)
	if !errors.Is(err, handlerErr) {
		t.Fatalf("got err %v; want %v", err, handlerErr)
	}
	if gotAction != routery.ActionAbort {
		t.Fatalf("got action %q; want abort", gotAction)
	}
}

func TestMetricsReturnsConfigErrorWhenNextRouteHandlerIsNil(t *testing.T) {
	t.Parallel()

	handler := routery.ApplyRoute[int, int](nil, Metrics[int, int]("primary", MetricsHooks[int]{
		OnStart: func(context.Context, string) {},
	}))

	_, err := routery.InvokeRouteHandler(context.Background(), 0, handler)
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}
