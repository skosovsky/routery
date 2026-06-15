package routery

import (
	"context"
	"errors"
	"sync"
)

// ErrorPredicate decides whether fallback should happen for a given error.
type ErrorPredicate func(error) bool

// WeightExtractor computes request weight for WeightBasedRouter.
type WeightExtractor[Req any] func(ctx context.Context, req Req) (int, error)

// PredicateFallback executes secondary only when shouldFallback returns true for a system error.
func PredicateFallback[Req any, Res any](
	primary RouteHandler[Req, Res],
	secondary RouteHandler[Req, Res],
	shouldFallback ErrorPredicate,
) RouteHandler[Req, Res] {
	if primary == nil || secondary == nil {
		return invalidRouteHandler[Req, Res](
			configError("predicate fallback requires non-nil primary and secondary route handlers"),
		)
	}
	if shouldFallback == nil {
		return invalidRouteHandler[Req, Res](configError("predicate fallback requires a non-nil predicate"))
	}

	return func(ctx context.Context, req Req, rec ResultRecorder[Res]) error {
		localPrimary := NewResultRecorder[Res]()
		err := primary(ctx, req, localPrimary)
		if err == nil {
			CopyRecorderOutcome(rec, localPrimary)
			return nil
		}
		if !shouldFallback(err) {
			return err
		}

		localSecondary := NewResultRecorder[Res]()
		if err := secondary(ctx, req, localSecondary); err != nil {
			return err
		}
		CopyRecorderOutcome(rec, localSecondary)

		return nil
	}
}

// FirstCompleted runs route handlers in parallel and merges the first successful stop outcome.
//
// A handler wins only when it returns nil and records a payload via Stop or Async.
// Ignore and Next do not win. When multiple handlers record payload, the first
// completion order wins, not registration order.
func FirstCompleted[Req any, Res any](handlers ...RouteHandler[Req, Res]) RouteHandler[Req, Res] {
	validated, err := validateRouteHandlers(handlers, "first completed")
	if err != nil {
		return invalidRouteHandler[Req, Res](err)
	}

	return func(ctx context.Context, req Req, rec ResultRecorder[Res]) error {
		derivedCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		safeRec := newThreadSafeRecorder(rec)
		results := make(chan firstCompletedResult[Res], len(validated))
		var group sync.WaitGroup

		for index, handler := range validated {
			group.Add(1)
			go func(handlerIndex int, current RouteHandler[Req, Res]) {
				defer group.Done()

				localRec := NewResultRecorder[Res]()
				handleErr := current(derivedCtx, req, localRec)
				results <- firstCompletedResult[Res]{
					index:    handlerIndex,
					recorder: localRec,
					err:      handleErr,
				}
			}(index, handler)
		}

		go func() {
			group.Wait()
			close(results)
		}()

		allErrors := make([]error, len(validated))

		for result := range results {
			if result.err == nil {
				if _, ok := result.recorder.Payload(); ok {
					CopyRecorderOutcome(safeRec, result.recorder)
					cancel()
					return nil
				}
			}
			if result.err != nil {
				allErrors[result.index] = result.err
			}
		}

		joined := errors.Join(allErrors...)
		if joined != nil {
			return joined
		}

		return ErrNoSuccessfulOutcome
	}
}

// WeightBasedRouter routes requests using user-provided weight extraction.
func WeightBasedRouter[Req any, Res any](
	extractor WeightExtractor[Req],
	threshold int,
	lightweight RouteHandler[Req, Res],
	heavyweight RouteHandler[Req, Res],
) RouteHandler[Req, Res] {
	if extractor == nil {
		return invalidRouteHandler[Req, Res](configError("weight router requires a non-nil extractor"))
	}
	if lightweight == nil || heavyweight == nil {
		return invalidRouteHandler[Req, Res](
			configError("weight router requires non-nil lightweight and heavyweight route handlers"),
		)
	}

	return func(ctx context.Context, req Req, rec ResultRecorder[Res]) error {
		weight, err := extractor(ctx, req)
		if err != nil {
			return err
		}
		if weight < threshold {
			return lightweight(ctx, req, rec)
		}

		return heavyweight(ctx, req, rec)
	}
}

type firstCompletedResult[Res any] struct {
	index    int
	recorder ResultRecorder[Res]
	err      error
}
