// Package quota defines optional caller-owned atomic admission and settlement ports.
// Durable state, consistency, TTL, pricing and distributed idempotency belong to Backend.
// Host implementations can opt into the separate quotatest conformance package.
// Testing fixtures do not prove storage crash durability or exactly-once remote effects.
package quota
