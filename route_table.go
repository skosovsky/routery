package routery

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// RouteID identifies a route entry in a table.
type RouteID string

// Matcher decides whether a route entry applies to a request.
type Matcher[Req any] func(Req) bool

// KeyExtractor extracts a typed route key from a request.
type KeyExtractor[Req any, Key comparable] func(Req) (Key, bool)

// RouteDecision is a generic classifier decision that can be bound to routes.
type RouteDecision[Key comparable, Reason comparable] struct {
	Key        Key
	Matched    bool
	Confidence float64
	Reason     Reason
}

// DecisionFunc computes a route decision for a request.
type DecisionFunc[Req any, Key comparable, Reason comparable] func(
	context.Context,
	Req,
) (RouteDecision[Key, Reason], error)

type routeEntry[Req any, Kind comparable, Reason comparable, Payload any] struct {
	id       RouteID
	priority int
	handler  RouteHandler[Req, Kind, Reason, Payload]
	nested   *builtTable[Req, Kind, Reason, Payload]
	matcher  routeMatcher[Req]
}

type routeMatcher[Req any] struct {
	kind         MatchKind
	match        func(RouteCall[Req]) (routeMatchData, bool, error)
	staticKey    string
	prefixLength int
}

type routeMatchData struct {
	key               string
	prefix            string
	remainder         string
	decisionReason    any
	hasDecisionReason bool
}

// RouteTable is a declarative, mutable route configuration builder.
type RouteTable[Req any, Kind comparable, Reason comparable, Payload any] struct {
	routes            []routeEntry[Req, Kind, Reason, Payload]
	fallback          RouteHandler[Req, Kind, Reason, Payload]
	longestPrefixWins bool
}

// BasicRouteTable is a convenience table for adapters that do not need custom Kind or Reason types.
type BasicRouteTable[Req any, Payload any] = RouteTable[Req, BasicKind, BasicReason, Payload]

// NewRouteTable starts building a route table.
func NewRouteTable[Req any, Kind comparable, Reason comparable, Payload any]() *RouteTable[Req, Kind, Reason, Payload] {
	return &RouteTable[Req, Kind, Reason, Payload]{
		routes:            nil,
		fallback:          nil,
		longestPrefixWins: false,
	}
}

// NewBasicRouteTable starts building a route table with BasicKind and BasicReason.
func NewBasicRouteTable[Req any, Payload any]() *BasicRouteTable[Req, Payload] {
	return NewRouteTable[Req, BasicKind, BasicReason, Payload]()
}

// Route registers a leaf handler with optional matcher and priority.
func (table *RouteTable[Req, Kind, Reason, Payload]) Route(
	id RouteID,
	priority int,
	match Matcher[Req],
	handler RouteHandler[Req, Kind, Reason, Payload],
) *RouteTable[Req, Kind, Reason, Payload] {
	table.routes = append(table.routes, routeEntry[Req, Kind, Reason, Payload]{
		id:       id,
		priority: priority,
		handler:  handler,
		nested:   nil,
		matcher:  predicateMatcher(match),
	})
	return table
}

// Mount registers a nested route table as a handler.
func (table *RouteTable[Req, Kind, Reason, Payload]) Mount(
	id RouteID,
	priority int,
	match Matcher[Req],
	sub *RouteTable[Req, Kind, Reason, Payload],
) *RouteTable[Req, Kind, Reason, Payload] {
	table.routes = append(table.routes, routeEntry[Req, Kind, Reason, Payload]{
		id:       id,
		priority: priority,
		handler:  nil,
		nested: &builtTable[Req, Kind, Reason, Payload]{
			source:   sub,
			routes:   nil,
			fallback: nil,
		},
		matcher: predicateMatcher(match),
	})
	return table
}

// Fallback sets the handler invoked when no route terminates dispatch.
func (table *RouteTable[Req, Kind, Reason, Payload]) Fallback(
	handler RouteHandler[Req, Kind, Reason, Payload],
) *RouteTable[Req, Kind, Reason, Payload] {
	table.fallback = handler
	return table
}

// Build returns an immutable router from the configured table.
//
//nolint:gocognit // Build validates, compiles nested tables, and sorts routes as one atomic step.
func (table *RouteTable[Req, Kind, Reason, Payload]) Build() (Router[Req, Kind, Reason, Payload], error) {
	if table == nil {
		return nil, configError("route table is nil")
	}

	built := &builtTable[Req, Kind, Reason, Payload]{
		source:   table,
		routes:   nil,
		fallback: table.fallback,
	}

	for _, entry := range table.routes {
		if entry.handler == nil && entry.nested == nil {
			return nil, configError("route " + string(entry.id) + " has no handler or nested table")
		}
		if entry.handler != nil && entry.nested != nil {
			return nil, configError("route " + string(entry.id) + " has both handler and nested table")
		}
		if entry.matcher.match == nil {
			return nil, configError("route " + string(entry.id) + " has invalid matcher")
		}

		copied := entry
		if copied.nested != nil {
			nestedRouter, err := copied.nested.source.Build()
			if err != nil {
				return nil, err
			}

			nestedTable, tableErr := routerTable[Req, Kind, Reason, Payload](nestedRouter)
			if tableErr != nil {
				return nil, tableErr
			}

			copied.nested = nestedTable
		}

		built.routes = append(built.routes, copied)
	}

	if len(built.routes) == 0 && built.fallback == nil {
		return nil, ErrNoHandlers
	}

	slices.SortStableFunc(built.routes, func(left, right routeEntry[Req, Kind, Reason, Payload]) int {
		if table.longestPrefixWins && bothPrefixRoutes(left, right) &&
			left.matcher.prefixLength != right.matcher.prefixLength {
			return right.matcher.prefixLength - left.matcher.prefixLength
		}
		if left.priority != right.priority {
			return right.priority - left.priority
		}
		if bothPrefixRoutes(left, right) && left.matcher.prefixLength != right.matcher.prefixLength {
			return right.matcher.prefixLength - left.matcher.prefixLength
		}

		return 0
	})

	return &routerImpl[Req, Kind, Reason, Payload]{
		table:       built,
		fingerprint: fingerprintTable(built),
	}, nil
}

type builtTable[Req any, Kind comparable, Reason comparable, Payload any] struct {
	source   *RouteTable[Req, Kind, Reason, Payload]
	routes   []routeEntry[Req, Kind, Reason, Payload]
	fallback RouteHandler[Req, Kind, Reason, Payload]
}

func bothPrefixRoutes[Req any, Kind comparable, Reason comparable, Payload any](
	left routeEntry[Req, Kind, Reason, Payload],
	right routeEntry[Req, Kind, Reason, Payload],
) bool {
	return left.matcher.kind == MatchKindPrefix && right.matcher.kind == MatchKindPrefix
}

func fingerprintTable[Req any, Kind comparable, Reason comparable, Payload any](
	table *builtTable[Req, Kind, Reason, Payload],
) string {
	parts := make([][]byte, 0, len(table.routes)+2)
	if table.source.longestPrefixWins {
		parts = append(parts, []byte("longest-prefix-wins"))
	}

	for _, entry := range table.routes {
		part := string(entry.id) + ":" + strconv.Itoa(entry.priority) + ":" +
			string(entry.matcher.kind) + ":" + entry.matcher.staticKey
		parts = append(parts, []byte(part))
	}

	if table.fallback != nil {
		parts = append(parts, []byte("fallback"))
	}

	return FingerprintSHA256(parts...)
}

func (table *builtTable[Req, Kind, Reason, Payload]) snapshotState() tableSnapshot {
	routeIDs := make([]RouteID, len(table.routes))
	for index, entry := range table.routes {
		routeIDs[index] = entry.id
	}

	return tableSnapshot{
		RouteIDs: routeIDs,
	}
}

func routerTable[Req any, Kind comparable, Reason comparable, Payload any](
	router Router[Req, Kind, Reason, Payload],
) (*builtTable[Req, Kind, Reason, Payload], error) {
	impl, ok := router.(*routerImpl[Req, Kind, Reason, Payload])
	if !ok {
		return nil, configError("invalid router implementation")
	}

	return impl.table, nil
}

// tableSnapshot captures immutable routing metadata for fingerprinting.
type tableSnapshot struct {
	RouteIDs []RouteID
}

func (entry routeEntry[Req, Kind, Reason, Payload]) matchRoute(
	call RouteCall[Req],
	parentPath []RouteID,
	depth int,
) (RouteMatch, bool, error) {
	data, ok, err := entry.matcher.match(call)
	match := RouteMatch{
		RouteID:           entry.id,
		Path:              appendRouteID(parentPath, entry.id),
		Priority:          entry.priority,
		Depth:             depth,
		Kind:              entry.matcher.kind,
		Key:               data.key,
		Prefix:            data.prefix,
		Remainder:         data.remainder,
		DecisionReason:    data.decisionReason,
		HasDecisionReason: data.hasDecisionReason,
	}

	return match, ok, err
}

func predicateMatcher[Req any](match Matcher[Req]) routeMatcher[Req] {
	return routeMatcher[Req]{
		kind:         MatchKindPredicate,
		staticKey:    "",
		prefixLength: 0,
		match: func(call RouteCall[Req]) (routeMatchData, bool, error) {
			if match == nil {
				return routeMatchData{}, true, nil
			}

			return routeMatchData{}, match(call.Request), nil
		},
	}
}

// KeyRouteBuilder registers exact-match routes for a typed key.
type KeyRouteBuilder[Req any, Kind comparable, Reason comparable, Payload any, Key comparable] struct {
	table     *RouteTable[Req, Kind, Reason, Payload]
	extractor KeyExtractor[Req, Key]
}

// OnKey starts a typed exact-match route builder.
func OnKey[Req any, Kind comparable, Reason comparable, Payload any, Key comparable](
	table *RouteTable[Req, Kind, Reason, Payload],
	extractor KeyExtractor[Req, Key],
) *KeyRouteBuilder[Req, Kind, Reason, Payload, Key] {
	return &KeyRouteBuilder[Req, Kind, Reason, Payload, Key]{
		table:     table,
		extractor: extractor,
	}
}

// Exact registers an exact-match route for key.
func (builder *KeyRouteBuilder[Req, Kind, Reason, Payload, Key]) Exact(
	id RouteID,
	priority int,
	key Key,
	handler RouteHandler[Req, Kind, Reason, Payload],
) *KeyRouteBuilder[Req, Kind, Reason, Payload, Key] {
	if builder == nil || builder.table == nil {
		return builder
	}

	builder.table.routes = append(builder.table.routes, routeEntry[Req, Kind, Reason, Payload]{
		id:       id,
		priority: priority,
		handler:  handler,
		nested:   nil,
		matcher:  exactMatcher(builder.extractor, key),
	})

	return builder
}

// StringKeyRouteBuilder registers exact and prefix routes for string-like keys.
type StringKeyRouteBuilder[Req any, Kind comparable, Reason comparable, Payload any, Key ~string] struct {
	table     *RouteTable[Req, Kind, Reason, Payload]
	extractor KeyExtractor[Req, Key]
}

// OnStringKey starts a typed string-key route builder.
func OnStringKey[Req any, Kind comparable, Reason comparable, Payload any, Key ~string](
	table *RouteTable[Req, Kind, Reason, Payload],
	extractor KeyExtractor[Req, Key],
) *StringKeyRouteBuilder[Req, Kind, Reason, Payload, Key] {
	return &StringKeyRouteBuilder[Req, Kind, Reason, Payload, Key]{
		table:     table,
		extractor: extractor,
	}
}

// Exact registers an exact-match route for a string-like key.
func (builder *StringKeyRouteBuilder[Req, Kind, Reason, Payload, Key]) Exact(
	id RouteID,
	priority int,
	key Key,
	handler RouteHandler[Req, Kind, Reason, Payload],
) *StringKeyRouteBuilder[Req, Kind, Reason, Payload, Key] {
	if builder == nil || builder.table == nil {
		return builder
	}

	builder.table.routes = append(builder.table.routes, routeEntry[Req, Kind, Reason, Payload]{
		id:       id,
		priority: priority,
		handler:  handler,
		nested:   nil,
		matcher:  exactMatcher(builder.extractor, key),
	})

	return builder
}

// Prefix registers a prefix-match route.
func (builder *StringKeyRouteBuilder[Req, Kind, Reason, Payload, Key]) Prefix(
	id RouteID,
	priority int,
	prefix Key,
	handler RouteHandler[Req, Kind, Reason, Payload],
) *StringKeyRouteBuilder[Req, Kind, Reason, Payload, Key] {
	if builder == nil || builder.table == nil {
		return builder
	}

	builder.table.routes = append(builder.table.routes, routeEntry[Req, Kind, Reason, Payload]{
		id:       id,
		priority: priority,
		handler:  handler,
		nested:   nil,
		matcher:  prefixMatcher(builder.extractor, prefix),
	})

	return builder
}

// LongestPrefixWins makes prefix length outrank priority when comparing prefix routes.
func (builder *StringKeyRouteBuilder[Req, Kind, Reason, Payload, Key]) LongestPrefixWins() *StringKeyRouteBuilder[
	Req,
	Kind,
	Reason,
	Payload,
	Key,
] {
	if builder == nil || builder.table == nil {
		return builder
	}
	builder.table.longestPrefixWins = true

	return builder
}

func exactMatcher[Req any, Key comparable](extractor KeyExtractor[Req, Key], expected Key) routeMatcher[Req] {
	return routeMatcher[Req]{
		kind:         MatchKindExact,
		staticKey:    fmt.Sprint(expected),
		prefixLength: 0,
		match: func(call RouteCall[Req]) (routeMatchData, bool, error) {
			if extractor == nil {
				return routeMatchData{}, false, configError("exact route key extractor is nil")
			}

			got, ok := extractor(call.Request)
			if !ok || got != expected {
				return routeMatchData{}, false, nil
			}

			return routeMatchData{
				key:               fmt.Sprint(got),
				prefix:            "",
				remainder:         "",
				decisionReason:    nil,
				hasDecisionReason: false,
			}, true, nil
		},
	}
}

func prefixMatcher[Req any, Key ~string](extractor KeyExtractor[Req, Key], prefix Key) routeMatcher[Req] {
	prefixText := prefixString(prefix)
	return routeMatcher[Req]{
		kind:         MatchKindPrefix,
		staticKey:    prefixText,
		prefixLength: len(prefixText),
		match: func(call RouteCall[Req]) (routeMatchData, bool, error) {
			if extractor == nil {
				return routeMatchData{}, false, configError("prefix route key extractor is nil")
			}

			got, ok := extractor(call.Request)
			if !ok {
				return routeMatchData{}, false, nil
			}

			key := string(got)
			if !strings.HasPrefix(key, prefixText) {
				return routeMatchData{}, false, nil
			}

			return routeMatchData{
				key:               key,
				prefix:            prefixText,
				remainder:         strings.TrimPrefix(key, prefixText),
				decisionReason:    nil,
				hasDecisionReason: false,
			}, true, nil
		},
	}
}

func prefixString[Key ~string](key Key) string {
	return string(key)
}

// DecisionRouteBuilder registers routes backed by a typed decision function.
type DecisionRouteBuilder[Req any, Kind comparable, Reason comparable, Payload any, Key comparable] struct {
	table *RouteTable[Req, Kind, Reason, Payload]
	group *decisionRouteGroup[Req, Key, Reason]
}

type decisionRouteGroup[Req any, Key comparable, Reason comparable] struct {
	decision DecisionFunc[Req, Key, Reason]
}

type decisionCacheEntry[Key comparable, Reason comparable] struct {
	result RouteDecision[Key, Reason]
	err    error
}

// OnDecision starts a typed decision-route builder.
func OnDecision[Req any, Kind comparable, Reason comparable, Payload any, Key comparable](
	table *RouteTable[Req, Kind, Reason, Payload],
	decision DecisionFunc[Req, Key, Reason],
) *DecisionRouteBuilder[Req, Kind, Reason, Payload, Key] {
	return &DecisionRouteBuilder[Req, Kind, Reason, Payload, Key]{
		table: table,
		group: &decisionRouteGroup[Req, Key, Reason]{
			decision: decision,
		},
	}
}

// Case registers a route selected by decision key and minimum confidence.
func (builder *DecisionRouteBuilder[Req, Kind, Reason, Payload, Key]) Case(
	id RouteID,
	priority int,
	key Key,
	minConfidence float64,
	handler RouteHandler[Req, Kind, Reason, Payload],
) *DecisionRouteBuilder[Req, Kind, Reason, Payload, Key] {
	if builder == nil || builder.table == nil {
		return builder
	}

	builder.table.routes = append(builder.table.routes, routeEntry[Req, Kind, Reason, Payload]{
		id:       id,
		priority: priority,
		handler:  handler,
		nested:   nil,
		matcher:  decisionMatcher(builder.group, key, minConfidence),
	})

	return builder
}

func decisionMatcher[Req any, Key comparable, Reason comparable](
	group *decisionRouteGroup[Req, Key, Reason],
	expected Key,
	minConfidence float64,
) routeMatcher[Req] {
	return routeMatcher[Req]{
		kind:         MatchKindDecision,
		staticKey:    fmt.Sprint(expected),
		prefixLength: 0,
		match: func(call RouteCall[Req]) (routeMatchData, bool, error) {
			if group == nil || group.decision == nil {
				return routeMatchData{}, false, configError("decision route classifier is nil")
			}

			result, err := decisionForCall(call, group)
			if err != nil {
				return routeMatchData{}, false, err
			}
			if !result.Matched || result.Key != expected || result.Confidence < minConfidence {
				return routeMatchData{}, false, nil
			}

			return routeMatchData{
				key:               fmt.Sprint(result.Key),
				prefix:            "",
				remainder:         "",
				decisionReason:    result.Reason,
				hasDecisionReason: true,
			}, true, nil
		},
	}
}

func decisionForCall[Req any, Key comparable, Reason comparable](
	call RouteCall[Req],
	group *decisionRouteGroup[Req, Key, Reason],
) (RouteDecision[Key, Reason], error) {
	if call.state == nil {
		return group.decision(call.Context, call.Request)
	}
	if call.state.decisions == nil {
		call.state.decisions = make(map[any]any)
	}

	cached, ok := call.state.decisions[group]
	if ok {
		entry, typed := cached.(decisionCacheEntry[Key, Reason])
		if !typed {
			var zero RouteDecision[Key, Reason]
			return zero, configError("decision route cache type mismatch")
		}
		return entry.result, entry.err
	}

	result, err := group.decision(call.Context, call.Request)
	call.state.decisions[group] = decisionCacheEntry[Key, Reason]{
		result: result,
		err:    err,
	}

	return result, err
}
