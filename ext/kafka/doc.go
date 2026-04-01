// Package routerykafka adapts Kafka producers ([github.com/segmentio/kafka-go]) to
// [github.com/skosovsky/routery.Executor] for use with middleware such as [github.com/skosovsky/routery.RetryIf]
// and [github.com/skosovsky/routery.Bulkhead].
//
// Idempotent producer: enable idempotent writes in your Kafka cluster / producer configuration.
// Retries after a client timeout can still duplicate records at the broker if the first attempt
// actually succeeded — idempotent semantics reduce duplicates.
//
// This package targets producers only; consumers are out of scope.
package routerykafka
