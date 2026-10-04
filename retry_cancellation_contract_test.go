package routery

import (
	"context"
	"errors"
	"testing"
)

func TestRetryIfFailedCallCancellationPrecedence(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name                            string
		attempts                        int
		cancelInCall, cancelInPredicate bool
	}{
		{"final-attempt", 1, true, false}, {"rejecting-predicate", 3, false, true}, {"failed-call", 3, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange.
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			providerErr := errors.New("provider failed")
			calls, closes := 0, 0
			base := BasicRouteHandler[int, string](func(RouteCall[int]) (BasicRouteResult[string], error) {
				calls++
				if test.cancelInCall {
					cancel()
				}
				result := BasicHandled("partial")
				result.Lifetime = NewLifetime(func() error { closes++; return nil })
				return result, providerErr
			})
			predicate := func(context.Context, int, error) bool {
				if test.cancelInPredicate {
					cancel()
				}
				return false
			}
			handler := ApplyRoute(base, RetryIf[int, BasicKind, BasicReason, string](test.attempts, 0, predicate))
			// Act.
			result, err := InvokeRouteHandler(ctx, 1, handler)
			// Assert.
			if !errors.Is(err, context.Canceled) || !errors.Is(err, providerErr) || calls != 1 || !result.HasPayload ||
				closes != 0 {
				t.Fatalf("error=%v result=%+v calls=%d closes=%d", err, result, calls, closes)
			}
			if closeErr := result.Lifetime.Close(); closeErr != nil || closes != 1 {
				t.Fatal(closeErr, closes)
			}
		})
	}
}
func TestRetryIfSuccessRemainsSuccessAfterCancellation(t *testing.T) {
	t.Parallel()
	// Arrange.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	base := BasicRouteHandler[int, string](
		func(RouteCall[int]) (BasicRouteResult[string], error) { cancel(); return BasicHandled("complete"), nil },
	)
	handler := ApplyRoute(
		base,
		RetryIf[int, BasicKind, BasicReason, string](3, 0, func(context.Context, int, error) bool { return true }),
	)
	// Act.
	result, err := InvokeRouteHandler(ctx, 1, handler)
	// Assert.
	if err != nil || result.Payload != "complete" {
		t.Fatal(result, err)
	}
}
