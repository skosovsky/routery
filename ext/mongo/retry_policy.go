package routerymongo

import (
	"context"
	"errors"

	"go.mongodb.org/mongo-driver/mongo"
)

const (
	mongoErrUnauthorized       = 13
	mongoErrDocumentValidation = 121
	mongoErrCannotCreateIndex  = 66
)

// DefaultRetryPolicy classifies MongoDB errors for [github.com/skosovsky/routery.RetryIf].
func DefaultRetryPolicy[Req any](ctx context.Context, req Req, err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	if inMongoTransaction(ctx) || transactionFromRequest(req) {
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

	var we mongo.WriteException
	if errors.As(err, &we) {
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
