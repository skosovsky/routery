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
func PredicateFallback[Req any, Kind comparable, Reason comparable, Payload any](
	primary RouteHandler[Req, Kind, Reason, Payload],
	secondary RouteHandler[Req, Kind, Reason, Payload],
	shouldFallback ErrorPredicate,
) RouteHandler[Req, Kind, Reason, Payload] {
	if primary == nil || secondary == nil {
		return invalidRouteHandler[Req, Kind, Reason, Payload](
			configError("predicate fallback requires non-nil primary and secondary route handlers"),
		)
	}
	if shouldFallback == nil {
		return invalidRouteHandler[Req, Kind, Reason, Payload](
			configError("predicate fallback requires a non-nil predicate"),
		)
	}

	return func(call RouteCall[Req]) (RouteResult[Kind, Reason, Payload], error) {
		result, err := primary(call)
		if err == nil {
			return result, nil
		}
		if !shouldFallback(err) {
			return AbortResult[Kind, Reason, Payload]().WithMatch(result.Match), err
		}

		return secondary(call)
	}
}

// FirstCompleted runs route handlers in parallel and returns the first successful payload result.
//
// A handler wins only when it returns nil and returns a payload. Terminal results without
// payload and ActionNext do not win. Completion order wins, not registration order.
func FirstCompleted[Req any, Kind comparable, Reason comparable, Payload any](
	handlers ...RouteHandler[Req, Kind, Reason, Payload],
) RouteHandler[Req, Kind, Reason, Payload] {
	validated, err := validateRouteHandlers(handlers, "first completed")
	if err != nil {
		return invalidRouteHandler[Req, Kind, Reason, Payload](err)
	}

	return func(call RouteCall[Req]) (RouteResult[Kind, Reason, Payload], error) {
		derivedCtx, cancel := context.WithCancel(call.Context)
		defer cancel()

		results := make(chan firstCompletedResult[Kind, Reason, Payload], len(validated))
		var group sync.WaitGroup

		for index, handler := range validated {
			group.Add(1)
			go func(handlerIndex int, current RouteHandler[Req, Kind, Reason, Payload]) {
				defer group.Done()

				result, handleErr := current(call.withContext(derivedCtx))
				results <- firstCompletedResult[Kind, Reason, Payload]{
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

		return collectFirstCompletedResult(results, len(validated), cancel)
	}
}

func collectFirstCompletedResult[Kind comparable, Reason comparable, Payload any](
	results <-chan firstCompletedResult[Kind, Reason, Payload],
	handlerCount int,
	cancel context.CancelFunc,
) (RouteResult[Kind, Reason, Payload], error) {
	allErrors := make([]error, handlerCount)
	var last RouteResult[Kind, Reason, Payload]

	for result := range results {
		if result.err != nil {
			allErrors[result.index] = result.err
			continue
		}

		validated, err := validateReturnedResult(result.result)
		if err != nil {
			return validated, err
		}
		last = validated
		if last.Action != ActionNext && last.HasPayload {
			cancel()
			return last, nil
		}
	}

	joined := errors.Join(allErrors...)
	if joined != nil {
		return AbortResult[Kind, Reason, Payload]().WithMatch(last.Match), joined
	}

	return AbortResult[Kind, Reason, Payload]().WithMatch(last.Match), ErrNoSuccessfulOutcome
}

// WeightBasedRouter routes requests using user-provided weight extraction.
func WeightBasedRouter[Req any, Kind comparable, Reason comparable, Payload any](
	extractor WeightExtractor[Req],
	threshold int,
	lightweight RouteHandler[Req, Kind, Reason, Payload],
	heavyweight RouteHandler[Req, Kind, Reason, Payload],
) RouteHandler[Req, Kind, Reason, Payload] {
	if extractor == nil {
		return invalidRouteHandler[Req, Kind, Reason, Payload](
			configError("weight router requires a non-nil extractor"),
		)
	}
	if lightweight == nil || heavyweight == nil {
		return invalidRouteHandler[Req, Kind, Reason, Payload](
			configError("weight router requires non-nil lightweight and heavyweight route handlers"),
		)
	}

	return func(call RouteCall[Req]) (RouteResult[Kind, Reason, Payload], error) {
		weight, err := extractor(call.Context, call.Request)
		if err != nil {
			return AbortResult[Kind, Reason, Payload](), err
		}
		if weight < threshold {
			return lightweight(call)
		}

		return heavyweight(call)
	}
}

type firstCompletedResult[Kind comparable, Reason comparable, Payload any] struct {
	index  int
	result RouteResult[Kind, Reason, Payload]
	err    error
}
