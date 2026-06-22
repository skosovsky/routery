package routery

import "fmt"

// DecisionResult is the typed output of a decision table.
type DecisionResult[Action comparable, Reason comparable] struct {
	Matched  bool
	Action   Action
	Reason   Reason
	Terminal bool
}

// DecisionTable maps typed input into ordered, caller-owned actions.
type DecisionTable[Input any, Action comparable, Reason comparable] interface {
	Decide(Input) (DecisionResult[Action, Reason], error)
}

// DecisionCaseMatcher decides whether a decision case applies to input.
type DecisionCaseMatcher[Input any] func(Input) (bool, error)

// DecisionTableBuilder builds an immutable ordered decision table.
type DecisionTableBuilder[Input any, Action comparable, Reason comparable] struct {
	cases []decisionCase[Input, Action, Reason]
}

type decisionCase[Input any, Action comparable, Reason comparable] struct {
	match    DecisionCaseMatcher[Input]
	action   Action
	reason   Reason
	terminal bool
}

type builtDecisionTable[Input any, Action comparable, Reason comparable] struct {
	cases []decisionCase[Input, Action, Reason]
}

// NewDecisionTable starts an ordered decision-table builder.
func NewDecisionTable[Input any, Action comparable, Reason comparable]() *DecisionTableBuilder[Input, Action, Reason] {
	return &DecisionTableBuilder[Input, Action, Reason]{
		cases: nil,
	}
}

// Case appends an ordered case that returns a terminal action when matched.
func (builder *DecisionTableBuilder[Input, Action, Reason]) Case(
	match DecisionCaseMatcher[Input],
	action Action,
	reason Reason,
) *DecisionTableBuilder[Input, Action, Reason] {
	return builder.CaseWithTerminal(match, action, reason, true)
}

// CaseWithTerminal appends an ordered case with explicit terminal metadata.
func (builder *DecisionTableBuilder[Input, Action, Reason]) CaseWithTerminal(
	match DecisionCaseMatcher[Input],
	action Action,
	reason Reason,
	terminal bool,
) *DecisionTableBuilder[Input, Action, Reason] {
	if builder == nil {
		return builder
	}

	builder.cases = append(builder.cases, decisionCase[Input, Action, Reason]{
		match:    match,
		action:   action,
		reason:   reason,
		terminal: terminal,
	})

	return builder
}

// Build returns an immutable decision table.
func (builder *DecisionTableBuilder[Input, Action, Reason]) Build() (DecisionTable[Input, Action, Reason], error) {
	if builder == nil {
		return nil, configError("decision table builder is nil")
	}
	if len(builder.cases) == 0 {
		return nil, ErrNoHandlers
	}

	cases := make([]decisionCase[Input, Action, Reason], len(builder.cases))
	copy(cases, builder.cases)
	for index, item := range cases {
		if item.match == nil {
			return nil, configError(fmt.Sprintf("decision table case at index %d has nil matcher", index))
		}
	}

	return &builtDecisionTable[Input, Action, Reason]{
		cases: cases,
	}, nil
}

// Decide evaluates cases in declaration order.
func (table *builtDecisionTable[Input, Action, Reason]) Decide(input Input) (DecisionResult[Action, Reason], error) {
	if table == nil {
		return NoDecision[Action, Reason](), configError("decision table is nil")
	}

	for _, item := range table.cases {
		matched, err := item.match(input)
		if err != nil {
			return NoDecision[Action, Reason](), err
		}
		if !matched {
			continue
		}

		return DecisionResult[Action, Reason]{
			Matched:  true,
			Action:   item.action,
			Reason:   item.reason,
			Terminal: item.terminal,
		}, nil
	}

	return NoDecision[Action, Reason](), nil
}

// NoDecision returns an explicit no-match decision result.
func NoDecision[Action comparable, Reason comparable]() DecisionResult[Action, Reason] {
	var action Action
	var reason Reason
	return DecisionResult[Action, Reason]{
		Matched:  false,
		Action:   action,
		Reason:   reason,
		Terminal: false,
	}
}

// DecisionResultHandler maps a table decision into a route result.
type DecisionResultHandler[Req any, Kind comparable, Reason comparable, Payload any, Action comparable, DecisionReason comparable] func(
	RouteCall[Req],
	DecisionResult[Action, DecisionReason],
) (RouteResult[Kind, Reason, Payload], error)

// DecisionTableHandler adapts a decision table into a route handler.
func DecisionTableHandler[
	Req any,
	Kind comparable,
	Reason comparable,
	Payload any,
	Action comparable,
	DecisionReason comparable,
](
	table DecisionTable[Req, Action, DecisionReason],
	handler DecisionResultHandler[Req, Kind, Reason, Payload, Action, DecisionReason],
) RouteHandler[Req, Kind, Reason, Payload] {
	if table == nil {
		return invalidRouteHandler[Req, Kind, Reason, Payload](configError("decision table is nil"))
	}
	if handler == nil {
		return invalidRouteHandler[Req, Kind, Reason, Payload](configError("decision result handler is nil"))
	}

	return func(call RouteCall[Req]) (RouteResult[Kind, Reason, Payload], error) {
		decision, err := table.Decide(call.Request)
		if err != nil {
			return AbortResult[Kind, Reason, Payload](), err
		}

		return handler(call, decision)
	}
}

// DecisionTableRouteBuilder registers route-table cases backed by a decision table.
type DecisionTableRouteBuilder[
	Req any,
	Kind comparable,
	Reason comparable,
	Payload any,
	Action comparable,
	DecisionReason comparable,
] struct {
	table *RouteTable[Req, Kind, Reason, Payload]
	group *decisionTableRouteGroup[Req, Action, DecisionReason]
}

type decisionTableRouteGroup[Req any, Action comparable, Reason comparable] struct {
	table DecisionTable[Req, Action, Reason]
}

type decisionTableCacheEntry[Action comparable, Reason comparable] struct {
	result DecisionResult[Action, Reason]
	err    error
}

// OnDecisionTable starts a route-table builder backed by an ordered decision table.
func OnDecisionTable[
	Req any,
	Kind comparable,
	Reason comparable,
	Payload any,
	Action comparable,
	DecisionReason comparable,
](
	table *RouteTable[Req, Kind, Reason, Payload],
	decisions DecisionTable[Req, Action, DecisionReason],
) *DecisionTableRouteBuilder[Req, Kind, Reason, Payload, Action, DecisionReason] {
	return &DecisionTableRouteBuilder[Req, Kind, Reason, Payload, Action, DecisionReason]{
		table: table,
		group: &decisionTableRouteGroup[Req, Action, DecisionReason]{
			table: decisions,
		},
	}
}

// Case registers a route selected by decision action.
func (builder *DecisionTableRouteBuilder[Req, Kind, Reason, Payload, Action, DecisionReason]) Case(
	id RouteID,
	priority int,
	action Action,
	handler RouteHandler[Req, Kind, Reason, Payload],
) *DecisionTableRouteBuilder[Req, Kind, Reason, Payload, Action, DecisionReason] {
	if builder == nil || builder.table == nil {
		return builder
	}

	builder.table.routes = append(builder.table.routes, routeEntry[Req, Kind, Reason, Payload]{
		id:       id,
		priority: priority,
		handler:  handler,
		nested:   nil,
		matcher:  decisionTableMatcher(builder.group, action),
	})

	return builder
}

func decisionTableMatcher[Req any, Action comparable, Reason comparable](
	group *decisionTableRouteGroup[Req, Action, Reason],
	expected Action,
) routeMatcher[Req] {
	return routeMatcher[Req]{
		kind:         MatchKindTable,
		staticKey:    fmt.Sprint(expected),
		prefixLength: 0,
		match: func(call RouteCall[Req]) (routeMatchData, bool, error) {
			if group == nil || group.table == nil {
				return routeMatchData{}, false, configError("decision table is nil")
			}

			result, err := decisionTableForCall(call, group)
			if err != nil {
				return routeMatchData{}, false, err
			}
			if !result.Matched || result.Action != expected {
				return routeMatchData{}, false, nil
			}

			return routeMatchData{
				key:               fmt.Sprint(result.Action),
				prefix:            "",
				remainder:         "",
				decisionReason:    result.Reason,
				hasDecisionReason: true,
			}, true, nil
		},
	}
}

func decisionTableForCall[Req any, Action comparable, Reason comparable](
	call RouteCall[Req],
	group *decisionTableRouteGroup[Req, Action, Reason],
) (DecisionResult[Action, Reason], error) {
	if call.state == nil {
		return group.table.Decide(call.Request)
	}
	if call.state.decisions == nil {
		call.state.decisions = make(map[any]any)
	}

	cached, ok := call.state.decisions[group]
	if ok {
		entry, typed := cached.(decisionTableCacheEntry[Action, Reason])
		if !typed {
			return NoDecision[Action, Reason](), configError("decision table cache type mismatch")
		}
		return entry.result, entry.err
	}

	result, err := group.table.Decide(call.Request)
	call.state.decisions[group] = decisionTableCacheEntry[Action, Reason]{
		result: result,
		err:    err,
	}

	return result, err
}
