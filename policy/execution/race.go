package execution

import (
	"context"
	"errors"
	"slices"
	"sync"

	"github.com/skosovsky/routery"
	"github.com/skosovsky/routery/policy/attempt"
)

// Permissions are explicit caller-owned replay and effect evidence, not approvals.
type Permissions struct {
	Replayable       bool
	DuplicateCost    bool
	ReadOnly         bool
	DuplicateEffects bool
}

// AcceptanceProfile distinguishes terminal validation from explicit early stream ownership.
type AcceptanceProfile uint8

const (
	CompleteOnly AcceptanceProfile = iota
	EarlyOwned
)

// RaceStatus distinguishes policy refusal and absence of an accepted result from failures.
type RaceStatus uint8

const (
	NoAccepted RaceStatus = iota
	RaceAccepted
	DuplicationForbidden
)

// RaceEntry records each returned physical attempt, including rejected and late results.
type RaceEntry[Kind comparable, Reason comparable, Payload any] struct {
	Identity attempt.Identity
	Result   Result[Kind, Reason, Payload]
	Err      error
	Accepted bool
}

// Journal retains synchronized accounting references without publishing any result data.
// Snapshots are not completion barriers; provider callbacks may return late or never.
type Journal[Kind comparable, Reason comparable, Payload any] struct {
	mu      sync.Mutex
	entries []RaceEntry[Kind, Reason, Payload]
	changes chan struct{}
}

// Changes coalesces notifications of newly returned entries; Snapshot is authoritative.
// This channel is not a completion signal and is not closed while late callbacks may return.
func (journal *Journal[Kind, Reason, Payload]) Changes() <-chan struct{} {
	return journal.changes
}

// Snapshot returns a detached slice; result resources and Receipts retain shared ownership.
func (journal *Journal[Kind, Reason, Payload]) Snapshot() []RaceEntry[Kind, Reason, Payload] {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	return slices.Clone(journal.entries)
}

func (journal *Journal[Kind, Reason, Payload]) append(entry RaceEntry[Kind, Reason, Payload]) {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	journal.entries = append(journal.entries, entry)
	select {
	case journal.changes <- struct{}{}:
	default:
	}
}

// RaceResult exposes the winner and a journal that may receive later loser results.
type RaceResult[Kind comparable, Reason comparable, Payload any] struct {
	Status              RaceStatus
	Winner              Result[Kind, Reason, Payload]
	Journal             *Journal[Kind, Reason, Payload]
	NestedAttemptsKnown bool
}

// Race bounds physical dispatch and validation concurrency, not distributed provider effects.
// Accept must not publish branch output. Request plans and callbacks must be concurrency-safe.
type Race[Req any, Kind comparable, Reason comparable, Payload any] struct {
	Boundary            Boundary[Req, Kind, Reason, Payload]
	Workers             int
	Permissions         Permissions
	Profile             AcceptanceProfile
	Accept              func(context.Context, Result[Kind, Reason, Payload]) (bool, error)
	NestedAttemptsKnown bool
}

// Run races eligible caller plans; each physical attempt uses its own Boundary admission.
func (race Race[Req, Kind, Reason, Payload]) Run(
	ctx context.Context, coordinator *attempt.Coordinator, plans []Step[Req],
) (RaceResult[Kind, Reason, Payload], error) {
	var result RaceResult[Kind, Reason, Payload]
	result.NestedAttemptsKnown = race.NestedAttemptsKnown
	if err := race.validate(coordinator, plans); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if len(plans) > 1 && !race.Permissions.allowsDuplication() {
		result.Status = DuplicationForbidden
		return result, nil
	}
	queue := make(chan Step[Req], len(plans))
	for _, plan := range plans {
		queue <- plan
	}
	close(queue)
	control := &raceControl[Req]{mu: sync.Mutex{}, queue: queue, err: nil, finalized: false}
	changes := make(chan struct{}, 1)
	journal := &Journal[Kind, Reason, Payload]{mu: sync.Mutex{}, entries: nil, changes: changes}
	result.Journal = journal
	workers := min(race.Workers, len(plans))
	handlers := make([]routery.BasicRouteHandler[struct{}, Result[Kind, Reason, Payload]], workers)
	for index := range handlers {
		handlers[index] = func(call routery.RouteCall[struct{}]) (routery.BasicRouteResult[Result[Kind, Reason, Payload]], error) {
			return race.worker(call.Context, coordinator, control, journal)
		}
	}
	winner, err := routery.FirstCompleted(handlers...)(routery.NewRouteCall(ctx, struct{}{}))
	if fatalErr := control.failure(); fatalErr != nil {
		return result, errors.Join(fatalErr, err, winner.Lifetime.Close())
	}
	if errors.Is(err, routery.ErrNoSuccessfulOutcome) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if err = checkWinner(ctx, winner.Payload); err != nil {
		return result, errors.Join(err, winner.Lifetime.Close())
	}
	if err = control.finalize(); err != nil {
		return result, errors.Join(err, winner.Lifetime.Close())
	}
	result.Status, result.Winner = RaceAccepted, winner.Payload
	return result, nil
}

// raceControl serializes plan authorization with observation of fatal contract errors.
// Already authorized plans keep their independent lifecycle and journal accounting.
type raceControl[Req any] struct {
	mu        sync.Mutex
	queue     <-chan Step[Req]
	err       error
	finalized bool
}

func (control *raceControl[Req]) next(ctx context.Context) (Step[Req], bool, error) {
	control.mu.Lock()
	defer control.mu.Unlock()
	var zero Step[Req]
	if control.err != nil {
		return zero, false, control.err
	}
	if control.finalized {
		return zero, false, nil
	}
	if err := ctx.Err(); err != nil {
		return zero, false, err
	}
	plan, ok := <-control.queue
	return plan, ok, nil
}

func (control *raceControl[Req]) observe(err error) {
	if !isControlError(err) {
		return
	}
	control.mu.Lock()
	defer control.mu.Unlock()
	control.err = errors.Join(control.err, err)
}

func (control *raceControl[Req]) failure() error {
	control.mu.Lock()
	defer control.mu.Unlock()
	return control.err
}

// finalize is the acceptance linearization point shared with fatal observation.
// Later failures remain in the journal; they cannot change an already accepted result.
func (control *raceControl[Req]) finalize() error {
	control.mu.Lock()
	defer control.mu.Unlock()
	if control.err != nil {
		return control.err
	}
	control.finalized = true
	return nil
}

func checkWinner[Kind comparable, Reason comparable, Payload any](
	ctx context.Context,
	winner Result[Kind, Reason, Payload],
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	event, _, err := winner.Receipt.Snapshot()
	if err != nil {
		return err
	}
	if event.Committed {
		return attempt.ErrInvalidEvent
	}
	return nil
}

func (permissions Permissions) allowsDuplication() bool {
	return permissions.Replayable && permissions.DuplicateCost && (permissions.ReadOnly || permissions.DuplicateEffects)
}

func (race Race[Req, Kind, Reason, Payload]) validate(coordinator *attempt.Coordinator, plans []Step[Req]) error {
	if coordinator == nil || race.Workers < 1 || race.Accept == nil || race.Profile > EarlyOwned || len(plans) == 0 {
		return ErrInvalidBoundary
	}
	seen := make(map[attempt.Identity]struct{}, len(plans))
	operation := plans[0].Identity.Operation
	for _, plan := range plans {
		if plan.Identity.Operation == "" || plan.Identity.Operation != operation || plan.Identity.Attempt == "" {
			return attempt.ErrInvalidEvent
		}
		if _, exists := seen[plan.Identity]; exists {
			return attempt.ErrInvalidEvent
		}
		if _, _, err := coordinator.Snapshot(plan.Identity); err == nil {
			return attempt.ErrInvalidEvent
		}
		seen[plan.Identity] = struct{}{}
	}
	return nil
}

func (race Race[Req, Kind, Reason, Payload]) worker(
	ctx context.Context,
	coordinator *attempt.Coordinator,
	control *raceControl[Req],
	journal *Journal[Kind, Reason, Payload],
) (routery.BasicRouteResult[Result[Kind, Reason, Payload]], error) {
	var failures []error
	for {
		plan, ok, err := control.next(ctx)
		if err != nil {
			return routery.AbortResult[routery.BasicKind, routery.BasicReason, Result[Kind, Reason, Payload]](), err
		}
		if !ok {
			break
		}
		result, runErr := race.Boundary.Run(routery.NewRouteCall(ctx, plan.Request), coordinator, plan.Identity)
		accepted, err := race.accept(ctx, result, runErr)
		control.observe(err)
		if accepted {
			journal.append(
				RaceEntry[Kind, Reason, Payload]{Identity: plan.Identity, Result: result, Err: err, Accepted: true},
			)
			winner := routery.BasicHandled(result)
			winner.Lifetime, winner.Match = result.Route.Lifetime, result.Route.Match
			return winner, nil
		}
		closeErr := result.Route.Lifetime.Close()
		if result.Receipt != nil {
			_, _, settlementErr := result.Receipt.Snapshot()
			closeErr = errors.Join(closeErr, settlementErr)
		}
		if err = errors.Join(err, closeErr); err != nil {
			failures = append(failures, err)
		}
		control.observe(closeErr)
		journal.append(
			RaceEntry[Kind, Reason, Payload]{Identity: plan.Identity, Result: result, Err: err, Accepted: false},
		)
		if closeErr != nil || (runErr != nil && !result.Started) || control.failure() != nil {
			return routery.AbortResult[routery.BasicKind, routery.BasicReason, Result[Kind, Reason, Payload]](),
				errors.Join(err, control.failure())
		}
	}
	return routery.BasicNext[Result[Kind, Reason, Payload]](routery.BasicReasonNoMatch), errors.Join(failures...)
}

func (race Race[Req, Kind, Reason, Payload]) accept(
	ctx context.Context, result Result[Kind, Reason, Payload], runErr error,
) (bool, error) {
	if runErr != nil || !result.Started || result.Receipt == nil {
		return false, runErr
	}
	if result.Route.Action != routery.ActionStop || !result.Route.HasPayload {
		return false, nil
	}
	event, _, err := result.Receipt.Snapshot()
	if err != nil {
		return false, err
	}
	if event.Committed {
		return false, attempt.ErrInvalidEvent
	}
	if race.Profile == CompleteOnly && event.Phase != attempt.Terminal {
		return false, nil
	}
	if race.Profile == EarlyOwned && (result.Route.Lifetime == nil || event.Phase < attempt.StreamOpened) {
		return false, nil
	}
	if err = ctx.Err(); err != nil {
		return false, err
	}
	accepted, err := race.Accept(ctx, result)
	if err != nil || !accepted {
		return false, err
	}
	event, _, err = result.Receipt.Snapshot()
	if err != nil {
		return false, err
	}
	if event.Committed {
		return false, attempt.ErrInvalidEvent
	}
	if err = ctx.Err(); err != nil {
		return false, err
	}
	return true, nil
}
