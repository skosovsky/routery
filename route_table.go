package routery

import (
	"slices"
	"strconv"
)

// RouteID identifies a route entry in a table.
type RouteID string

// Matcher decides whether a route entry applies to a request.
type Matcher[Req any] func(Req) bool

type routeEntry[Req any, Res any] struct {
	id       RouteID
	priority int
	match    Matcher[Req]
	handler  RouteHandler[Req, Res]
	nested   *builtTable[Req, Res]
}

// RouteTable is a declarative, mutable route configuration builder.
type RouteTable[Req any, Res any] struct {
	routes   []routeEntry[Req, Res]
	fallback RouteHandler[Req, Res]
}

// NewRouteTable starts building a route table.
func NewRouteTable[Req any, Res any]() *RouteTable[Req, Res] {
	return &RouteTable[Req, Res]{}
}

// Route registers a leaf handler with optional matcher and priority.
func (table *RouteTable[Req, Res]) Route(
	id RouteID,
	priority int,
	match Matcher[Req],
	handler RouteHandler[Req, Res],
) *RouteTable[Req, Res] {
	table.routes = append(table.routes, routeEntry[Req, Res]{ //nolint:exhaustruct // nested is intentionally unset.
		id:       id,
		priority: priority,
		match:    match,
		handler:  handler,
	})
	return table
}

// Mount registers a nested route table as a handler.
func (table *RouteTable[Req, Res]) Mount(
	id RouteID,
	priority int,
	match Matcher[Req],
	sub *RouteTable[Req, Res],
) *RouteTable[Req, Res] {
	table.routes = append(table.routes, routeEntry[Req, Res]{ //nolint:exhaustruct // handler is intentionally unset.
		id:       id,
		priority: priority,
		match:    match,
		nested:   &builtTable[Req, Res]{source: sub}, //nolint:exhaustruct // routes and fallback are built later.
	})
	return table
}

// Fallback sets the handler invoked when no route terminates dispatch.
func (table *RouteTable[Req, Res]) Fallback(handler RouteHandler[Req, Res]) *RouteTable[Req, Res] {
	table.fallback = handler
	return table
}

// Build returns an immutable router from the configured table.
func (table *RouteTable[Req, Res]) Build() (Router[Req, Res], error) {
	if table == nil {
		return nil, configError("route table is nil")
	}

	built := &builtTable[Req, Res]{ //nolint:exhaustruct // routes are populated in the loop below.
		source:   table,
		fallback: table.fallback,
	}

	for _, entry := range table.routes {
		if entry.handler == nil && entry.nested == nil {
			return nil, configError("route " + string(entry.id) + " has no handler or nested table")
		}
		if entry.handler != nil && entry.nested != nil {
			return nil, configError("route " + string(entry.id) + " has both handler and nested table")
		}

		copied := entry
		if copied.nested != nil {
			nestedRouter, err := copied.nested.source.Build()
			if err != nil {
				return nil, err
			}

			nestedTable, tableErr := routerTable[Req, Res](nestedRouter)
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

	slices.SortStableFunc(built.routes, func(left, right routeEntry[Req, Res]) int {
		if left.priority != right.priority {
			return right.priority - left.priority
		}

		return 0
	})

	return &routerImpl[Req, Res]{
		table:       built,
		fingerprint: fingerprintTable(built),
	}, nil
}

type builtTable[Req any, Res any] struct {
	source   *RouteTable[Req, Res]
	routes   []routeEntry[Req, Res]
	fallback RouteHandler[Req, Res]
}

func fingerprintTable[Req any, Res any](table *builtTable[Req, Res]) string {
	parts := make([][]byte, 0, len(table.routes)+1)
	for _, entry := range table.routes {
		parts = append(parts, []byte(string(entry.id)+":"+strconv.Itoa(entry.priority)))
	}

	if table.fallback != nil {
		parts = append(parts, []byte("fallback"))
	}

	return FingerprintSHA256(parts...)
}

func (table *builtTable[Req, Res]) snapshotState() tableSnapshot {
	routeIDs := make([]RouteID, len(table.routes))
	for index, entry := range table.routes {
		routeIDs[index] = entry.id
	}

	return tableSnapshot{
		RouteIDs: routeIDs,
	}
}

func routerTable[Req any, Res any](router Router[Req, Res]) (*builtTable[Req, Res], error) {
	impl, ok := router.(*routerImpl[Req, Res])
	if !ok {
		return nil, configError("invalid router implementation")
	}

	return impl.table, nil
}

// tableSnapshot captures immutable routing metadata for fingerprinting.
type tableSnapshot struct {
	RouteIDs []RouteID
}
