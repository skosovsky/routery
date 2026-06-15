package routery

import (
	"context"
	"errors"
	"testing"
)

func TestRouterDispatchStop(t *testing.T) {
	t.Parallel()

	router, err := NewRouteTable[int, string]().
		Route("ok", 1, nil, func(_ context.Context, req int, rec ResultRecorder[string]) error {
			rec.Stop("value:"+itoaHelper(req), "")
			return nil
		}).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	outcome, err := router.Dispatch(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.HasPayload || outcome.Payload != "value:7" {
		t.Fatalf("got %+v; want payload value:7", outcome)
	}
	if outcome.Action != ActionStop {
		t.Fatalf("got action %q; want stop", outcome.Action)
	}
}

func TestRouterDispatchNoMatch(t *testing.T) {
	t.Parallel()

	router, err := NewRouteTable[int, string]().
		Route("skip", 1, func(int) bool { return false }, FromFunc(func(context.Context, int) (string, error) {
			return "never", nil
		})).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	outcome, err := router.Dispatch(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.HasPayload {
		t.Fatalf("got payload %q; want none", outcome.Payload)
	}
	if outcome.ReasonCode != reasonNoMatch {
		t.Fatalf("got reason %q; want %q", outcome.ReasonCode, reasonNoMatch)
	}
	if outcome.Action != ActionStop {
		t.Fatalf("got action %q; want stop", outcome.Action)
	}
}

func TestRouterNestedNoMatchReturnsToParent(t *testing.T) {
	t.Parallel()

	nested := NewRouteTable[int, string]().
		Route("inner", 1, func(int) bool { return false }, FromFunc(func(context.Context, int) (string, error) {
			return "inner", nil
		}))

	router, err := NewRouteTable[int, string]().
		Mount("group", 10, nil, nested).
		Route("outer", 1, nil, FromFunc(func(context.Context, int) (string, error) {
			return "outer", nil
		})).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	outcome, err := router.Dispatch(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.HasPayload || outcome.Payload != "outer" {
		t.Fatalf("got %+v; want outer", outcome)
	}
}

func TestRouterNestedStopPreventsParent(t *testing.T) {
	t.Parallel()

	nested := NewRouteTable[int, string]().
		Route("inner", 1, nil, FromFunc(func(context.Context, int) (string, error) {
			return "inner", nil
		}))

	router, err := NewRouteTable[int, string]().
		Mount("group", 10, nil, nested).
		Route("outer", 1, nil, FromFunc(func(context.Context, int) (string, error) {
			return "outer", nil
		})).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	outcome, err := router.Dispatch(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.HasPayload || outcome.Payload != "inner" {
		t.Fatalf("got %+v; want inner", outcome)
	}
}

func TestRouterFallback(t *testing.T) {
	t.Parallel()

	router, err := NewRouteTable[int, string]().
		Route("skip", 1, func(int) bool { return false }, FromFunc(func(context.Context, int) (string, error) {
			return "route", nil
		})).
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

func TestRouterFallbackNextAtRootBecomesNoMatch(t *testing.T) {
	t.Parallel()

	router, err := NewRouteTable[int, string]().
		Route("skip", 1, func(int) bool { return false }, FromFunc(func(context.Context, int) (string, error) {
			return "route", nil
		})).
		Fallback(func(_ context.Context, _ int, rec ResultRecorder[string]) error {
			rec.Next("delegate")
			return nil
		}).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	outcome, err := router.Dispatch(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.HasPayload {
		t.Fatal("expected no payload")
	}
	if outcome.ReasonCode != reasonNoMatch {
		t.Fatalf("got reason %q; want %q", outcome.ReasonCode, reasonNoMatch)
	}
}

func TestRouterNestedIgnorePreservesReasonCode(t *testing.T) {
	t.Parallel()

	nested := NewRouteTable[int, string]().
		Route("inner", 1, nil, func(_ context.Context, _ int, rec ResultRecorder[string]) error {
			rec.Ignore("skipped")
			return nil
		})

	router, err := NewRouteTable[int, string]().
		Mount("group", 10, nil, nested).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	outcome, err := router.Dispatch(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.HasPayload {
		t.Fatal("expected no payload")
	}
	if outcome.ReasonCode != "skipped" {
		t.Fatalf("got reason %q; want skipped", outcome.ReasonCode)
	}
}

func TestRouterDispatchHandlerError(t *testing.T) {
	t.Parallel()

	handlerErr := errors.New("handler failed")
	router, err := NewRouteTable[int, string]().
		Route("fail", 1, nil, func(_ context.Context, _ int, _ ResultRecorder[string]) error {
			return handlerErr
		}).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	outcome, err := router.Dispatch(context.Background(), 0)
	if !errors.Is(err, handlerErr) {
		t.Fatalf("got err %v; want %v", err, handlerErr)
	}
	if outcome.HasPayload {
		t.Fatal("expected no payload on error")
	}
	if outcome.Action != ActionAbort {
		t.Fatalf("got action %q; want abort", outcome.Action)
	}
}

func TestRouterMatchedIgnoreSkipsFallback(t *testing.T) {
	t.Parallel()

	fallbackCalled := false
	router, err := NewRouteTable[int, string]().
		Route("match", 1, nil, func(_ context.Context, _ int, rec ResultRecorder[string]) error {
			rec.Ignore("declined")
			return nil
		}).
		Fallback(func(_ context.Context, _ int, rec ResultRecorder[string]) error {
			fallbackCalled = true
			rec.Stop("fallback", "")
			return nil
		}).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	outcome, err := router.Dispatch(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if fallbackCalled {
		t.Fatal("expected fallback to be skipped after Ignore")
	}
	if outcome.HasPayload {
		t.Fatal("expected no payload")
	}
	if outcome.ReasonCode != "declined" {
		t.Fatalf("got reason %q; want declined", outcome.ReasonCode)
	}
	if outcome.Action != ActionStop {
		t.Fatalf("got action %q; want stop", outcome.Action)
	}
}

func TestRouterMatchedNextReachesFallback(t *testing.T) {
	t.Parallel()

	fallbackCalled := false
	router, err := NewRouteTable[int, string]().
		Route("match", 1, nil, func(_ context.Context, _ int, rec ResultRecorder[string]) error {
			rec.Next("delegate")
			return nil
		}).
		Fallback(func(_ context.Context, _ int, rec ResultRecorder[string]) error {
			fallbackCalled = true
			rec.Stop("fallback", "")
			return nil
		}).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	outcome, err := router.Dispatch(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !fallbackCalled {
		t.Fatal("expected fallback to run after Next")
	}
	if !outcome.HasPayload || outcome.Payload != "fallback" {
		t.Fatalf("got %+v; want fallback payload", outcome)
	}
	if outcome.Action != ActionStop {
		t.Fatalf("got action %q; want stop", outcome.Action)
	}
}

func TestRouterSilentHandlerContinuesTraversal(t *testing.T) {
	t.Parallel()

	router, err := NewRouteTable[int, string]().
		Route("silent", 10, nil, func(_ context.Context, _ int, _ ResultRecorder[string]) error {
			return nil
		}).
		Route("second", 1, nil, FromFunc(func(context.Context, int) (string, error) {
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
	if !outcome.HasPayload || outcome.Payload != "second" {
		t.Fatalf("got %+v; want second route payload", outcome)
	}
	if outcome.Action != ActionStop {
		t.Fatalf("got action %q; want stop", outcome.Action)
	}
}

func TestRouterNestedFallbackStop(t *testing.T) {
	t.Parallel()

	nested := NewRouteTable[int, string]().
		Route("skip", 1, func(int) bool { return false }, FromFunc(func(context.Context, int) (string, error) {
			return "route", nil
		})).
		Fallback(FromFunc(func(context.Context, int) (string, error) {
			return "nested-fallback", nil
		}))

	outerCalled := false
	router, err := NewRouteTable[int, string]().
		Mount("group", 10, nil, nested).
		Route("outer", 1, nil, func(_ context.Context, _ int, _ ResultRecorder[string]) error {
			outerCalled = true
			return nil
		}).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	outcome, err := router.Dispatch(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if outerCalled {
		t.Fatal("expected parent route to be skipped after nested fallback stop")
	}
	if !outcome.HasPayload || outcome.Payload != "nested-fallback" {
		t.Fatalf("got %+v; want nested-fallback", outcome)
	}
	if outcome.Action != ActionStop {
		t.Fatalf("got action %q; want stop", outcome.Action)
	}
}

func TestRouterNestedHandlerErrorAborts(t *testing.T) {
	t.Parallel()

	handlerErr := errors.New("nested failed")
	nested := NewRouteTable[int, string]().
		Route("inner", 1, nil, func(_ context.Context, _ int, _ ResultRecorder[string]) error {
			return handlerErr
		})

	outerCalled := false
	router, err := NewRouteTable[int, string]().
		Mount("group", 10, nil, nested).
		Route("outer", 1, nil, func(_ context.Context, _ int, rec ResultRecorder[string]) error {
			outerCalled = true
			rec.Stop("outer", "")
			return nil
		}).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	outcome, err := router.Dispatch(context.Background(), 0)
	if !errors.Is(err, handlerErr) {
		t.Fatalf("got err %v; want %v", err, handlerErr)
	}
	if outerCalled {
		t.Fatal("expected parent route to be skipped after nested handler error")
	}
	if outcome.HasPayload {
		t.Fatal("expected no payload on error")
	}
	if outcome.Action != ActionAbort {
		t.Fatalf("got action %q; want abort", outcome.Action)
	}
}

func TestRouterMountMatcherSkipsNested(t *testing.T) {
	t.Parallel()

	nested := NewRouteTable[int, string]().
		Route("inner", 1, nil, FromFunc(func(context.Context, int) (string, error) {
			return "inner", nil
		}))

	router, err := NewRouteTable[int, string]().
		Mount("group", 10, func(int) bool { return false }, nested).
		Route("outer", 1, nil, FromFunc(func(context.Context, int) (string, error) {
			return "outer", nil
		})).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	outcome, err := router.Dispatch(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.HasPayload || outcome.Payload != "outer" {
		t.Fatalf("got %+v; want outer", outcome)
	}
}

func TestRouterNestedFallbackNextReturnsToParent(t *testing.T) {
	t.Parallel()

	nested := NewRouteTable[int, string]().
		Route("skip", 1, func(int) bool { return false }, FromFunc(func(context.Context, int) (string, error) {
			return "route", nil
		})).
		Fallback(func(_ context.Context, _ int, rec ResultRecorder[string]) error {
			rec.Next("delegate")
			return nil
		})

	router, err := NewRouteTable[int, string]().
		Mount("group", 10, nil, nested).
		Route("outer", 1, nil, FromFunc(func(context.Context, int) (string, error) {
			return "outer", nil
		})).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	outcome, err := router.Dispatch(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.HasPayload || outcome.Payload != "outer" {
		t.Fatalf("got %+v; want outer after nested fallback next", outcome)
	}
}

func TestRouterDispatchAsync(t *testing.T) {
	t.Parallel()

	router, err := NewRouteTable[int, string]().
		Route("async", 1, nil, func(_ context.Context, _ int, rec ResultRecorder[string]) error {
			rec.Async("accepted", "")
			return nil
		}).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	outcome, err := router.Dispatch(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !outcome.HasPayload || outcome.Payload != "accepted" {
		t.Fatalf("got %+v; want async payload", outcome)
	}
	if outcome.Action != ActionStop {
		t.Fatalf("got action %q; want stop", outcome.Action)
	}
	if outcome.ReasonCode != reasonAsyncAccepted {
		t.Fatalf("got reason %q; want %q", outcome.ReasonCode, reasonAsyncAccepted)
	}
}

func itoaHelper(value int) string {
	if value == 0 {
		return "0"
	}

	negative := value < 0
	if negative {
		value = -value
	}

	digits := make([]byte, 0, 12)
	for value > 0 {
		digits = append(digits, byte('0'+value%10))
		value /= 10
	}
	for left, right := 0, len(digits)-1; left < right; left, right = left+1, right-1 {
		digits[left], digits[right] = digits[right], digits[left]
	}
	if negative {
		return "-" + string(digits)
	}
	return string(digits)
}
