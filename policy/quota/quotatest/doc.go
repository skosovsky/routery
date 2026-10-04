// Package quotatest supplies opt-in conformance checks for host quota.Backend implementations.
// It is testing support; runtime packages never import it. Fixtures own storage and faults.
// RequiredPassed excludes unsupported optional capabilities; Complete requires all scenarios.
// A passing in-process fixture is not evidence of crash durability or exactly-once effects.
package quotatest
