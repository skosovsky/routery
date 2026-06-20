package routerymongo

import (
	"context"

	"go.mongodb.org/mongo-driver/mongo"
	driversession "go.mongodb.org/mongo-driver/x/mongo/driver/session"
)

// TransactionalRequest allows request types to opt out of retries when they represent work
// inside a transaction, even if the context does not expose session state.
type TransactionalRequest interface {
	MongoInTransaction() bool
}

type clientSessionProvider interface {
	ClientSession() *driversession.Client
}

func inMongoTransaction(ctx context.Context) bool {
	sess := mongo.SessionFromContext(ctx)
	if sess == nil {
		return false
	}
	xs, ok := sess.(clientSessionProvider)
	if !ok {
		return false
	}
	return xs.ClientSession().TransactionRunning()
}

func transactionFromRequest[Req any](req Req) bool {
	r, ok := any(req).(TransactionalRequest)
	return ok && r.MongoInTransaction()
}
