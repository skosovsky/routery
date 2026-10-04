package routerymongo

import (
	"context"
	"errors"

	"github.com/skosovsky/routery"
	"github.com/skosovsky/routery/policy/attempt"

	"go.mongodb.org/mongo-driver/mongo"
)

const (
	mongoErrUnauthorized       = 13
	mongoErrDocumentValidation = 121
	mongoErrCannotCreateIndex  = 66
)

// IsTransientError classifies provider errors without authorizing repetition.
// Replay safety must be provided separately through RetryPolicy or execution.Sequence.
func IsTransientError(err error) bool {
	if err == nil || errors.Is(err, routery.ErrInvalidConfig) {
		return false
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	if errors.Is(err, mongo.ErrNoDocuments) {
		return false
	}

	if mongo.IsDuplicateKeyError(err) {
		return false
	}

	if errors.Is(err, mongo.ErrClientDisconnected) {
		return true
	}

	if mongo.IsNetworkError(err) || mongo.IsTimeout(err) {
		return true
	}

	if we, ok := errors.AsType[mongo.WriteException](err); ok {
		for _, e := range we.WriteErrors {
			if isMongoAuthOrValidationCode(e.Code) {
				return false
			}
		}
		if we.WriteConcernError != nil && isMongoAuthOrValidationCode(we.WriteConcernError.Code) {
			return false
		}
	}

	return false
}

func isMongoAuthOrValidationCode(code int) bool {
	switch code {
	case mongoErrUnauthorized, mongoErrDocumentValidation, mongoErrCannotCreateIndex:
		return true
	default:
		return false
	}
}

// RetryPolicy requires explicit host evidence; nil evidence denies repetition.
func RetryPolicy[Req any](evidence attempt.Evidence[Req]) routery.RetryPredicate[Req] {
	predicate := attempt.RetryPredicate(IsTransientError, evidence)
	return func(ctx context.Context, req Req, err error) bool {
		if inMongoTransaction(ctx) || transactionFromRequest(req) {
			return false
		}
		return predicate(ctx, req, err)
	}
}
