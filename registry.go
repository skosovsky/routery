package routery

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"sync"
	"sync/atomic"
)

// PrefixPolicy controls how prefix routes are ordered inside a registry snapshot.
type PrefixPolicy string

const (
	PrefixPolicyNone              PrefixPolicy = ""
	PrefixPolicyPriority          PrefixPolicy = "priority"
	PrefixPolicyLongestPrefixWins PrefixPolicy = "longest_prefix_wins"
)

// RouteSpec describes one immutable route registration.
type RouteSpec[Req any, Kind comparable, Reason comparable, Payload any] struct {
	id           RouteID
	priority     int
	handler      RouteHandler[Req, Kind, Reason, Payload]
	matcher      routeMatcher[Req]
	prefixPolicy PrefixPolicy
}

// RouteRegistry accepts runtime route registrations and exposes immutable snapshots.
type RouteRegistry[Req any, Kind comparable, Reason comparable, Payload any] interface {
	Register(RouteSpec[Req, Kind, Reason, Payload]) error
	Unregister(RouteID) error
	Snapshot() RouteTableSnapshot[Req, Kind, Reason, Payload]
}

// RouteTableSnapshot is an immutable dispatch view produced by a mutable route registry.
type RouteTableSnapshot[Req any, Kind comparable, Reason comparable, Payload any] struct {
	Router      Router[Req, Kind, Reason, Payload]
	Fingerprint string
	RouteIDs    []RouteID
}

// MutableRouteRegistry is a generic runtime route registry with atomic immutable snapshots.
type MutableRouteRegistry[Req any, Kind comparable, Reason comparable, Payload any] struct {
	mu       sync.Mutex
	specs    map[RouteID]RouteSpec[Req, Kind, Reason, Payload]
	order    []RouteID
	snapshot atomic.Pointer[RouteTableSnapshot[Req, Kind, Reason, Payload]]
}

// NewRouteRegistry creates an empty mutable route registry.
func NewRouteRegistry[Req any, Kind comparable, Reason comparable, Payload any]() *MutableRouteRegistry[
	Req,
	Kind,
	Reason,
	Payload,
] {
	registry := &MutableRouteRegistry[Req, Kind, Reason, Payload]{
		mu:       sync.Mutex{},
		specs:    make(map[RouteID]RouteSpec[Req, Kind, Reason, Payload]),
		order:    nil,
		snapshot: atomic.Pointer[RouteTableSnapshot[Req, Kind, Reason, Payload]]{},
	}
	snapshot := emptyRouteTableSnapshot[Req, Kind, Reason, Payload]()
	registry.snapshot.Store(&snapshot)

	return registry
}

// NewRouteSpec creates a predicate route spec.
func NewRouteSpec[Req any, Kind comparable, Reason comparable, Payload any](
	id RouteID,
	priority int,
	match Matcher[Req],
	handler RouteHandler[Req, Kind, Reason, Payload],
) RouteSpec[Req, Kind, Reason, Payload] {
	return RouteSpec[Req, Kind, Reason, Payload]{
		id:           id,
		priority:     priority,
		handler:      handler,
		matcher:      predicateMatcher(match),
		prefixPolicy: PrefixPolicyNone,
	}
}

// ExactRouteSpec creates an exact key route spec.
func ExactRouteSpec[Req any, Kind comparable, Reason comparable, Payload any, Key comparable](
	id RouteID,
	priority int,
	extractor KeyExtractor[Req, Key],
	key Key,
	handler RouteHandler[Req, Kind, Reason, Payload],
) RouteSpec[Req, Kind, Reason, Payload] {
	return RouteSpec[Req, Kind, Reason, Payload]{
		id:           id,
		priority:     priority,
		handler:      handler,
		matcher:      exactMatcher(extractor, key),
		prefixPolicy: PrefixPolicyNone,
	}
}

// PrefixRouteSpec creates a prefix key route spec that preserves priority-first ordering.
func PrefixRouteSpec[Req any, Kind comparable, Reason comparable, Payload any, Key ~string](
	id RouteID,
	priority int,
	extractor KeyExtractor[Req, Key],
	prefix Key,
	handler RouteHandler[Req, Kind, Reason, Payload],
) RouteSpec[Req, Kind, Reason, Payload] {
	return prefixRouteSpec(id, priority, extractor, prefix, handler, PrefixPolicyPriority)
}

// LongestPrefixRouteSpec creates a prefix key route spec for longest-prefix-wins snapshots.
func LongestPrefixRouteSpec[Req any, Kind comparable, Reason comparable, Payload any, Key ~string](
	id RouteID,
	priority int,
	extractor KeyExtractor[Req, Key],
	prefix Key,
	handler RouteHandler[Req, Kind, Reason, Payload],
) RouteSpec[Req, Kind, Reason, Payload] {
	return prefixRouteSpec(id, priority, extractor, prefix, handler, PrefixPolicyLongestPrefixWins)
}

func prefixRouteSpec[Req any, Kind comparable, Reason comparable, Payload any, Key ~string](
	id RouteID,
	priority int,
	extractor KeyExtractor[Req, Key],
	prefix Key,
	handler RouteHandler[Req, Kind, Reason, Payload],
	policy PrefixPolicy,
) RouteSpec[Req, Kind, Reason, Payload] {
	return RouteSpec[Req, Kind, Reason, Payload]{
		id:           id,
		priority:     priority,
		handler:      handler,
		matcher:      prefixMatcher(extractor, prefix),
		prefixPolicy: policy,
	}
}

// ID returns the normalized or declared route id carried by spec.
func (spec RouteSpec[Req, Kind, Reason, Payload]) ID() RouteID {
	return spec.id
}

// Priority returns the declared route priority.
func (spec RouteSpec[Req, Kind, Reason, Payload]) Priority() int {
	return spec.priority
}

// NormalizeRouteID trims a route id and rejects empty ids.
func NormalizeRouteID(id RouteID) (RouteID, error) {
	normalized := RouteID(strings.TrimSpace(string(id)))
	if normalized == "" {
		return "", fmt.Errorf("%w: empty route id", ErrInvalidRouteID)
	}

	return normalized, nil
}

// Register adds spec and atomically publishes a rebuilt snapshot.
func (registry *MutableRouteRegistry[Req, Kind, Reason, Payload]) Register(
	spec RouteSpec[Req, Kind, Reason, Payload],
) error {
	if registry == nil {
		return configError("route registry is nil")
	}

	normalizedID, err := validateRouteSpec(spec)
	if err != nil {
		return err
	}
	spec.id = normalizedID

	registry.mu.Lock()
	defer registry.mu.Unlock()

	if _, exists := registry.specs[normalizedID]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateRoute, normalizedID)
	}

	nextSpecs := cloneRouteSpecs(registry.specs)
	nextOrder := append([]RouteID(nil), registry.order...)
	nextSpecs[normalizedID] = spec
	nextOrder = append(nextOrder, normalizedID)

	snapshot, buildErr := buildRouteTableSnapshot(nextOrder, nextSpecs)
	if buildErr != nil {
		return buildErr
	}

	registry.specs = nextSpecs
	registry.order = nextOrder
	registry.snapshot.Store(&snapshot)

	return nil
}

// Unregister removes id and atomically publishes a rebuilt snapshot.
func (registry *MutableRouteRegistry[Req, Kind, Reason, Payload]) Unregister(id RouteID) error {
	if registry == nil {
		return configError("route registry is nil")
	}

	normalizedID, err := NormalizeRouteID(id)
	if err != nil {
		return err
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()

	if _, exists := registry.specs[normalizedID]; !exists {
		return fmt.Errorf("%w: %s", ErrRouteNotFound, normalizedID)
	}

	nextSpecs := cloneRouteSpecs(registry.specs)
	delete(nextSpecs, normalizedID)
	nextOrder := make([]RouteID, 0, len(registry.order)-1)
	for _, routeID := range registry.order {
		if routeID != normalizedID {
			nextOrder = append(nextOrder, routeID)
		}
	}

	snapshot, buildErr := buildRouteTableSnapshot(nextOrder, nextSpecs)
	if buildErr != nil {
		return buildErr
	}

	registry.specs = nextSpecs
	registry.order = nextOrder
	registry.snapshot.Store(&snapshot)

	return nil
}

// Snapshot returns the latest immutable route table snapshot.
func (registry *MutableRouteRegistry[Req, Kind, Reason, Payload]) Snapshot() RouteTableSnapshot[
	Req,
	Kind,
	Reason,
	Payload,
] {
	if registry == nil {
		return emptyRouteTableSnapshot[Req, Kind, Reason, Payload]()
	}

	current := registry.snapshot.Load()
	if current == nil {
		return emptyRouteTableSnapshot[Req, Kind, Reason, Payload]()
	}

	return cloneRouteTableSnapshot(*current)
}

// Dispatch routes through the immutable snapshot router.
func (snapshot RouteTableSnapshot[Req, Kind, Reason, Payload]) Dispatch(
	ctx context.Context,
	req Req,
) (RouteResult[Kind, Reason, Payload], error) {
	if snapshot.Router == nil {
		return AbortResult[Kind, Reason, Payload](), ErrNoHandlers
	}

	return snapshot.Router.Dispatch(ctx, req)
}

// DispatchWithSink routes through the immutable snapshot router and emits events.
func (snapshot RouteTableSnapshot[Req, Kind, Reason, Payload]) DispatchWithSink(
	ctx context.Context,
	req Req,
	sink OutcomeSink[Kind, Reason, Payload],
) (RouteResult[Kind, Reason, Payload], error) {
	if snapshot.Router == nil {
		return AbortResult[Kind, Reason, Payload](), ErrNoHandlers
	}

	return snapshot.Router.DispatchWithSink(ctx, req, sink)
}

func validateRouteSpec[Req any, Kind comparable, Reason comparable, Payload any](
	spec RouteSpec[Req, Kind, Reason, Payload],
) (RouteID, error) {
	normalizedID, err := NormalizeRouteID(spec.id)
	if err != nil {
		return "", err
	}
	if spec.handler == nil {
		return "", configError("route " + string(normalizedID) + " has nil handler")
	}
	if spec.matcher.match == nil {
		return "", configError("route " + string(normalizedID) + " has invalid matcher")
	}
	switch spec.prefixPolicy {
	case PrefixPolicyNone, PrefixPolicyPriority, PrefixPolicyLongestPrefixWins:
		return normalizedID, nil
	default:
		return "", fmt.Errorf("%w: unknown prefix policy %q", ErrConflictingPrefixPolicy, spec.prefixPolicy)
	}
}

func buildRouteTableSnapshot[Req any, Kind comparable, Reason comparable, Payload any](
	order []RouteID,
	specs map[RouteID]RouteSpec[Req, Kind, Reason, Payload],
) (RouteTableSnapshot[Req, Kind, Reason, Payload], error) {
	if len(order) == 0 {
		return emptyRouteTableSnapshot[Req, Kind, Reason, Payload](), nil
	}

	table := NewRouteTable[Req, Kind, Reason, Payload]()
	prefixPolicy := PrefixPolicyNone
	for _, id := range order {
		spec := specs[id]
		if spec.prefixPolicy != PrefixPolicyNone {
			if prefixPolicy == PrefixPolicyNone {
				prefixPolicy = spec.prefixPolicy
			} else if prefixPolicy != spec.prefixPolicy {
				return emptyRouteTableSnapshot[Req, Kind, Reason, Payload](),
					fmt.Errorf("%w: %s and %s", ErrConflictingPrefixPolicy, prefixPolicy, spec.prefixPolicy)
			}
		}

		table.routes = append(table.routes, routeEntry[Req, Kind, Reason, Payload]{
			id:       spec.id,
			priority: spec.priority,
			handler:  spec.handler,
			nested:   nil,
			matcher:  spec.matcher,
		})
	}
	table.longestPrefixWins = prefixPolicy == PrefixPolicyLongestPrefixWins

	router, err := table.Build()
	if err != nil {
		return emptyRouteTableSnapshot[Req, Kind, Reason, Payload](), err
	}

	routerSnapshot := router.Snapshot()
	return RouteTableSnapshot[Req, Kind, Reason, Payload]{
		Router:      router,
		Fingerprint: routerSnapshot.Fingerprint,
		RouteIDs:    append([]RouteID(nil), routerSnapshot.State.RouteIDs...),
	}, nil
}

func cloneRouteSpecs[Req any, Kind comparable, Reason comparable, Payload any](
	specs map[RouteID]RouteSpec[Req, Kind, Reason, Payload],
) map[RouteID]RouteSpec[Req, Kind, Reason, Payload] {
	cloned := make(map[RouteID]RouteSpec[Req, Kind, Reason, Payload], len(specs))
	maps.Copy(cloned, specs)

	return cloned
}

func emptyRouteTableSnapshot[Req any, Kind comparable, Reason comparable, Payload any]() RouteTableSnapshot[
	Req,
	Kind,
	Reason,
	Payload,
] {
	return RouteTableSnapshot[Req, Kind, Reason, Payload]{
		Router:      nil,
		Fingerprint: FingerprintSHA256([]byte("empty-route-registry")),
		RouteIDs:    nil,
	}
}

func cloneRouteTableSnapshot[Req any, Kind comparable, Reason comparable, Payload any](
	snapshot RouteTableSnapshot[Req, Kind, Reason, Payload],
) RouteTableSnapshot[Req, Kind, Reason, Payload] {
	return RouteTableSnapshot[Req, Kind, Reason, Payload]{
		Router:      snapshot.Router,
		Fingerprint: snapshot.Fingerprint,
		RouteIDs:    append([]RouteID(nil), snapshot.RouteIDs...),
	}
}
