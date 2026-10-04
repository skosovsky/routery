package attempt

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/skosovsky/routery"
)

func TestReplayPredicateEvidence(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name             string
		outcome          Outcome
		safe, replayable bool
		eventErr         error
		want             bool
	}{
		{"read-only", Unknown, true, true, nil, true},
		{"not-executed", NotExecuted, false, true, nil, true},
		{"unknown", Unknown, false, true, nil, false},
		{"unprepared", NotExecuted, false, false, nil, false},
		{"evidence-error", Unknown, true, true, io.EOF, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange.
			evidence := func(context.Context, int, error) (Event, Replay, error) {
				return Event{
					Identity: Identity{Operation: "read", Attempt: "one"},
					Phase:    Terminal,
					Outcome:  test.outcome,
				}, Replay{
					Retryable:     true,
					Replayable:    test.replayable,
					SafeDuplicate: test.safe,
				}, test.eventErr
			}
			predicate := RetryPredicate(func(error) bool { return true }, evidence)
			// Act.
			allowed := predicate(context.Background(), 1, io.EOF)
			// Assert.
			if allowed != test.want {
				t.Fatalf("allowed=%v", allowed)
			}
			for _, denied := range []error{nil, context.Canceled, context.DeadlineExceeded, errors.Join(io.EOF, routery.ErrInvalidConfig)} {
				if predicate(context.Background(), 1, denied) {
					t.Fatalf("authorized %v", denied)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if predicate(ctx, 1, io.EOF) {
				t.Fatal("cancelled replay")
			}
		})
	}
}

func TestReplayPredicateDrivesRepeatFromHostFacts(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		outcome  Outcome
		safe     bool
		provider error
		want     int
	}{
		{"read-only", Unknown, true, io.EOF, 2}, {"not-executed", NotExecuted, false, io.EOF, 2},
		{"unknown-effect", Unknown, false, io.EOF, 1}, {"invalid-config", Unknown, true, errors.Join(io.EOF, routery.ErrInvalidConfig), 1},
		{"cancelled-error", Unknown, true, context.Canceled, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange.
			calls := 0
			base := routery.BasicRouteHandler[int, string](
				func(routery.RouteCall[int]) (routery.BasicRouteResult[string], error) {
					calls++
					if calls == 1 {
						return routery.BasicRouteResult[string]{}, test.provider
					}
					return routery.BasicHandled("result"), nil
				},
			)
			evidence := func(context.Context, int, error) (Event, Replay, error) {
				return Event{
					Identity: Identity{Operation: "host", Attempt: "failed"},
					Phase:    Terminal,
					Outcome:  test.outcome,
				}, Replay{
					Retryable:     true,
					Replayable:    true,
					SafeDuplicate: test.safe,
				}, nil
			}
			predicate := RetryPredicate(func(error) bool { return true }, evidence)
			handler := routery.ApplyRoute(
				base,
				routery.RetryIf[int, routery.BasicKind, routery.BasicReason, string](3, 0, predicate),
			)
			// Act.
			result, err := routery.InvokeRouteHandler(context.Background(), 0, handler)
			// Assert.
			if calls != test.want {
				t.Fatalf("calls=%d want=%d", calls, test.want)
			}
			if test.want == 2 && (err != nil || result.Payload != "result") {
				t.Fatal(result, err)
			}
			if test.want == 1 && !errors.Is(err, test.provider) {
				t.Fatalf("provider error lost: %v", err)
			}
		})
	}
}
