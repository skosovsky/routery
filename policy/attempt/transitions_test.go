package attempt

import (
	"errors"
	"testing"
)

func TestCoordinatorRejectsMalformedOrRegressiveFactsWithoutMutation(t *testing.T) {
	for _, name := range []string{"phase", "outcome", "operation", "attempt", "regression", "uncommit", "not executed after commit", "nonterminal completed"} {
		t.Run(name, func(t *testing.T) {
			// Arrange.
			coordinator, err := NewCoordinator("operation", 2)
			if err != nil {
				t.Fatal(err)
			}
			id := Identity{Operation: "operation", Attempt: "physical"}
			if _, err = coordinator.Begin(id); err != nil {
				t.Fatal(err)
			}
			original := Event{Identity: id, Phase: StreamOpened, Committed: true, Outcome: Unknown}
			if err = coordinator.Update(original); err != nil {
				t.Fatal(err)
			}
			invalid := invalidTransition(name, original)
			// Act.
			err = coordinator.Update(invalid)
			current, remaining, snapshotErr := coordinator.Snapshot(id)
			// Assert: invalid metadata changes neither facts nor physical attempt budget.
			if !errors.Is(err, ErrInvalidEvent) || snapshotErr != nil || current != original || remaining != 1 {
				t.Fatalf("err=%v current=%+v remaining=%d", err, current, remaining)
			}
		})
	}
}

func invalidTransition(name string, event Event) Event {
	switch name {
	case "phase":
		event.Phase = Phase(255)
	case "outcome":
		event.Outcome = Outcome(255)
	case "operation":
		event.Identity.Operation = "other"
	case "attempt":
		event.Identity.Attempt = "unknown"
	case "regression":
		event.Phase = Dispatched
	case "uncommit":
		event.Committed = false
	case "not executed after commit":
		event.Phase, event.Outcome = Terminal, NotExecuted
	case "nonterminal completed":
		event.Outcome = Completed
	}
	return event
}
