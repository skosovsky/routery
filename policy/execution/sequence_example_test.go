package execution_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/skosovsky/routery"
	"github.com/skosovsky/routery/policy/attempt"
	"github.com/skosovsky/routery/policy/execution"
)

// Both examples use unrelated host types. No SDK/agent schema is required.
type documentQuery struct{ Name string }
type documentFailure uint8

const temporaryDocumentFailure documentFailure = 1

func ExampleSequence_ordinaryOperation() {
	// Arrange.
	sequence := documentSequence[string](
		func(call routery.RouteCall[documentQuery], _ *execution.Receipt) (routery.BasicRouteResult[string], error) {
			return routery.BasicHandled(call.Request.Name), nil
		},
	)
	coordinator, _ := attempt.NewCoordinator("document", 2)
	// Act.
	result, err := sequence.Run(
		context.Background(),
		coordinator,
		execution.Step[documentQuery]{
			Request:  documentQuery{Name: "inventory"},
			Identity: attempt.Identity{Operation: "document", Attempt: "initial"},
		},
	)
	// Assert.
	fmt.Println(result.Last.Route.Payload, err)
	// Output: inventory <nil>
}
func ExampleSequence_resourceResult() {
	// Arrange: stream-open is not Completed. Caller owns cancellation until Close.
	sequence := documentSequence[io.ReadCloser](
		func(call routery.RouteCall[documentQuery], receipt *execution.Receipt) (routery.BasicRouteResult[io.ReadCloser], error) {
			body := io.NopCloser(strings.NewReader(call.Request.Name))
			result := routery.BasicHandled[io.ReadCloser](body)
			result.Lifetime = routery.NewLifetime(body.Close)
			event, _, err := receipt.Snapshot()
			if err != nil {
				return result, err
			}
			event.Phase = attempt.StreamOpened
			return result, receipt.Record(event)
		},
	)
	coordinator, _ := attempt.NewCoordinator("document", 2)
	// Act.
	result, err := sequence.Run(
		context.Background(),
		coordinator,
		execution.Step[documentQuery]{
			Request:  documentQuery{Name: "contents"},
			Identity: attempt.Identity{Operation: "document", Attempt: "initial"},
		},
	)
	if err != nil {
		fmt.Println(errors.Join(err, result.Last.Route.Lifetime.Close(), receiptFailure(result.Last.Receipt)))
		return
	}
	data, readErr := io.ReadAll(result.Last.Route.Payload)
	closeErr := errors.Join(result.Last.Route.Lifetime.Close(), receiptFailure(result.Last.Receipt))
	// Assert.
	fmt.Println(string(data), readErr, closeErr)
	// Output: contents <nil> <nil>
}

func documentSequence[Payload any](
	dispatch func(routery.RouteCall[documentQuery], *execution.Receipt) (routery.BasicRouteResult[Payload], error),
) execution.Sequence[documentQuery, documentFailure, routery.BasicKind, routery.BasicReason, Payload] {
	return execution.Sequence[documentQuery, documentFailure, routery.BasicKind, routery.BasicReason, Payload]{
		Boundary: execution.Boundary[documentQuery, routery.BasicKind, routery.BasicReason, Payload]{
			Fresh:    func(context.Context, documentQuery) error { return nil },
			Dispatch: dispatch,
		},
		Classify: func(execution.Result[routery.BasicKind, routery.BasicReason, Payload], error) (documentFailure, error) {
			return temporaryDocumentFailure, nil
		},
		Replay: func(execution.Failure[documentFailure]) (attempt.Replay, error) {
			return attempt.Replay{Retryable: true, Replayable: true, SafeDuplicate: true}, nil
		}, // Host declares a read-only operation, not transport-derived safety.
		Schedule: func(execution.Failure[documentFailure], attempt.Decision) (attempt.ScheduleInput, error) {
			return attempt.ScheduleInput{Now: time.Now()}, nil
		},
		Next: func(_ context.Context, previous execution.Step[documentQuery], _ attempt.Decision) (execution.Step[documentQuery], error) {
			previous.Identity.Attempt += "/retry"
			return previous, nil
		},
		Now:                 time.Now,
		NestedAttemptsKnown: true, // Controlled fixture has no hidden retries.
	}
}
