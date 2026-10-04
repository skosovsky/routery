// Package routerykafka adapts Kafka producers ([github.com/segmentio/kafka-go]) to
// [github.com/skosovsky/routery.RouteHandler] for use with middleware such as [github.com/skosovsky/routery.RetryIf]
// and [github.com/skosovsky/routery.Bulkhead].
//
// MessageWriter is synchronous with broker acknowledgements; the host guarantees
// this for custom writers. Concrete kafka.Writer Async or RequireNone configurations
// are rejected. Messages/bytes are borrowed until return. PublishResult preserves
// per-message facts. Producer deduplication across external calls is host evidence.
//
// This package targets producers only; consumers are out of scope.
//
// Client/SDK retries are unobservable here. Configure one retry owner; see
// docs/adapter-replay-contracts.md in the routery repository for client controls.
// One handler invocation does not promise one physical network attempt.
package routerykafka
