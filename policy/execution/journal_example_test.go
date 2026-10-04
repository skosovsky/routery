package execution_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/skosovsky/routery"
	"github.com/skosovsky/routery/policy/attempt"
	"github.com/skosovsky/routery/policy/execution"
	"github.com/skosovsky/routery/policy/quota"
)

func ExampleRace_lateJournalObservations() {
	// Arrange: controlled callbacks; a real host must join its own provider workers.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	loserStarted, releaseLoser, loserSettled := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseLoser) }) }
	defer release()
	boundary := execution.Boundary[string, routery.BasicKind, routery.BasicReason, io.ReadCloser]{
		Fresh: func(context.Context, string) error { return nil },
		Admit: func(_ context.Context, _ string, id attempt.Identity) (execution.Admission, error) {
			return execution.Admission{
				Status: quota.Admitted,
				Finish: func(ctx context.Context, _ attempt.Event) error {
					if id.Attempt == "loser" {
						close(loserSettled)
					}
					return ctx.Err()
				},
			}, nil
		},
		CleanupContext: func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), time.Second)
		},
		Dispatch: func(call routery.RouteCall[string], receipt *execution.Receipt) (routery.BasicRouteResult[io.ReadCloser], error) {
			if call.Request == "loser" {
				close(loserStarted)
				// Branch cancellation cannot prove remote non-execution; fixture waits for its explicit release.
				select {
				case <-releaseLoser:
				case <-ctx.Done():
					return routery.BasicRouteResult[io.ReadCloser]{}, ctx.Err()
				}
			} else {
				select {
				case <-loserStarted:
				case <-ctx.Done():
					return routery.BasicRouteResult[io.ReadCloser]{}, ctx.Err()
				}
			}
			body := io.NopCloser(strings.NewReader(call.Request))
			result := routery.BasicHandled[io.ReadCloser](body)
			result.Lifetime = routery.NewLifetime(body.Close)
			event, _, snapshotErr := receipt.Snapshot()
			if snapshotErr != nil {
				return result, snapshotErr
			}
			event.Phase = attempt.StreamOpened
			if call.Request == "loser" {
				event.Phase, event.Outcome = attempt.Terminal, attempt.Completed
			} // Explicit late provider report.
			return result, receipt.Record(event)
		},
	}
	race := execution.Race[string, routery.BasicKind, routery.BasicReason, io.ReadCloser]{
		Boundary:    boundary,
		Workers:     2,
		Permissions: execution.Permissions{Replayable: true, DuplicateCost: true, ReadOnly: true},
		Profile:     execution.EarlyOwned,
		Accept: func(context.Context, execution.Result[routery.BasicKind, routery.BasicReason, io.ReadCloser]) (bool, error) {
			return true, nil
		},
		NestedAttemptsKnown: false,
	}
	coordinator, _ := attempt.NewCoordinator("read", 2)
	// Act: winner is an owned handle, not Completed. Journal initially has one entry.
	result, err := race.Run(
		ctx,
		coordinator,
		[]execution.Step[string]{
			{Request: "winner", Identity: attempt.Identity{Operation: "read", Attempt: "winner"}},
			{Request: "loser", Identity: attempt.Identity{Operation: "read", Attempt: "loser"}},
		},
	)
	if err != nil {
		release()
		fmt.Println(errors.Join(err, result.Winner.Route.Lifetime.Close()))
		return
	}
	initial := result.Journal.Snapshot()
	winnerEvent, _, snapshotErr := result.Winner.Receipt.Snapshot()
	release()
	// Separate fixture completion signal; Snapshot itself is never a barrier.
	select {
	case <-loserSettled:
	case <-ctx.Done():
		fmt.Println(errors.Join(ctx.Err(), result.Winner.Route.Lifetime.Close()))
		return
	}
	entries, journalErr := awaitFixtureEntries(ctx, result.Journal, 2)
	closeErr := result.Winner.Route.Lifetime.Close()
	_, _, settlementErr := result.Winner.Receipt.Snapshot()
	// Assert: late loser remains observable with its explicit outcome and rejection.
	lateCompleted := fixtureLoserCompleted(entries)
	fmt.Println(
		"initial-entries",
		len(initial),
		"winner-completed",
		winnerEvent.Outcome == attempt.Completed,
		"late-entries",
		len(entries),
		"late-completed",
		lateCompleted,
	)
	fmt.Println(
		"cleanup",
		errors.Join(snapshotErr, journalErr, closeErr, settlementErr),
		"nested-accounted",
		result.NestedAttemptsKnown,
	)
	// Output:
	// initial-entries 1 winner-completed false late-entries 2 late-completed true
	// cleanup <nil> nested-accounted false
}

// Fixture knows exactly two callbacks. This is not a general provider completion barrier.
func awaitFixtureEntries[Kind comparable, Reason comparable, Payload any](
	ctx context.Context,
	journal *execution.Journal[Kind, Reason, Payload],
	count int,
) ([]execution.RaceEntry[Kind, Reason, Payload], error) {
	for {
		entries := journal.Snapshot()
		if len(entries) == count {
			return entries, nil
		}
		select {
		case <-journal.Changes():
		case <-ctx.Done():
			return entries, ctx.Err()
		}
	}
}

func fixtureLoserCompleted(entries []execution.RaceEntry[routery.BasicKind, routery.BasicReason, io.ReadCloser]) bool {
	for _, entry := range entries {
		if entry.Identity.Attempt == "loser" {
			event, _, receiptErr := entry.Result.Receipt.Snapshot()
			return !entry.Accepted && event.Outcome == attempt.Completed && receiptErr == nil
		}
	}
	return false
}
