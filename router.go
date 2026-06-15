package routery

import "context"

// Router is the immutable dispatch entry point for a built route table.
type Router[Req any, Res any] interface {
	Dispatch(ctx context.Context, req Req) (RouteOutcome[Res], error)
	Snapshot() RouteSnapshot[tableSnapshot]
}

type routerImpl[Req any, Res any] struct {
	table       *builtTable[Req, Res]
	fingerprint string
}

// Snapshot returns the current routing snapshot with fingerprint.
// Fingerprint reflects route topology (ids and priorities), not handler function identity.
func (router *routerImpl[Req, Res]) Snapshot() RouteSnapshot[tableSnapshot] {
	return RouteSnapshot[tableSnapshot]{
		Fingerprint: router.fingerprint,
		State:       router.table.snapshotState(),
	}
}

// Dispatch routes a request through the built table.
func (router *routerImpl[Req, Res]) Dispatch(ctx context.Context, req Req) (RouteOutcome[Res], error) {
	rec := NewResultRecorder[Res]()
	if err := dispatchTable(ctx, req, rec, router.table, true); err != nil {
		return abortOutcome[Res](), err
	}

	return outcomeFromRecorder(rec), nil
}

func dispatchTable[Req any, Res any](
	ctx context.Context,
	req Req,
	rec ResultRecorder[Res],
	table *builtTable[Req, Res],
	atRoot bool,
) error {
	for _, entry := range table.routes {
		if entry.match != nil && !entry.match(req) {
			continue
		}

		var err error
		switch {
		case entry.nested != nil:
			err = dispatchTable(ctx, req, rec, entry.nested, false)
		case entry.handler != nil:
			err = entry.handler(ctx, req, rec)
		default:
			return configError("route " + string(entry.id) + " has no handler")
		}

		if err != nil {
			return err
		}

		switch rec.Action() {
		case ActionNext:
			continue
		case ActionStop:
			return nil
		default:
			return configError("unexpected disposition in recorder: " + string(rec.Action()))
		}
	}

	if table.fallback != nil {
		if err := table.fallback(ctx, req, rec); err != nil {
			return err
		}
		if rec.Action() == ActionNext && atRoot {
			rec.Ignore(reasonNoMatch)
		}

		return nil
	}

	if atRoot {
		rec.Ignore(reasonNoMatch)
		return nil
	}

	rec.Next("")
	return nil
}
