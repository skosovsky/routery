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

// PredicateFallback executes secondary only when shouldFallback returns true.
func PredicateFallback[Req any, Res any](
	primary Executor[Req, Res],
	secondary Executor[Req, Res],
	shouldFallback ErrorPredicate,
) Executor[Req, Res] {
	if primary == nil || secondary == nil {
		return invalidExecutor[Req, Res](
			configError("predicate fallback requires non-nil primary and secondary executors"),
		)
	}
	if shouldFallback == nil {
		return invalidExecutor[Req, Res](configError("predicate fallback requires a non-nil predicate"))
	}

	return ExecutorFunc[Req, Res](func(ctx context.Context, req Req) (Res, error) {
		response, err := primary.Execute(ctx, req)
		if err == nil {
			return response, nil
		}
		if !shouldFallback(err) {
			return response, err
		}

		return secondary.Execute(ctx, req)
	})
}

// FirstCompleted runs executors in parallel and returns the first successful response.
func FirstCompleted[Req any, Res any](executors ...Executor[Req, Res]) Executor[Req, Res] {
	validated, err := validateExecutors(executors, "first completed")
	if err != nil {
		return invalidExecutor[Req, Res](err)
	}

	return ExecutorFunc[Req, Res](func(ctx context.Context, req Req) (Res, error) {
		derivedCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		results := make(chan firstCompletedResult[Res], len(validated))
		var group sync.WaitGroup

		for index, executor := range validated {
			group.Add(1)
			go func(executorIndex int, current Executor[Req, Res]) {
				defer group.Done()

				response, executeErr := current.Execute(derivedCtx, req)
				results <- firstCompletedResult[Res]{
					index:    executorIndex,
					response: response,
					err:      executeErr,
				}
			}(index, executor)
		}

		go func() {
			group.Wait()
			close(results)
		}()

		allErrors := make([]error, len(validated))

		for result := range results {
			if result.err == nil {
				cancel()
				return result.response, nil
			}
			allErrors[result.index] = result.err
		}

		return zeroValue[Res](), errors.Join(allErrors...)
	})
}

// WeightBasedRouter routes requests using user-provided weight extraction.
func WeightBasedRouter[Req any, Res any](
	extractor WeightExtractor[Req],
	threshold int,
	lightweight Executor[Req, Res],
	heavyweight Executor[Req, Res],
) Executor[Req, Res] {
	if extractor == nil {
		return invalidExecutor[Req, Res](configError("weight router requires a non-nil extractor"))
	}
	if lightweight == nil || heavyweight == nil {
		return invalidExecutor[Req, Res](
			configError("weight router requires non-nil lightweight and heavyweight executors"),
		)
	}

	return ExecutorFunc[Req, Res](func(ctx context.Context, req Req) (Res, error) {
		weight, err := extractor(ctx, req)
		if err != nil {
			return zeroValue[Res](), err
		}
		if weight < threshold {
			return lightweight.Execute(ctx, req)
		}

		return heavyweight.Execute(ctx, req)
	})
}

type firstCompletedResult[Res any] struct {
	index    int
	response Res
	err      error
}
