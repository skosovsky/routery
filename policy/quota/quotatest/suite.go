package quotatest

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"
	"testing"
	"time"

	"github.com/skosovsky/routery/policy/quota"
)

const (
	fixtureCleanupTimeout        = 5 * time.Second
	suiteTimeout                 = 30 * time.Second
	initialUnits          uint64 = 1
	partialUnits          uint64 = 2
	completeUnits         uint64 = 4
	changedUnits          uint64 = 3
	extraUnits                   = completeUnits - initialUnits
	scopeChange                  = "scope"
)

// Status separates unsupported fault injection from a successful check.
type Status uint8

const (
	Passed Status = iota
	Failed
	Unsupported
)

// Result records evidence for a required or capability-specific scenario.
type Result struct {
	Name     string
	Required bool
	Status   Status
	Err      error
}

// Report exposes actual coverage; unsupported checks never count as passed.
type Report struct{ Results []Result }

func (report Report) RequiredPassed() bool {
	for _, result := range report.Results {
		if result.Required && result.Status != Passed {
			return false
		}
	}
	return len(report.Results) > 0
}
func (report Report) Complete() bool {
	for _, result := range report.Results {
		if result.Status != Passed {
			return false
		}
	}
	return len(report.Results) > 0
}

// Record is authoritative host ledger inspection, not the state of a local Session.
// Applications/Finalizations count durable mutations, not retries of client methods.
// Inspect also returns Available before reservation, with Found=false and no error.
type Record[Scope comparable, Unit comparable, Handle comparable, Reason comparable] struct {
	Found         bool
	Request       quota.ReserveRequest[Scope, Unit]
	Reservation   quota.Reservation[Handle, Reason]
	State         quota.State
	Actual        map[Unit]uint64
	Available     map[Unit]uint64
	Applications  uint64
	Finalizations uint64
	Retained      bool
}

// Fixture creates independent clients sharing one backend and honest ledger inspection.
// Optional faults apply the write before losing its acknowledgment. Every callback is bounded.
// The primary request estimates exactly1 Unit; capacity permits four unique holds.
type Fixture[Scope comparable, Unit comparable, Handle comparable, Reason comparable] struct {
	Request        quota.ReserveRequest[Scope, Unit]
	OtherScope     Scope
	Unit           Unit
	Client         func() quota.Backend[Scope, Unit, Handle, Reason]
	Inspect        func(context.Context, quota.ReserveRequest[Scope, Unit]) (Record[Scope, Unit, Handle, Reason], error)
	LoseReserveAck func(context.Context) error
	LoseCommitAck  func(context.Context) error
	Expire         func(context.Context, Handle) error
	Restart        func(context.Context) error
	Close          func(context.Context) error
}

// Factory resets authoritative state for each scenario. It must not reuse local sessions.
type Factory[Scope comparable, Unit comparable, Handle comparable, Reason comparable] func(context.Context) (Fixture[Scope, Unit, Handle, Reason], error)

type scenario[Scope comparable, Unit comparable, Handle comparable, Reason comparable] struct {
	name      string
	required  bool
	supported func(Fixture[Scope, Unit, Handle, Reason]) bool
	check     func(context.Context, Fixture[Scope, Unit, Handle, Reason]) error
}

// Check runs scenarios with fresh fixtures; use Run to report through Go testing.
// Context cancellation remains a fixture/backend precondition, not forced goroutine termination.
func Check[Scope comparable, Unit comparable, Handle comparable, Reason comparable](
	ctx context.Context,
	factory Factory[Scope, Unit, Handle, Reason],
) Report {
	report := Report{Results: nil}
	for _, test := range scenarios[Scope, Unit, Handle, Reason]() {
		result := Result{Name: test.name, Required: test.required, Status: Passed, Err: nil}
		if factory == nil {
			result.Status = Failed
			result.Err = errors.New("quotatest: nil fixture factory")
			report.Results = append(report.Results, result)
			continue
		}
		fixture, err := factory(ctx)
		if err == nil {
			err = validFixture(fixture)
		}
		if err != nil {
			result.Status = Failed
			result.Err = err
		} else if test.supported != nil && !test.supported(fixture) {
			result.Status = Unsupported
		} else if err = test.check(ctx, fixture); err != nil {
			result.Status = Failed
			result.Err = err
		}
		if fixture.Close != nil {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), fixtureCleanupTimeout)
			closeErr := fixture.Close(cleanup)
			cancel()
			if closeErr != nil {
				result.Status = Failed
				result.Err = errors.Join(result.Err, closeErr)
			}
		}
		report.Results = append(report.Results, result)
	}
	return report
}

// Run reports failures and explicitly logs unsupported capabilities, returning full coverage.
func Run[Scope comparable, Unit comparable, Handle comparable, Reason comparable](
	t *testing.T,
	factory Factory[Scope, Unit, Handle, Reason],
) Report {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), suiteTimeout)
	defer cancel()
	report := Check(ctx, factory)
	for _, result := range report.Results {
		if result.Status == Unsupported {
			t.Logf("UNSUPPORTED %s: no evidence for this optional fault scenario", result.Name)
			continue
		}
		t.Run(result.Name, func(t *testing.T) {
			switch result.Status {
			case Failed:
				t.Fatal(result.Err)
			case Unsupported:
				t.Log("UNSUPPORTED: no evidence for this optional fault scenario")
			case Passed:
				t.Log("PASS: conformance evidence recorded")
			}
		})
	}
	return report
}

func validFixture[Scope comparable, Unit comparable, Handle comparable, Reason comparable](
	fixture Fixture[Scope, Unit, Handle, Reason],
) error {
	if fixture.Client == nil || fixture.Inspect == nil || fixture.Close == nil ||
		fixture.Request.Identity.Operation == "" ||
		fixture.Request.Identity.Attempt == "" ||
		fixture.Request.Fingerprint == "" ||
		fixture.Request.Scope == fixture.OtherScope ||
		len(fixture.Request.Estimated) != 1 ||
		fixture.Request.Estimated[fixture.Unit] != initialUnits {
		return errors.New("quotatest: invalid fixture contract")
	}
	return nil
}

func scenarios[Scope comparable, Unit comparable, Handle comparable, Reason comparable]() []scenario[Scope, Unit, Handle, Reason] {
	return []scenario[Scope, Unit, Handle, Reason]{
		{name: "deduplication", required: true, supported: nil, check: deduplicate[Scope, Unit, Handle, Reason]},
		{name: "mismatched-reserve", required: true, supported: nil, check: mismatched[Scope, Unit, Handle, Reason]},
		{name: "concurrent-finalization", required: true, supported: nil, check: finalize[Scope, Unit, Handle, Reason]},
		{name: "release-idempotency", required: true, supported: nil, check: releaseProof[Scope, Unit, Handle, Reason]},
		{name: "pending-overage", required: true, supported: nil, check: pending[Scope, Unit, Handle, Reason]},
		{
			name:      "lost-reserve-ack",
			required:  false,
			supported: func(f Fixture[Scope, Unit, Handle, Reason]) bool { return f.LoseReserveAck != nil },
			check:     reserveAck[Scope, Unit, Handle, Reason],
		},
		{
			name:      "lost-commit-ack",
			required:  false,
			supported: func(f Fixture[Scope, Unit, Handle, Reason]) bool { return f.LoseCommitAck != nil },
			check:     commitAck[Scope, Unit, Handle, Reason],
		},
		{
			name:      "expiration",
			required:  false,
			supported: func(f Fixture[Scope, Unit, Handle, Reason]) bool { return f.Expire != nil },
			check:     expiration[Scope, Unit, Handle, Reason],
		},
		{
			name:      "restart-recovery",
			required:  false,
			supported: func(f Fixture[Scope, Unit, Handle, Reason]) bool { return f.Restart != nil && f.LoseCommitAck != nil },
			check:     restartRecovery[Scope, Unit, Handle, Reason],
		},
	}
}

func admit[Scope comparable, Unit comparable, Handle comparable, Reason comparable](
	ctx context.Context,
	fixture Fixture[Scope, Unit, Handle, Reason],
) (quota.Reservation[Handle, Reason], *quota.Session[Scope, Unit, Handle, Reason], error) {
	reservation, session, err := quota.Admit(ctx, fixture.Client(), fixture.Request, quota.FailClosed)
	if err == nil && (reservation.Admission != quota.Admitted || session == nil) {
		err = errors.New("quotatest: fixture must admit primary request")
	}
	return reservation, session, err
}

func inspect[Scope comparable, Unit comparable, Handle comparable, Reason comparable](
	ctx context.Context,
	fixture Fixture[Scope, Unit, Handle, Reason],
	request quota.ReserveRequest[Scope, Unit],
) (Record[Scope, Unit, Handle, Reason], error) {
	record, err := fixture.Inspect(ctx, request)
	if err == nil &&
		(!record.Found || record.Request.Scope != request.Scope || record.Request.Identity != request.Identity || record.Request.Fingerprint != request.Fingerprint || !record.Request.Deadline.Equal(request.Deadline) || !maps.Equal(record.Request.Estimated, request.Estimated) || record.Applications != 1 || len(record.Available) != 1 || !hasUnit(record.Available, fixture.Unit)) {
		err = errors.New("quotatest: reservation identity/application mismatch")
	}
	return record, err
}

func deduplicate[Scope comparable, Unit comparable, Handle comparable, Reason comparable](
	ctx context.Context,
	f Fixture[Scope, Unit, Handle, Reason],
) error {
	start := make(chan struct{})
	var workers sync.WaitGroup
	reservations := make([]quota.Reservation[Handle, Reason], 2)
	failures := make([]error, 2)
	for index := range 2 {
		client := f.Client()
		workers.Go(func() { <-start; reservations[index], failures[index] = client.Reserve(ctx, f.Request) })
	}
	close(start)
	workers.Wait()
	if failures[0] != nil || failures[1] != nil {
		return errors.Join(failures...)
	}
	if reservations[0].Admission != quota.Admitted || reservations[1].Admission != quota.Admitted ||
		reservations[0].Handle != reservations[1].Handle {
		return errors.New("quotatest: duplicate reservation not deduplicated")
	}
	if _, err := inspect(ctx, f, f.Request); err != nil {
		return err
	}
	for _, change := range []string{scopeChange, "operation", "attempt"} {
		request := f.Request
		switch change {
		case scopeChange:
			request.Scope = f.OtherScope
		case "operation":
			request.Identity.Operation += "/different"
		case "attempt":
			request.Identity.Attempt += "/different"
		}
		reservation, err := f.Client().Reserve(ctx, request)
		if change == scopeChange && errors.Is(err, quota.ErrConflict) {
			continue
		}
		if err != nil {
			return err
		}
		if reservation.Admission != quota.Admitted || reservation.Handle == reservations[0].Handle {
			return fmt.Errorf("quotatest: %s identity collapsed or fixture capacity insufficient", change)
		}
		if _, err = inspect(ctx, f, request); err != nil {
			return err
		}
	}
	return nil
}

func mismatched[Scope comparable, Unit comparable, Handle comparable, Reason comparable](
	ctx context.Context,
	f Fixture[Scope, Unit, Handle, Reason],
) error {
	if _, _, err := admit(ctx, f); err != nil {
		return err
	}
	for _, change := range []string{"estimate", "fingerprint", "deadline"} {
		request := f.Request
		request.Estimated = maps.Clone(request.Estimated)
		switch change {
		case "estimate":
			request.Estimated[f.Unit] = partialUnits
		case "fingerprint":
			request.Fingerprint += "/changed"
		case "deadline":
			request.Deadline = request.Deadline.Add(time.Minute)
		}
		if _, err := f.Client().Reserve(ctx, request); !errors.Is(err, quota.ErrConflict) {
			return fmt.Errorf("quotatest: mismatched %s accepted: %w", change, err)
		}
	}
	_, err := inspect(ctx, f, f.Request)
	return err
}

func finalize[Scope comparable, Unit comparable, Handle comparable, Reason comparable](
	ctx context.Context,
	f Fixture[Scope, Unit, Handle, Reason],
) error {
	reservation, commit, err := admit(ctx, f)
	if err != nil {
		return err
	}
	_, release, err := admit(ctx, f)
	if err != nil {
		return err
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	workers.Go(
		func() { <-start; results <- commit.Settle(ctx, "commit", map[Unit]uint64{f.Unit: completeUnits}, true) },
	)
	workers.Go(func() {
		<-start
		results <- release.Release(ctx, quota.ReleaseProof[Handle, Reason]{Handle: reservation.Handle, ID: "release", NotExecuted: true, Reason: *new(Reason)})
	})
	close(start)
	workers.Wait()
	close(results)
	successes, conflicts := 0, 0
	for result := range results {
		switch {
		case result == nil:
			successes++
		case errors.Is(result, quota.ErrConflict):
			conflicts++
		default:
			return result
		}
	}
	if successes != 1 || conflicts != 1 {
		return fmt.Errorf("quotatest: conflicting finalizations succeeded=%d conflicts=%d", successes, conflicts)
	}
	record, err := inspect(ctx, f, f.Request)
	if err != nil {
		return err
	}
	if record.Finalizations != 1 || (record.State != quota.Committed && record.State != quota.Released) {
		return errors.New("quotatest: finalization not exclusive")
	}
	return nil
}

func pending[Scope comparable, Unit comparable, Handle comparable, Reason comparable](
	ctx context.Context,
	f Fixture[Scope, Unit, Handle, Reason],
) error {
	reservation, session, err := admit(ctx, f)
	if err != nil {
		return err
	}
	initial, inspectErr := inspect(ctx, f, f.Request)
	if inspectErr != nil {
		return inspectErr
	}
	if err = session.Settle(ctx, "usage", map[Unit]uint64{f.Unit: partialUnits}, false); err != nil {
		return err
	}
	record, err := inspect(ctx, f, f.Request)
	if err != nil {
		return err
	}
	if record.State != quota.Pending || !record.Retained || record.Finalizations != 0 ||
		record.Actual[f.Unit] != partialUnits ||
		record.Available[f.Unit] > initial.Available[f.Unit] {
		return errors.New("quotatest: Pending refunded or facts lost")
	}
	_, independent, err := admit(ctx, f)
	if err != nil {
		return err
	}
	if err = independent.Settle(ctx, "usage", map[Unit]uint64{f.Unit: completeUnits}, true); err != nil {
		return err
	}
	if err = session.Settle(ctx, "usage", map[Unit]uint64{f.Unit: completeUnits}, true); err != nil {
		return err
	}
	_, duplicate, err := admit(ctx, f)
	if err != nil {
		return err
	}
	if err = duplicate.Settle(ctx, "usage", map[Unit]uint64{f.Unit: completeUnits}, true); err != nil {
		return err
	}
	if err = duplicate.Settle(
		ctx,
		"different",
		map[Unit]uint64{f.Unit: completeUnits},
		true,
	); !errors.Is(
		err,
		quota.ErrConflict,
	) {
		return errors.New("quotatest: changed settlement identity accepted")
	}
	if err = f.Client().
		Commit(ctx, quota.Settlement[Handle, Unit]{Handle: reservation.Handle, ID: "usage", Actual: map[Unit]uint64{f.Unit: changedUnits}, Complete: true}); !errors.Is(
		err,
		quota.ErrConflict,
	) {
		return errors.New("quotatest: changed finalized usage accepted")
	}
	if err = f.Client().
		Release(ctx, quota.ReleaseProof[Handle, Reason]{Handle: reservation.Handle, ID: "release", NotExecuted: true, Reason: *new(Reason)}); !errors.Is(
		err,
		quota.ErrConflict,
	) {
		return errors.New("quotatest: release after commit accepted")
	}
	record, err = inspect(ctx, f, f.Request)
	if err != nil {
		return err
	}
	if record.State != quota.Committed || record.Actual[f.Unit] != completeUnits || record.Finalizations != 1 ||
		record.Available[f.Unit] != afterOverage(initial.Available[f.Unit]) {
		return errors.New("quotatest: overage truncated or settlement duplicated")
	}
	return nil
}

func hasUnit[Unit comparable](available map[Unit]uint64, unit Unit) bool {
	_, ok := available[unit]
	return ok
}

func committedUsage[Scope comparable, Unit comparable, Handle comparable, Reason comparable](
	ctx context.Context, f Fixture[Scope, Unit, Handle, Reason], initial Record[Scope, Unit, Handle, Reason],
) error {
	record, err := inspect(ctx, f, f.Request)
	if err != nil {
		return err
	}
	if record.State != quota.Committed || record.Retained || record.Finalizations != 1 ||
		record.Actual[f.Unit] != completeUnits ||
		record.Available[f.Unit] != afterOverage(initial.Available[f.Unit]) {
		return errors.New("quotatest: committed usage refunded, duplicated or truncated")
	}
	return nil
}

func reserveAck[Scope comparable, Unit comparable, Handle comparable, Reason comparable](
	ctx context.Context,
	f Fixture[Scope, Unit, Handle, Reason],
) error {
	before, err := f.Inspect(ctx, f.Request)
	if err != nil {
		return err
	}
	if !hasUnit(before.Available, f.Unit) || before.Available[f.Unit] < initialUnits {
		return errors.New("quotatest: missing initial reserve credits")
	}
	if err = f.LoseReserveAck(ctx); err != nil {
		return err
	}
	_, session, err := quota.Admit(ctx, f.Client(), f.Request, quota.FailOpen)
	var unknown *quota.ReserveError
	if !errors.As(err, &unknown) || !unknown.UnknownAck || session != nil {
		return errors.New("quotatest: unknown reserve acknowledgment lost uncertainty")
	}
	record, err := inspect(ctx, f, f.Request)
	if err != nil {
		return err
	}
	if record.State != quota.Reserved || !record.Retained || record.Finalizations != 0 {
		return errors.New("quotatest: lost reserve acknowledgment failed to retain hold")
	}
	if record.Available[f.Unit] != before.Available[f.Unit]-initialUnits {
		return errors.New("quotatest: lost reserve acknowledgment refunded hold")
	}
	reservation, _, err := admit(ctx, f)
	if err != nil {
		return err
	}
	if reservation.Handle != record.Reservation.Handle {
		return errors.New("quotatest: lost reserve identity changed")
	}
	replayed, err := inspect(ctx, f, f.Request)
	if err != nil {
		return err
	}
	if replayed.Available[f.Unit] != record.Available[f.Unit] || !replayed.Retained ||
		replayed.State != quota.Reserved {
		return errors.New("quotatest: reserve replay changed hold accounting")
	}
	return nil
}

func commitAck[Scope comparable, Unit comparable, Handle comparable, Reason comparable](
	ctx context.Context,
	f Fixture[Scope, Unit, Handle, Reason],
) error {
	if err := f.LoseCommitAck(ctx); err != nil {
		return err
	}
	_, session, err := admit(ctx, f)
	if err != nil {
		return err
	}
	initial, err := inspect(ctx, f, f.Request)
	if err != nil {
		return err
	}
	if err = session.Settle(
		ctx,
		"lost",
		map[Unit]uint64{f.Unit: completeUnits},
		true,
	); err == nil ||
		session.State() != quota.Pending {
		return errors.New("quotatest: lost commit acknowledgment invented certainty")
	}
	if err = committedUsage(ctx, f, initial); err != nil {
		return err
	}
	if err = session.Settle(ctx, "lost", map[Unit]uint64{f.Unit: completeUnits}, true); err != nil {
		return err
	}
	return committedUsage(ctx, f, initial)
}

func expiration[Scope comparable, Unit comparable, Handle comparable, Reason comparable](
	ctx context.Context,
	f Fixture[Scope, Unit, Handle, Reason],
) error {
	reservation, session, err := admit(ctx, f)
	if err != nil {
		return err
	}
	if err = session.Settle(ctx, "pending", map[Unit]uint64{f.Unit: partialUnits}, false); err != nil {
		return err
	}
	beforeExpiry, inspectErr := inspect(ctx, f, f.Request)
	if inspectErr != nil {
		return inspectErr
	}
	if err = f.Expire(ctx, reservation.Handle); err != nil {
		return err
	}
	if err = session.Settle(
		ctx,
		"pending",
		map[Unit]uint64{f.Unit: completeUnits},
		true,
	); !errors.Is(
		err,
		quota.ErrUnknownHandle,
	) {
		return errors.New("quotatest: expired handle silently finalized")
	}
	record, err := inspect(ctx, f, f.Request)
	if err != nil {
		return err
	}
	if session.State() != quota.Pending || !record.Retained || record.State == quota.Released ||
		record.Finalizations != 0 ||
		record.Actual[f.Unit] != partialUnits || record.Available[f.Unit] > beforeExpiry.Available[f.Unit] {
		return errors.New("quotatest: TTL invented zero usage/refund")
	}
	return nil
}

func restartRecovery[Scope comparable, Unit comparable, Handle comparable, Reason comparable](
	ctx context.Context,
	f Fixture[Scope, Unit, Handle, Reason],
) error {
	if err := f.LoseCommitAck(ctx); err != nil {
		return err
	}
	_, session, err := admit(ctx, f)
	if err != nil {
		return err
	}
	initial, err := inspect(ctx, f, f.Request)
	if err != nil {
		return err
	}
	if err = session.Settle(ctx, "recovery", map[Unit]uint64{f.Unit: completeUnits}, true); err == nil {
		return errors.New("quotatest: fixture did not lose acknowledgment")
	}
	if err = committedUsage(ctx, f, initial); err != nil {
		return err
	}
	if err = f.Restart(ctx); err != nil {
		return err
	}
	_, recovered, err := admit(ctx, f)
	if err != nil {
		return err
	}
	if err = recovered.Settle(ctx, "recovery", map[Unit]uint64{f.Unit: completeUnits}, true); err != nil {
		return err
	}
	return committedUsage(ctx, f, initial)
}

func afterOverage(available uint64) uint64 {
	if available < extraUnits {
		return 0
	}
	return available - extraUnits
}

func releaseProof[Scope comparable, Unit comparable, Handle comparable, Reason comparable](
	ctx context.Context,
	f Fixture[Scope, Unit, Handle, Reason],
) error {
	reservation, _, err := admit(ctx, f)
	if err != nil {
		return err
	}
	initial, err := inspect(ctx, f, f.Request)
	if err != nil {
		return err
	}
	proof := quota.ReleaseProof[Handle, Reason]{
		Handle:      reservation.Handle,
		ID:          "proven-absent",
		NotExecuted: false,
		Reason:      *new(Reason),
	}
	if err = f.Client().Release(ctx, proof); !errors.Is(err, quota.ErrConflict) {
		return errors.New("quotatest: release without proof accepted")
	}
	proof.NotExecuted = true
	for range 2 {
		if err = f.Client().Release(ctx, proof); err != nil {
			return err
		}
	}
	changed := proof
	changed.ID = "changed"
	if err = f.Client().Release(ctx, changed); !errors.Is(err, quota.ErrConflict) {
		return errors.New("quotatest: changed release identity accepted")
	}
	settlement := quota.Settlement[Handle, Unit]{
		Handle:   reservation.Handle,
		ID:       "after-release",
		Actual:   map[Unit]uint64{f.Unit: completeUnits},
		Complete: true,
	}
	if err = f.Client().Commit(ctx, settlement); !errors.Is(err, quota.ErrConflict) {
		return errors.New("quotatest: commit after release accepted")
	}
	settlement.Complete = false
	if err = f.Client().Pending(ctx, settlement); !errors.Is(err, quota.ErrConflict) {
		return errors.New("quotatest: Pending resurrected released hold")
	}
	record, err := inspect(ctx, f, f.Request)
	if err != nil {
		return err
	}
	if record.State != quota.Released || record.Finalizations != 1 || record.Retained ||
		record.Available[f.Unit] != initial.Available[f.Unit]+initialUnits {
		return errors.New("quotatest: release duplicated or refund incorrect")
	}
	return nil
}
