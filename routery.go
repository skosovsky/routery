package routery

import (
	"context"
	"errors"
	"fmt"
)

// RouteStatus describes how a handler finished relative to the routing pipeline.
type RouteStatus string

const (
	// StatusHandled indicates the request was processed and the pipeline should stop.
	StatusHandled RouteStatus = "handled"
	// StatusIgnored indicates the request was intentionally skipped.
	StatusIgnored RouteStatus = "ignored"
	// StatusNext delegates to the next pipeline rule.
	StatusNext RouteStatus = "next"
	// StatusAsync indicates asynchronous processing was accepted.
	StatusAsync RouteStatus = "async"
)

// RouteResult is the business outcome of a handler invocation.
//
// System failures are reported via the returned error, not via RouteStatus.
type RouteResult[TRes any] struct {
	Status     RouteStatus
	ReasonCode string
	Payload    TRes
}

// Handler processes a request and returns a route result or a system error.
type Handler[TReq any, TRes any] interface {
	Handle(ctx context.Context, req TReq) (RouteResult[TRes], error)
}

// HandlerFunc adapts a function to the Handler interface.
type HandlerFunc[TReq any, TRes any] func(ctx context.Context, req TReq) (RouteResult[TRes], error)

// Handle runs f for the provided context and request.
func (f HandlerFunc[TReq, TRes]) Handle(ctx context.Context, req TReq) (RouteResult[TRes], error) {
	return f(ctx, req)
}

// HandlerMiddleware decorates a handler.
type HandlerMiddleware[TReq any, TRes any] func(Handler[TReq, TRes]) Handler[TReq, TRes]

// RetryPredicate decides whether a failed execution should be retried.
//
// Retry middleware invokes the predicate only when Handle returns a non-nil error.
// Business statuses such as StatusNext and StatusIgnored never trigger retries.
type RetryPredicate[TReq any] func(ctx context.Context, req TReq, err error) bool

// ErrNoHandlers indicates that no handlers were provided.
var ErrNoHandlers = errors.New("routery: no handlers provided")

// ErrInvalidConfig indicates invalid router or middleware configuration.
var ErrInvalidConfig = errors.New("routery: invalid configuration")

// ErrCircuitOpen indicates that a circuit breaker is open and requests are failing fast.
var ErrCircuitOpen = errors.New("routery: circuit breaker open")

// ErrTooManyRequests indicates that a bulkhead semaphore has no capacity left.
var ErrTooManyRequests = errors.New("routery: too many concurrent requests")

// Handled returns a successful terminal route result.
func Handled[TRes any](payload TRes) RouteResult[TRes] {
	//nolint:exhaustruct // ReasonCode is empty for handled results.
	return RouteResult[TRes]{
		Status:  StatusHandled,
		Payload: payload,
	}
}

// Ignored returns a terminal route result that skips further processing.
func Ignored[TRes any](reasonCode string) RouteResult[TRes] {
	//nolint:exhaustruct // Payload is intentionally zero for ignored results.
	return RouteResult[TRes]{
		Status:     StatusIgnored,
		ReasonCode: reasonCode,
	}
}

// Next returns a route result that continues to the next pipeline rule.
func Next[TRes any](reasonCode string) RouteResult[TRes] {
	//nolint:exhaustruct // Payload is intentionally zero for next results.
	return RouteResult[TRes]{
		Status:     StatusNext,
		ReasonCode: reasonCode,
	}
}

// Async returns a terminal route result for accepted asynchronous work.
func Async[TRes any](payload TRes, reasonCode string) RouteResult[TRes] {
	return RouteResult[TRes]{
		Status:     StatusAsync,
		ReasonCode: reasonCode,
		Payload:    payload,
	}
}

func zeroRouteResult[TRes any]() RouteResult[TRes] {
	return RouteResult[TRes]{}
}

func configError(detail string) error {
	return fmt.Errorf("%w: %s", ErrInvalidConfig, detail)
}

func invalidHandler[TReq any, TRes any](err error) Handler[TReq, TRes] {
	return HandlerFunc[TReq, TRes](func(context.Context, TReq) (RouteResult[TRes], error) {
		return zeroRouteResult[TRes](), err
	})
}

func validateHandlers[TReq any, TRes any](handlers []Handler[TReq, TRes], name string) ([]Handler[TReq, TRes], error) {
	if len(handlers) == 0 {
		return nil, ErrNoHandlers
	}

	validated := make([]Handler[TReq, TRes], len(handlers))
	copy(validated, handlers)

	for index, handler := range validated {
		if handler == nil {
			return nil, configError(fmt.Sprintf("%s handler at index %d is nil", name, index))
		}
	}

	return validated, nil
}
