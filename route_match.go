package routery

// MatchKind describes how a route entry matched the request.
type MatchKind string

const (
	MatchKindPredicate MatchKind = "predicate"
	MatchKindExact     MatchKind = "exact"
	MatchKindPrefix    MatchKind = "prefix"
	MatchKindDecision  MatchKind = "decision"
	MatchKindFallback  MatchKind = "fallback"
)

// RouteMatch contains engine-produced route metadata for handlers, middleware,
// observability callbacks, and dispatch results.
type RouteMatch struct {
	RouteID           RouteID
	Path              []RouteID
	Priority          int
	Depth             int
	Kind              MatchKind
	Key               string
	Prefix            string
	Remainder         string
	DecisionReason    any
	HasDecisionReason bool
}

// MatchDecisionReason returns the classifier reason carried by a decision match.
func MatchDecisionReason[Reason comparable](match RouteMatch) (Reason, bool) {
	var zero Reason
	if !match.HasDecisionReason {
		return zero, false
	}

	reason, ok := match.DecisionReason.(Reason)
	return reason, ok
}

func zeroRouteMatch() RouteMatch {
	return RouteMatch{
		RouteID:           "",
		Path:              nil,
		Priority:          0,
		Depth:             0,
		Kind:              "",
		Key:               "",
		Prefix:            "",
		Remainder:         "",
		DecisionReason:    nil,
		HasDecisionReason: false,
	}
}
