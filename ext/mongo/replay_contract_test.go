package routerymongo

import (
	"context"
	"errors"
	"testing"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

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
			writer := &lostUpdate{calls: &calls, effects: &effects, dedup: safe}
			base := NewUpdateOneRouteHandler(writer)
			var evidence attempt.Evidence[UpdateOneRequest]
			if safe {
				evidence = replayEvidence[UpdateOneRequest](true, attempt.Unknown)
			}
			handler := routery.ApplyRoute(
				base,
				routery.RetryIf[UpdateOneRequest, routery.BasicKind, routery.BasicReason, *mongo.UpdateResult](
					3,
					0,
					RetryPolicy(evidence),
				),
			)
			// Act.
			_, err := routery.InvokeRouteHandler(context.Background(), UpdateOneRequest{}, handler)
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

type lostUpdate struct {
	calls, effects *int
	dedup          bool
}

func (writer *lostUpdate) UpdateOne(context.Context, any, any, ...*options.UpdateOptions) (*mongo.UpdateResult, error) {
	*writer.calls++
	if !writer.dedup || *writer.effects == 0 {
		*writer.effects++
	}
	return nil, mongo.CommandError{Labels: []string{"NetworkError"}}
}
func TestTransactionEvidenceCannotAuthorizeRepeat(t *testing.T) {
	t.Parallel()
	// Arrange.
	predicate := RetryPolicy(replayEvidence[any](true, attempt.Unknown))
	// Act.
	allowed := predicate(context.Background(), txFlag(true), mongo.ErrClientDisconnected)
	// Assert.
	if allowed {
		t.Fatal("transaction replay authorized")
	}
}

type partialFind struct {
	cursor *mongo.Cursor
	err    error
}

func (fixture partialFind) Find(context.Context, any, ...*options.FindOptions) (*mongo.Cursor, error) {
	return fixture.cursor, fixture.err
}
func TestPartialCursorRetainsLifetimeOnError(t *testing.T) {
	t.Parallel()
	// Arrange.
	cursor, createErr := mongo.NewCursorFromDocuments([]any{}, nil, nil)
	if createErr != nil {
		t.Fatal(createErr)
	}
	providerErr := mongo.ErrClientDisconnected
	handler := NewFindRouteHandler(partialFind{cursor: cursor, err: providerErr})
	// Act.
	result, err := routery.InvokeRouteHandler(context.Background(), FindRequest{}, handler)
	// Assert.
	if !errors.Is(err, providerErr) || !result.HasPayload || result.Payload != cursor || result.Lifetime == nil {
		t.Fatal(result, err)
	}
	if closeErr := result.Lifetime.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if closeErr := result.Lifetime.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
}
