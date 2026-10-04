package routeryhttp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/skosovsky/routery"
	"github.com/skosovsky/routery/policy/attempt"
	"github.com/skosovsky/routery/policy/execution"
)

type hintFailure uint8

const unavailableHintFailure hintFailure = 1

func TestHTTPHintDefersActualDispatchBeyondDeadline(t *testing.T) {
	t.Parallel()
	// Arrange.
	now := time.Unix(100, 0)
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Header:     http.Header{"Retry-After": []string{"30"}},
			Body:       io.NopCloser(strings.NewReader("unavailable")),
			Request:    request,
		}, nil
	})}
	handler := NewRouteHandler(client)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.invalid/operation", nil)
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := attempt.NewCoordinator("operation", 2)
	if err != nil {
		t.Fatal(err)
	}
	type result = execution.Result[routery.BasicKind, routery.BasicReason, *http.Response]
	sequence := execution.Sequence[*http.Request, hintFailure, routery.BasicKind, routery.BasicReason, *http.Response]{
		Boundary: execution.Boundary[*http.Request, routery.BasicKind, routery.BasicReason, *http.Response]{
			Fresh: func(context.Context, *http.Request) error { return nil },
			Dispatch: func(call routery.RouteCall[*http.Request], receipt *execution.Receipt) (routery.BasicRouteResult[*http.Response], error) {
				response, dispatchErr := handler(call)
				event, _, snapshotErr := receipt.Snapshot()
				if snapshotErr != nil {
					return response, errors.Join(dispatchErr, snapshotErr)
				}
				// Headers/open response are not full stream completion or proof
				// that the provider did not execute or charge this request.
				event.Phase = attempt.StreamOpened
				return response, errors.Join(dispatchErr, receipt.Record(event))
			},
		},
		Classify: func(_ result, err error) (hintFailure, error) {
			var failure *StatusError
			if !errors.As(err, &failure) || failure.Code != http.StatusServiceUnavailable {
				return unavailableHintFailure, err
			}
			return unavailableHintFailure, nil
		},
		Replay: func(execution.Failure[hintFailure]) (attempt.Replay, error) {
			// This controlled fixture explicitly permits duplicate GET cost.
			return attempt.Replay{Retryable: true, Replayable: true, SafeDuplicate: true}, nil
		},
		Schedule: func(failure execution.Failure[hintFailure], _ attempt.Decision) (attempt.ScheduleInput, error) {
			var statusFailure *StatusError
			if !errors.As(failure.Err, &statusFailure) {
				return attempt.ScheduleInput{}, failure.Err
			}
			hint, hintErr := RetryAfterHint(statusFailure.Response.Header, HintClock{
				ReceivedAt: now, Source: "provider-reference", Reference: "receipt-clock",
			})
			return attempt.ScheduleInput{Backoff: time.Second, Hint: hint}, hintErr
		},
		Next: func(context.Context, execution.Step[*http.Request], attempt.Decision) (execution.Step[*http.Request], error) {
			t.Error("defer constructed another physical attempt")
			return execution.Step[*http.Request]{}, nil
		},
		Now:      func() time.Time { return now },
		Wait:     func(context.Context, time.Time) error { t.Error("defer waited"); return nil },
		Deadline: now.Add(5 * time.Second),
		DeadlineContext: func(ctx context.Context, deadline time.Time) (context.Context, context.CancelFunc) {
			child, cancel := context.WithCancel(ctx)
			return &hintDeadlineContext{Context: child, deadline: deadline}, cancel
		},
		NestedAttemptsKnown: true,
	}
	// Act.
	outcome, runErr := sequence.Run(t.Context(), coordinator, execution.Step[*http.Request]{
		Request: request, Identity: attempt.Identity{Operation: "operation", Attempt: "physical-1"},
	})
	t.Cleanup(func() { _ = outcome.Last.Route.Lifetime.Close() })
	// Assert.
	if runErr != nil || calls.Load() != 1 || outcome.Decision.Action != attempt.Defer ||
		outcome.Decision.Reason != attempt.DeadlineExhausted {
		t.Fatalf("error=%v calls=%d decision=%+v", runErr, calls.Load(), outcome.Decision)
	}
	if outcome.Failure == nil || outcome.Failure.Event.Outcome != attempt.Unknown ||
		!outcome.Decision.NotBefore.Equal(now.Add(30*time.Second)) {
		t.Fatal("HTTP hint inferred outcome or lost original not-before")
	}
}

type hintDeadlineContext struct {
	context.Context

	deadline time.Time
}

func (ctx *hintDeadlineContext) Deadline() (time.Time, bool) { return ctx.deadline, true }
