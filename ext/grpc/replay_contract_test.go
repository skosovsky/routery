package routerygrpc

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/skosovsky/routery"
	"github.com/skosovsky/routery/policy/attempt"
)

func TestLostResponseReplayEvidence(t *testing.T) {
	t.Parallel()
	for _, safe := range []bool{false, true} {
		t.Run(map[bool]string{false: "unknown-denied", true: "verified-dedup"}[safe], func(t *testing.T) {
			t.Parallel()
			// Arrange: fixture's dedup flag models a verified endpoint guarantee.
			calls, effects := 0, 0
			base := NewUnaryRouteHandler(func(context.Context, int) (string, error) {
				calls++
				if !safe || effects == 0 {
					effects++
				}
				return "", status.Error(codes.Unavailable, "response lost")
			})
			var evidence attempt.Evidence[int]
			if safe {
				evidence = replayEvidence[int](true, attempt.Unknown)
			}
			handler := routery.ApplyRoute(
				base,
				routery.RetryIf[int, routery.BasicKind, routery.BasicReason, string](3, 0, RetryPolicy(evidence)),
			)
			// Act.
			_, err := routery.InvokeRouteHandler(context.Background(), 0, handler)
			// Assert.
			expectedCalls := 1
			if safe {
				expectedCalls = 3
			}
			if err == nil || calls != expectedCalls || effects != 1 {
				t.Fatalf("error=%v calls=%d effects=%d", err, calls, effects)
			}
		})
	}
}

func replayEvidence[Req any](safe bool, outcome attempt.Outcome) attempt.Evidence[Req] {
	return func(context.Context, Req, error) (attempt.Event, attempt.Replay, error) {
		return attempt.Event{
				Identity: attempt.Identity{Operation: "effect", Attempt: "lost-response"},
				Phase:    attempt.Terminal,
				Outcome:  outcome,
			},
			attempt.Replay{
				Retryable:     true,
				Replayable:    true,
				SafeDuplicate: safe,
			}, nil
	}
}
