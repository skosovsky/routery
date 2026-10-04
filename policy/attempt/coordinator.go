package attempt

import (
	"errors"
	"sync"
)

// ErrInvalidEvent indicates contradictory lifecycle metadata or invalid identity.
var ErrInvalidEvent = errors.New("routery/attempt: invalid event")

// Phase is the monotonic local execution phase, independent of delivery commit.
type Phase uint8

const (
	BeforeDispatch Phase = iota
	Dispatched
	StreamOpened
	Terminal
)

// Outcome describes certainty about remote execution, not local success.
type Outcome uint8

const (
	Unknown Outcome = iota
	NotExecuted
	Completed
)

// Identity names a logical operation and a unique physical attempt.
type Identity struct {
	Operation string
	Attempt   string
}

// Event is a complete monotonic lifecycle update, safe to submit repeatedly.
// Terminal unknown may be reconciled later with definitive remote knowledge.
type Event struct {
	Identity  Identity
	Phase     Phase
	Committed bool
	Outcome   Outcome
}

// StartResult distinguishes budget exhaustion from malformed or reused identities.
type StartResult struct {
	Started   bool
	Event     Event
	Remaining int
}

// Coordinator isolates synchronized in-process state for one logical operation.
// Repeated Begin is not a second dispatch; it errors. IDs are never recycled.
type Coordinator struct {
	mu        sync.Mutex
	operation string
	limit     int
	events    map[string]Event
}

// NewCoordinator declares the attempt budget before any execution.
func NewCoordinator(operation string, limit int) (*Coordinator, error) {
	if operation == "" || limit < 1 {
		return nil, ErrInvalidEvent
	}
	return &Coordinator{mu: sync.Mutex{}, operation: operation, limit: limit, events: make(map[string]Event)}, nil
}

// Begin allocates an identity and consumes budget without starting remote execution.
func (coordinator *Coordinator) Begin(id Identity) (StartResult, error) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if id.Operation != coordinator.operation || id.Attempt == "" {
		return StartResult{}, ErrInvalidEvent
	}
	if _, exists := coordinator.events[id.Attempt]; exists {
		return StartResult{}, ErrInvalidEvent
	}
	event := Event{Identity: id, Phase: BeforeDispatch, Committed: false, Outcome: Unknown}
	if len(coordinator.events) >= coordinator.limit {
		return StartResult{Started: false, Event: event, Remaining: 0}, nil
	}
	coordinator.events[id.Attempt] = event
	return StartResult{Started: true, Event: event, Remaining: coordinator.limit - len(coordinator.events)}, nil
}

// Update records a monotonic event. Reconciliation can resolve a terminal unknown.
func (coordinator *Coordinator) Update(event Event) error {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	previous, exists := coordinator.events[event.Identity.Attempt]
	if !exists || event.Identity != previous.Identity || !validEvent(event) {
		return ErrInvalidEvent
	}
	if event.Phase < previous.Phase || (previous.Committed && !event.Committed) {
		return ErrInvalidEvent
	}
	if previous.Outcome != Unknown && event.Outcome != previous.Outcome {
		return ErrInvalidEvent
	}
	coordinator.events[event.Identity.Attempt] = event
	return nil
}

// Snapshot returns a value copy suitable for diagnostics or policy evaluation.
func (coordinator *Coordinator) Snapshot(id Identity) (Event, int, error) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	event, exists := coordinator.events[id.Attempt]
	if !exists || event.Identity != id {
		return Event{}, 0, ErrInvalidEvent
	}
	return event, coordinator.limit - len(coordinator.events), nil
}

func validEvent(event Event) bool {
	if event.Phase > Terminal || event.Outcome > Completed {
		return false
	}
	if event.Committed && (event.Phase < Dispatched || event.Outcome == NotExecuted) {
		return false
	}
	if event.Outcome == Completed && event.Phase != Terminal {
		return false
	}
	return event.Outcome != NotExecuted || event.Phase == Terminal
}
