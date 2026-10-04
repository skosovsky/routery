package attempt

import (
	"errors"
	"sync"
	"testing"
)

//nolint:gocognit // Keep the phase matrix and its observable lifecycle assertions together.
func TestRepeatAuthorizationOrdersLateEvents(t *testing.T) {
	for _, before := range []bool{true, false} {
		t.Run(
			map[bool]string{true: "event before authorization", false: "event after authorization"}[before],
			func(t *testing.T) {
				// Arrange.
				coordinator, err := NewCoordinator("op", 2)
				if err != nil {
					t.Fatal(err)
				}
				first := Identity{Operation: "op", Attempt: "first"}
				next := Identity{Operation: "op", Attempt: "next"}
				for _, id := range []Identity{first, next} {
					if _, beginErr := coordinator.Begin(id); beginErr != nil {
						t.Fatal(beginErr)
					}
				}
				expected := Event{Identity: first, Phase: Terminal, Outcome: Unknown}
				if err = coordinator.Update(expected); err != nil {
					t.Fatal(err)
				}
				updated := expected
				updated.Committed = true
				gate, done := make(chan struct{}), make(chan error, 1)
				go func() { <-gate; done <- coordinator.Update(updated) }()
				if before {
					close(gate)
					if err = <-done; err != nil {
						t.Fatal(err)
					}
				}
				// Act.
				decision, authErr := coordinator.AuthorizeRepeat(expected, next, Replay{
					Retryable: true, Replayable: true, SafeDuplicate: true,
				}, nil)
				if !before {
					close(gate)
					if err = <-done; err != nil {
						t.Fatal(err)
					}
				}
				actual, remaining, snapshotErr := coordinator.Snapshot(next)
				// Assert.
				if snapshotErr != nil || remaining != 0 {
					t.Fatalf("snapshot=%+v remaining=%d err=%v", actual, remaining, snapshotErr)
				}
				if before {
					if !errors.Is(authErr, ErrEventChanged) || actual.Phase != BeforeDispatch {
						t.Fatalf("early event ignored: decision=%+v event=%+v err=%v", decision, actual, authErr)
					}
					denied, deniedErr := coordinator.AuthorizeRepeat(updated, next, Replay{
						Retryable: true, Replayable: true, SafeDuplicate: true,
					}, nil)
					if deniedErr != nil || denied.Reason != VisibleOutput || denied.Action != Stop {
						t.Fatalf("fresh denial=%+v err=%v", denied, deniedErr)
					}
				} else if authErr != nil || decision.Action != Retry || actual.Phase != Dispatched {
					t.Fatalf("late event revoked dispatch: decision=%+v event=%+v err=%v", decision, actual, authErr)
				}
			},
		)
	}
}

func TestConcurrentRepeatAllocationsRespectBudgetAndIdentity(t *testing.T) {
	// Arrange.
	coordinator, err := NewCoordinator("op", 2)
	if err != nil {
		t.Fatal(err)
	}
	first := Identity{Operation: "op", Attempt: "first"}
	if _, err = coordinator.Begin(first); err != nil {
		t.Fatal(err)
	}
	expected := Event{Identity: first, Phase: Terminal, Outcome: NotExecuted}
	if err = coordinator.Update(expected); err != nil {
		t.Fatal(err)
	}
	started := make(chan Identity, 2)
	var group sync.WaitGroup
	// Act.
	for _, name := range []string{"second", "third"} {
		group.Go(func() {
			id := Identity{Operation: "op", Attempt: name}
			allocated, beginErr := coordinator.Begin(id)
			if beginErr != nil {
				t.Error(beginErr)
			}
			if allocated.Started {
				decision, authErr := coordinator.AuthorizeRepeat(
					expected,
					id,
					Replay{Retryable: true, Replayable: true},
					nil,
				)
				if authErr != nil || decision.Action != Retry {
					t.Errorf("decision=%+v err=%v", decision, authErr)
				}
				started <- id
			}
		})
	}
	group.Wait()
	close(started)
	// Assert.
	if len(started) != 1 {
		t.Fatalf("authorized %d next attempts with one remaining slot", len(started))
	}
	id := <-started
	if _, err = coordinator.Begin(id); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("recycled identity: %v", err)
	}
	if _, err = coordinator.AuthorizeRepeat(
		expected,
		id,
		Replay{Retryable: true, Replayable: true},
		nil,
	); !errors.Is(
		err,
		ErrInvalidEvent,
	) {
		t.Fatalf("authorized same identity twice: %v", err)
	}
}
