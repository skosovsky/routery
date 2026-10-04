package routerykafka

import (
	"context"
	"testing"

	"github.com/segmentio/kafka-go"

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
			writer := &lostPublish{calls: &calls, effects: &effects, dedup: safe}
			base := NewProducerRouteHandler(writer)
			var evidence attempt.Evidence[PublishRequest]
			if safe {
				evidence = replayEvidence[PublishRequest](true, attempt.Unknown)
			}
			handler := routery.ApplyRoute(
				base,
				routery.RetryIf[PublishRequest, routery.BasicKind, routery.BasicReason, PublishResult](
					3,
					0,
					RetryPolicy(evidence),
				),
			)
			// Act.
			_, err := routery.InvokeRouteHandler(
				context.Background(),
				PublishRequest{Messages: []kafka.Message{{Value: []byte("payload")}}},
				handler,
			)
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

type lostPublish struct {
	calls, effects *int
	dedup          bool
}

func (writer *lostPublish) WriteMessages(context.Context, ...kafka.Message) error {
	*writer.calls++
	if !writer.dedup || *writer.effects == 0 {
		*writer.effects++
	}
	return kafka.RequestTimedOut
}
