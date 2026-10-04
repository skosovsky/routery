package attempt

import (
	"context"
	"errors"

	"github.com/skosovsky/routery"
)

// Evidence returns current host facts and replay permission for a failed invocation.
// Event identity is supplied by the host; this bridge does not count physical SDK attempts.
// Use execution.Sequence for live evidence, reconciliation and atomic authorization.
type Evidence[Req any] func(context.Context, Req, error) (Event, Replay, error)

// RetryPredicate combines error classification with explicit replay evidence.
// Missing or invalid evidence fails closed. It authorizes only Retry, never Fallback.
func RetryPredicate[Req any](classify func(error) bool, evidence Evidence[Req]) routery.RetryPredicate[Req] {
	return func(ctx context.Context, req Req, err error) bool {
		if ctx.Err() != nil || err == nil || classify == nil || evidence == nil ||
			errors.Is(
				err,
				routery.ErrInvalidConfig,
			) || errors.Is(err, ErrInvalidEvent) || errors.Is(err, ErrInvalidHint) || errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) || !classify(err) {
			return false
		}
		event, replay, evidenceErr := evidence(ctx, req, err)
		if evidenceErr != nil {
			return false
		}
		decision, decisionErr := Decide(ctx, event, 1, replay)
		return decisionErr == nil && decision.Action == Retry
	}
}
