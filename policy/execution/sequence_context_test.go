package execution

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/skosovsky/routery"
	"github.com/skosovsky/routery/policy/attempt"
	"github.com/skosovsky/routery/policy/quota"
)

//nolint:gocognit // Keep the phase matrix and its observable lifecycle assertions together.
func TestSequenceOwnedContextRetainedUntilCloseOrDeadline(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "partial error"}[partial], func(t *testing.T) {
			// Arrange: the host clock explicitly drives cancellation, without sleeps.
			coordinator, id := setup(t)
			now := time.Unix(100, 0)
			sequence := sequenceFixture(t, &now)
			sequence.Deadline = now.Add(time.Hour)
			var contexts []context.Context
			var expire context.CancelCauseFunc
			cancels, closes, settlements := 0, 0, 0
			settlementErr := errors.New("settlement acknowledgement lost")
			sequence.DeadlineContext = func(ctx context.Context, deadline time.Time) (context.Context, context.CancelFunc) {
				child, cancel := context.WithCancelCause(ctx)
				expire = cancel
				return &drivenDeadline{
					Context:  child,
					deadline: deadline,
				}, func() { cancels++; cancel(context.Canceled) }
			}
			sequence.Boundary = testBoundary{
				Fresh: func(context.Context, string) error { return nil },
				Admit: func(context.Context, string, attempt.Identity) (Admission, error) {
					return Admission{
						Status: quota.Admitted,
						Finish: func(ctx context.Context, event attempt.Event) error {
							settlements++
							if ctx.Err() != nil || event.Outcome != attempt.Unknown {
								t.Error("invented outcome or canceled cleanup")
							}
							return settlementErr
						},
					}, nil
				},
				Dispatch: func(call routery.RouteCall[string], _ *Receipt) (routery.BasicRouteResult[string], error) {
					contexts = append(contexts, call.Context)
					result := routery.BasicHandled("live stream")
					result.Lifetime = routery.NewLifetime(func() error { closes++; return nil })
					if partial {
						return result, errors.New("buffered partial failure")
					}
					return result, nil
				},
				CleanupContext: cleanupContext,
			}
			// Unknown cannot replay, so the partial owner is also returned live.
			sequence.Replay = func(Failure[failureClass]) (attempt.Replay, error) { return attempt.Replay{}, nil }
			// Act.
			result, err := sequence.Run(t.Context(), coordinator, Step[string]{Request: "request", Identity: id})
			// Assert: return is not resource completion or cancellation.
			borrowed := contexts[0]
			if err != nil || borrowed.Err() != nil || closes != 0 || settlements != 0 || cancels != 0 {
				t.Fatalf(
					"err=%v context=%v closes=%d settlements=%d cancels=%d",
					err,
					borrowed.Err(),
					closes,
					settlements,
					cancels,
				)
			}
			now = sequence.Deadline
			expire(context.DeadlineExceeded)
			if !errors.Is(borrowed.Err(), context.DeadlineExceeded) {
				t.Fatal("deadline did not cancel live resource")
			}
			if err = result.Last.Route.Lifetime.Close(); err != nil {
				t.Fatal(err)
			}
			if err = result.Last.Route.Lifetime.Close(); err != nil {
				t.Fatal(err)
			}
			_, _, receiptErr := result.Last.Receipt.Snapshot()
			if closes != 1 || cancels != 1 || settlements != 1 || !errors.Is(receiptErr, settlementErr) {
				t.Fatalf("closes=%d cancels=%d settlements=%d receipt=%v", closes, cancels, settlements, receiptErr)
			}
		})
	}
}

type drivenDeadline struct {
	context.Context

	deadline time.Time
}

func (ctx *drivenDeadline) Deadline() (time.Time, bool) { return ctx.deadline, true }

func (ctx *drivenDeadline) Err() error {
	if errors.Is(context.Cause(ctx.Context), context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return ctx.Context.Err()
}

func TestSequenceCannotExtendContextDeadline(t *testing.T) {
	// Arrange: all timestamps use the real context clock domain.
	coordinator, id := setup(t)
	now := time.Now()
	parentDeadline := now.Add(time.Hour)
	ctx, cancel := context.WithDeadline(t.Context(), parentDeadline)
	defer cancel()
	sequence := sequenceFixture(t, &now)
	sequence.DeadlineContext = nil
	sequence.Deadline = now.Add(2 * time.Hour)
	var observed time.Time
	sequence.Boundary = testBoundary{
		Fresh: func(context.Context, string) error { return nil },
		Dispatch: func(call routery.RouteCall[string], _ *Receipt) (routery.BasicRouteResult[string], error) {
			observed, _ = call.Context.Deadline()
			return routery.BasicHandled("value"), nil
		},
	}
	// Act.
	_, err := sequence.Run(ctx, coordinator, Step[string]{Request: "request", Identity: id})
	// Assert.
	if err != nil || !observed.Equal(parentDeadline) {
		t.Fatalf("observed=%v parent=%v err=%v", observed, parentDeadline, err)
	}
}

func TestSequenceRejectsExtendingDeadlineFactory(t *testing.T) {
	// Arrange.
	coordinator, id := setup(t)
	now := time.Unix(100, 0)
	sequence := sequenceFixture(t, &now)
	sequence.Deadline = now.Add(time.Hour)
	calls, cancels := 0, 0
	sequence.DeadlineContext = func(ctx context.Context, deadline time.Time) (context.Context, context.CancelFunc) {
		child, cancel := syntheticDeadlineContext(ctx, deadline.Add(time.Hour))
		return child, func() { cancels++; cancel() }
	}
	sequence.Boundary = testBoundary{
		Fresh: func(context.Context, string) error { return nil },
		Dispatch: func(routery.RouteCall[string], *Receipt) (routery.BasicRouteResult[string], error) {
			calls++
			return routery.BasicHandled("unexpected"), nil
		},
	}
	// Act.
	_, err := sequence.Run(t.Context(), coordinator, Step[string]{Request: "request", Identity: id})
	// Assert.
	if !errors.Is(err, ErrInvalidBoundary) || calls != 0 || cancels != 1 {
		t.Fatalf("calls=%d cancels=%d err=%v", calls, cancels, err)
	}
}

func TestReceiptSettlementCallbacksRunOutsideMutex(t *testing.T) {
	// Arrange.
	coordinator, id := setup(t)
	var receipt *Receipt
	boundary := testBoundary{
		Fresh: func(context.Context, string) error { return nil },
		Admit: func(context.Context, string, attempt.Identity) (Admission, error) {
			return Admission{Status: quota.Admitted, Finish: func(context.Context, attempt.Event) error {
				if !receipt.mu.TryLock() {
					t.Fatal("Finish runs under Receipt mutex")
				}
				receipt.mu.Unlock()
				return nil
			}}, nil
		},
		Dispatch: func(_ routery.RouteCall[string], current *Receipt) (routery.BasicRouteResult[string], error) {
			receipt = current
			return routery.BasicHandled("value"), nil
		},
		CleanupContext: func() (context.Context, context.CancelFunc) {
			if !receipt.mu.TryLock() {
				t.Fatal("cleanup factory runs under Receipt mutex")
			}
			receipt.mu.Unlock()
			return cleanupContext()
		},
	}
	// Act.
	_, err := boundary.Run(routery.NewRouteCall(t.Context(), "request"), coordinator, id)
	// Assert.
	if err != nil {
		t.Fatal(err)
	}
}
