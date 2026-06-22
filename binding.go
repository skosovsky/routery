package routery

import (
	"fmt"
	"strings"
)

// RouteBinding carries a caller-owned branch and payload together with an auditable snapshot.
type RouteBinding[Branch comparable, Binding any] struct {
	Branch   Branch
	Binding  Binding
	Snapshot RouteSnapshot[RouteBindingSnapshot]
}

// RouteBindingSnapshot captures route metadata and caller-supplied freshness data.
type RouteBindingSnapshot struct {
	Match             RouteMatch
	InputFingerprint  string
	Revision          string
	DecisionReason    any
	HasDecisionReason bool
}

// SnapshotFreshnessPolicy validates whether a stored snapshot is still current.
type SnapshotFreshnessPolicy[TState any, Current any] interface {
	ValidateSnapshot(RouteSnapshot[TState], Current) error
}

// SnapshotFreshnessPolicyFunc adapts a function to SnapshotFreshnessPolicy.
type SnapshotFreshnessPolicyFunc[TState any, Current any] func(RouteSnapshot[TState], Current) error

// ValidateSnapshot implements SnapshotFreshnessPolicy.
func (fn SnapshotFreshnessPolicyFunc[TState, Current]) ValidateSnapshot(
	snapshot RouteSnapshot[TState],
	current Current,
) error {
	if fn == nil {
		return configError("snapshot freshness policy is nil")
	}

	return fn(snapshot, current)
}

// NewRouteBinding creates a typed route binding with route metadata and freshness data.
func NewRouteBinding[Branch comparable, Binding any](
	branch Branch,
	binding Binding,
	match RouteMatch,
	inputFingerprint string,
	revision string,
) RouteBinding[Branch, Binding] {
	state := RouteBindingSnapshot{
		Match:             cloneRouteMatch(match),
		InputFingerprint:  inputFingerprint,
		Revision:          revision,
		DecisionReason:    match.DecisionReason,
		HasDecisionReason: match.HasDecisionReason,
	}

	return RouteBinding[Branch, Binding]{
		Branch:  branch,
		Binding: binding,
		Snapshot: RouteSnapshot[RouteBindingSnapshot]{
			Fingerprint: fingerprintBindingSnapshot(state),
			State:       state,
		},
	}
}

// HandledBinding returns a terminal result whose payload is a typed route binding.
func HandledBinding[Kind comparable, Reason comparable, Branch comparable, Binding any](
	kind Kind,
	reason Reason,
	binding RouteBinding[Branch, Binding],
) RouteResult[Kind, Reason, RouteBinding[Branch, Binding]] {
	return Handled(kind, reason, binding)
}

// ValidateSnapshotFreshness runs policy against snapshot and current state.
func ValidateSnapshotFreshness[TState any, Current any](
	snapshot RouteSnapshot[TState],
	current Current,
	policy SnapshotFreshnessPolicy[TState, Current],
) error {
	if policy == nil {
		return configError("snapshot freshness policy is nil")
	}

	return policy.ValidateSnapshot(snapshot, current)
}

func cloneRouteMatch(match RouteMatch) RouteMatch {
	match.Path = append([]RouteID(nil), match.Path...)
	return match
}

func fingerprintBindingSnapshot(snapshot RouteBindingSnapshot) string {
	path := make([]string, 0, len(snapshot.Match.Path))
	for _, id := range snapshot.Match.Path {
		path = append(path, string(id))
	}

	return FingerprintSHA256(
		[]byte(string(snapshot.Match.RouteID)),
		[]byte(strings.Join(path, "/")),
		fmt.Append(nil, snapshot.Match.Priority),
		fmt.Append(nil, snapshot.Match.Depth),
		[]byte(string(snapshot.Match.Kind)),
		[]byte(snapshot.Match.Key),
		[]byte(snapshot.Match.Prefix),
		[]byte(snapshot.Match.Remainder),
		[]byte(snapshot.InputFingerprint),
		[]byte(snapshot.Revision),
		fmt.Append(nil, snapshot.HasDecisionReason),
		fmt.Appendf(nil, "%T:%v", snapshot.DecisionReason, snapshot.DecisionReason),
	)
}
