// Package model projects caller-owned model facts into generic routing policies.
// It contains no model catalog, SDK, price updater or quality evaluator.
// Optional/default quality is sanitized against the current request Task and measurement freshness.
// Defaults cannot satisfy mandatory quality; stale descriptors still fail the whole selection.
package model
