package attempt

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestCoordinatorIdentityPhasesAndReconciliation(t *testing.T) {
	// Arrange.
	coordinator, err := NewCoordinator("operation", 2)
	if err != nil {
		t.Fatal(err)
	}
	id := Identity{Operation: "operation", Attempt: "first"}
	// Act.
	start, err := coordinator.Begin(id)
	committed := Event{Identity: id, Phase: StreamOpened, Committed: true, Outcome: Unknown}
	if err != nil || !start.Started {
		t.Fatal("start failed")
	}
	if err = coordinator.Update(committed); err != nil {
		t.Fatal(err)
	}
	terminal := committed
	terminal.Phase = Terminal
	if err = coordinator.Update(terminal); err != nil {
		t.Fatal(err)
	}
	terminal.Outcome = Completed
	var group sync.WaitGroup
	for range 10 {
		group.Go(func() {
			if updateErr := coordinator.Update(terminal); updateErr != nil {
				t.Error(updateErr)
			}
		})
	}
	group.Wait()
	snapshot, remaining, err := coordinator.Snapshot(id)
	_, duplicateErr := coordinator.Begin(id)
	regressionErr := coordinator.Update(committed)
	second, err2 := coordinator.Begin(Identity{Operation: "operation", Attempt: "second"})
	exhausted, err3 := coordinator.Begin(Identity{Operation: "operation", Attempt: "third"})
	// Assert.
	if err != nil || err2 != nil || err3 != nil || snapshot != terminal || remaining != 1 {
		t.Fatal("lost state")
	}
	if !errors.Is(duplicateErr, ErrInvalidEvent) || !errors.Is(regressionErr, ErrInvalidEvent) || !second.Started ||
		exhausted.Started {
		t.Fatal("identity/budget invariant")
	}
}

func TestReplayDecisionMatrix(t *testing.T) {
	for _, test := range []struct {
		name      string
		outcome   Outcome
		committed bool
		replay    Replay
		action    Action
	}{
		{name: "before dispatch", outcome: NotExecuted, replay: Replay{Retryable: true, Replayable: true}, action: Retry},
		{name: "visible partial", outcome: Unknown, committed: true, replay: Replay{Retryable: true, Replayable: true, SafeDuplicate: true}, action: Stop},
		{name: "buffered unknown", outcome: Unknown, replay: Replay{Retryable: true, Replayable: true}, action: Stop},
		{name: "unknown reconcile", outcome: Unknown, replay: Replay{CanReconcile: true}, action: Reconcile},
		{name: "safe duplicate fallback", outcome: Unknown, replay: Replay{Retryable: true, Replayable: true, SafeDuplicate: true, UseFallback: true}, action: Fallback},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Arrange.
			event := Event{
				Identity:  Identity{Operation: "operation", Attempt: "first"},
				Phase:     Terminal,
				Committed: test.committed,
				Outcome:   test.outcome,
			}
			// Act.
			decision, err := Decide(t.Context(), event, 1, test.replay)
			// Assert.
			if err != nil || decision.Action != test.action || decision.Event != event {
				t.Fatalf("decision=%+v err=%v", decision, err)
			}
		})
	}
}

func TestSchedulingDeadlineHintScopeAndCancellation(t *testing.T) {
	// Arrange.
	now := time.Unix(100, 0)
	decision := Decision{Action: Retry}
	hint := Hint{Present: true, NotBefore: now.Add(30 * time.Second), Source: "adapter", Clock: "UTC"}
	cooldowns := NewCooldowns[string]()
	cooldowns.Extend("A", now.Add(time.Minute))
	// Act.
	scheduled, err := Schedule(decision, ScheduleInput{Now: now, Hint: hint, Deadline: now.Add(5 * time.Second)})
	invalid, invalidErr := Schedule(
		decision,
		ScheduleInput{Now: now, Hint: Hint{Present: true}, InvalidHint: IgnoreHint},
	)
	_, rejected := Schedule(decision, ScheduleInput{Now: now, Hint: Hint{Present: true}})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	cancelErr := Wait(ctx, now, func() time.Time { return now })
	// Assert.
	if err != nil || scheduled.Action != Defer || scheduled.NotBefore.Before(hint.NotBefore) {
		t.Fatal("early retry")
	}
	if invalidErr != nil || invalid.Reason != HintIgnored || !errors.Is(rejected, ErrInvalidHint) {
		t.Fatal("invalid hint policy")
	}
	if !cooldowns.Until("B").IsZero() || !errors.Is(cancelErr, context.Canceled) {
		t.Fatal("scope/cancel invariant")
	}
}
