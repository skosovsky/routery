package routery

import (
	"cmp"
	"context"
	"fmt"
	"math"
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
	sub      *RouteTable[Req, Kind, Reason, Payload]
	matcher  routeMatcher[Req]
}

type routeMatcher[Req any] struct {
	group        any
	kind         MatchKind
	match        func(RouteCall[Req]) (routeMatchData, bool, error)
	staticKey    string
	prefixLength int
	configErr    error
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
		sub:      nil,
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
		nested:   nil,
		sub:      sub,
		matcher:  predicateMatcher(match),
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
func (table *RouteTable[Req, Kind, Reason, Payload]) Build() (Router[Req, Kind, Reason, Payload], error) {
	return table.build(make(map[*RouteTable[Req, Kind, Reason, Payload]]bool), nil)
}

func (table *RouteTable[Req, Kind, Reason, Payload]) build(
	ancestors map[*RouteTable[Req, Kind, Reason, Payload]]bool, path []RouteID,
) (Router[Req, Kind, Reason, Payload], error) {
	if table == nil {
		return nil, configError("route table is nil")
	}

	if ancestors[table] {
		return nil, configError(fmt.Sprintf("mount cycle at %v", path))
	}
	ancestors[table] = true
	defer delete(ancestors, table)

	built := &builtTable[Req, Kind, Reason, Payload]{
		longestPrefixWins: table.longestPrefixWins,
		routes:            nil,
		fallback:          table.fallback,
	}

	for _, entry := range table.routes {
		if err := entry.validateConfiguration(); err != nil {
			return nil, err
		}

		copied := entry
		if copied.sub != nil {
			nestedRouter, err := copied.sub.build(ancestors, appendRouteID(path, copied.id))
			if err != nil {
				return nil, err
			}

			nestedTable, tableErr := routerTable[Req, Kind, Reason, Payload](nestedRouter)
			if tableErr != nil {
				return nil, tableErr
			}

			copied.nested = nestedTable
			copied.sub = nil
		}

		built.routes = append(built.routes, copied)
	}

	if len(built.routes) == 0 && built.fallback == nil {
		return nil, ErrNoHandlers
	}

	slices.SortStableFunc(built.routes, func(left, right routeEntry[Req, Kind, Reason, Payload]) int {
		return compareRoutes(left, right, built.longestPrefixWins)
	})

	return &routerImpl[Req, Kind, Reason, Payload]{
		table:       built,
		fingerprint: fingerprintTable(built),
	}, nil
}

type builtTable[Req any, Kind comparable, Reason comparable, Payload any] struct {
	routes            []routeEntry[Req, Kind, Reason, Payload]
	fallback          RouteHandler[Req, Kind, Reason, Payload]
	longestPrefixWins bool
}

func compareRoutes[Req any, Kind comparable, Reason comparable, Payload any](
	left, right routeEntry[Req, Kind, Reason, Payload],
	longest bool,
) int {
	leftGroup, rightGroup := routeGroup(left.matcher.kind), routeGroup(right.matcher.kind)
	if longest {
		if order := cmp.Compare(leftGroup, rightGroup); order != 0 {
			return order
		}
		if order := cmp.Compare(right.matcher.prefixLength, left.matcher.prefixLength); order != 0 {
			return order
		}
	}
	if order := cmp.Compare(right.priority, left.priority); order != 0 {
		return order
	}
	if order := cmp.Compare(leftGroup, rightGroup); order != 0 {
		return order
	}
	return cmp.Compare(right.matcher.prefixLength, left.matcher.prefixLength)
}

func fingerprintTable[Req any, Kind comparable, Reason comparable, Payload any](
	table *builtTable[Req, Kind, Reason, Payload],
) string {
	return fingerprintTree(table, make(map[any]int))
}

func fingerprintTree[Req any, Kind comparable, Reason comparable, Payload any](
	table *builtTable[Req, Kind, Reason, Payload],
	groups map[any]int,
) string {
	parts := [][]byte{
		[]byte("routery-topology-v2"),
		[]byte(strconv.FormatBool(table.longestPrefixWins)),
		[]byte(strconv.FormatBool(table.fallback != nil)),
		[]byte(strconv.Itoa(len(table.routes))),
	}
	for _, entry := range table.routes {
		groupIndex := 0
		if entry.matcher.group != nil {
			groupIndex = groups[entry.matcher.group]
			if groupIndex == 0 {
				groupIndex = len(groups) + 1
				groups[entry.matcher.group] = groupIndex
			}
		}
		nested := "leaf"
		if entry.nested != nil {
			nested = fingerprintTree(entry.nested, groups)
		}
		parts = append(
			parts,
			[]byte(
				FingerprintSHA256(
					[]byte(entry.id),
					[]byte(strconv.Itoa(entry.priority)),
					[]byte(entry.matcher.kind),
					[]byte(entry.matcher.staticKey),
					[]byte(strconv.Itoa(groupIndex)), []byte(nested),
				),
			),
		)
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
		group:        nil,
		kind:         MatchKindPredicate,
		staticKey:    "",
		prefixLength: 0,
		configErr:    nil,
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
		sub:      nil,
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
		sub:      nil,
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
		sub:      nil,
		matcher:  prefixMatcher(builder.extractor, prefix),
	})

	return builder
}

// LongestPrefixWins puts the prefix group before all other routes in this table.
// Prefixes use length, priority, declaration order; other routes use priority then declaration.
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
		group:        nil,
		kind:         MatchKindExact,
		staticKey:    topologyKey(expected),
		prefixLength: 0,
		configErr:    nil,
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
		group:        nil,
		kind:         MatchKindPrefix,
		staticKey:    prefixText,
		prefixLength: len(prefixText),
		configErr:    nil,
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
		sub:      nil,
		matcher:  decisionMatcher(builder.group, key, minConfidence),
	})

	return builder
}

func decisionMatcher[Req any, Key comparable, Reason comparable](
	group *decisionRouteGroup[Req, Key, Reason],
	expected Key,
	minConfidence float64,
) routeMatcher[Req] {
	if !validConfidence(minConfidence) {
		return routeMatcher[Req]{
			group:        nil,
			kind:         MatchKindDecision,
			match:        nil,
			staticKey:    "",
			prefixLength: 0,
			configErr:    configError("decision confidence threshold must be finite and in [0,1]"),
		}
	}
	return routeMatcher[Req]{
		group: group,
		kind:  MatchKindDecision,
		staticKey: FingerprintSHA256(
			[]byte(topologyKey(expected)),
			[]byte(strconv.FormatFloat(minConfidence, 'g', -1, 64)),
		),
		prefixLength: 0,
		configErr:    nil,
		match: func(call RouteCall[Req]) (routeMatchData, bool, error) {
			if group == nil || group.decision == nil {
				return routeMatchData{}, false, configError("decision route classifier is nil")
			}

			result, err := decisionForCall(call, group)
			if err != nil {
				return routeMatchData{}, false, err
			}
			if !validConfidence(result.Confidence) {
				return routeMatchData{}, false, &InvalidConfidenceError{Confidence: result.Confidence}
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

func routeGroup(kind MatchKind) int {
	if kind == MatchKindPrefix {
		return 0
	}
	return 1
}

func validConfidence(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}

// InvalidConfidenceError reports a malformed classifier signal.
type InvalidConfidenceError struct{ Confidence float64 }

func (*InvalidConfidenceError) Error() string {
	return "routery: classifier confidence must be finite and in [0,1]"
}

func (entry routeEntry[Req, Kind, Reason, Payload]) validateConfiguration() error {
	if entry.matcher.configErr != nil {
		return entry.matcher.configErr
	}
	if entry.handler == nil && entry.sub == nil {
		return configError("route " + string(entry.id) + " has no handler or nested table")
	}
	if entry.handler != nil && entry.sub != nil {
		return configError("route " + string(entry.id) + " has both handler and nested table")
	}
	if entry.matcher.match == nil {
		return configError("route " + string(entry.id) + " has invalid matcher")
	}

	return nil
}
