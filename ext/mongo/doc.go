// Package routerymongo wraps MongoDB collection operations as [github.com/skosovsky/routery.RouteHandler]
// values with an explicit [RetryPolicy].
//
// Retries are disabled when the context carries an active multi-document transaction (see
// [mongo.SessionFromContext] and the driver's session APIs) or when the request implements
// [TransactionalRequest] with [TransactionalRequest.MongoInTransaction] true.
//
// Combine with [github.com/skosovsky/routery.RetryIf] for resilient CRUD calls.
// Find results own their cursor through result.Lifetime. Close that lifetime rather
// than only the cursor to release routing callbacks as well. Cursor cleanup uses a
// context without cancellation so that loser cancellation does not suppress cleanup;
// the host remains responsible for driver network timeouts.
//
// Client/SDK retries are unobservable here. Configure one retry owner; see
// docs/adapter-replay-contracts.md in the routery repository for client controls.
// One handler invocation does not promise one physical network attempt.
package routerymongo
