package routery

import (
	"context"
	"errors"
	"sync"
)

// ErrorPredicate decides whether fallback should happen for a given error.
type ErrorPredicate func(error) bool

// WeightExtractor computes request weight for WeightBasedRouter.
type WeightExtractor[TReq any] func(ctx context.Context, req TReq) (int, error)

// PredicateFallback executes secondary only when shouldFallback returns true for a system error.
func PredicateFallback[TReq any, TRes any](
	primary Handler[TReq, TRes],
	secondary Handler[TReq, TRes],
	shouldFallback ErrorPredicate,
) Handler[TReq, TRes] {
	if primary == nil || secondary == nil {
		return invalidHandler[TReq, TRes](
			configError("predicate fallback requires non-nil primary and secondary handlers"),
		)
	}
	if shouldFallback == nil {
		return invalidHandler[TReq, TRes](configError("predicate fallback requires a non-nil predicate"))
	}

	return HandlerFunc[TReq, TRes](func(ctx context.Context, req TReq) (RouteResult[TRes], error) {
		result, err := primary.Handle(ctx, req)
		if err == nil {
			return result, nil
		}
		if !shouldFallback(err) {
			return result, err
		}

		return secondary.Handle(ctx, req)
	})
}

// FirstCompleted runs handlers in parallel and returns the first successful result.
//
// A handler is considered successful when Handle returns a nil error, regardless
// of the business route status in RouteResult.
func FirstCompleted[TReq any, TRes any](handlers ...Handler[TReq, TRes]) Handler[TReq, TRes] {
	validated, err := validateHandlers(handlers, "first completed")
	if err != nil {
		return invalidHandler[TReq, TRes](err)
	}

	return HandlerFunc[TReq, TRes](func(ctx context.Context, req TReq) (RouteResult[TRes], error) {
		derivedCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		results := make(chan firstCompletedResult[TRes], len(validated))
		var group sync.WaitGroup

		for index, handler := range validated {
			group.Add(1)
			go func(handlerIndex int, current Handler[TReq, TRes]) {
				defer group.Done()

				result, handleErr := current.Handle(derivedCtx, req)
				results <- firstCompletedResult[TRes]{
					index:  handlerIndex,
					result: result,
					err:    handleErr,
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
				cancel()
				return result.result, nil
			}
			allErrors[result.index] = result.err
		}

		return zeroRouteResult[TRes](), errors.Join(allErrors...)
	})
}

// WeightBasedRouter routes requests using user-provided weight extraction.
func WeightBasedRouter[TReq any, TRes any](
	extractor WeightExtractor[TReq],
	threshold int,
	lightweight Handler[TReq, TRes],
	heavyweight Handler[TReq, TRes],
) Handler[TReq, TRes] {
	if extractor == nil {
		return invalidHandler[TReq, TRes](configError("weight router requires a non-nil extractor"))
	}
	if lightweight == nil || heavyweight == nil {
		return invalidHandler[TReq, TRes](
			configError("weight router requires non-nil lightweight and heavyweight handlers"),
		)
	}

	return HandlerFunc[TReq, TRes](func(ctx context.Context, req TReq) (RouteResult[TRes], error) {
		weight, err := extractor(ctx, req)
		if err != nil {
			return zeroRouteResult[TRes](), err
		}
		if weight < threshold {
			return lightweight.Handle(ctx, req)
		}

		return heavyweight.Handle(ctx, req)
	})
}

type firstCompletedResult[TRes any] struct {
	index  int
	result RouteResult[TRes]
	err    error
}
