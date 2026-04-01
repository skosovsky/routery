// Package routerymongo wraps MongoDB collection operations as [github.com/skosovsky/routery.Executor]
// values with a shared [DefaultRetryPolicy].
//
// Retries are disabled when the context carries an active multi-document transaction (see
// [mongo.SessionFromContext] and the driver's session APIs) or when the request implements
// [TransactionalRequest] with [TransactionalRequest.MongoInTransaction] true.
//
// Combine with [github.com/skosovsky/routery.RetryIf] for resilient CRUD calls.
package routerymongo
