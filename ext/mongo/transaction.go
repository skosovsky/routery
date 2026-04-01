package routerymongo

import (
	"context"

	"go.mongodb.org/mongo-driver/mongo"
)

// TransactionalRequest allows request types to opt out of retries when they represent work
// inside a transaction, even if the context does not expose session state.
type TransactionalRequest interface {
	MongoInTransaction() bool
}

func inMongoTransaction(ctx context.Context) bool {
	sess := mongo.SessionFromContext(ctx)
	if sess == nil {
		return false
	}
	xs, ok := sess.(mongo.XSession) //nolint:staticcheck // read TransactionRunning from driver session.
	if !ok {
		return false
	}
	return xs.ClientSession().TransactionRunning()
}

func transactionFromRequest[Req any](req Req) bool {
	r, ok := any(req).(TransactionalRequest)
	return ok && r.MongoInTransaction()
}
