package quota

import (
	"context"
	"errors"
	"maps"
	"sync"
	"testing"

	"github.com/skosovsky/routery/policy/attempt"
)

type testHold struct {
	request    ReserveRequest[string, string]
	state      State
	settlement *Settlement[string, string]
	release    *ReleaseProof[string, string]
}

// Atomic fake demonstrates the backend contract, not a production ledger.
type atomicBackend struct {
	mu            sync.Mutex
	remaining     uint64
	holds         map[string]*testHold
	commitCalls   int
	pendingCalls  int
	releaseCalls  int
	loseCommitAck bool
	reserveError  error
}

func newAtomicBackend() *atomicBackend {
	return &atomicBackend{remaining: 1, holds: make(map[string]*testHold)}
}

func (backend *atomicBackend) Reserve(
	_ context.Context,
	request ReserveRequest[string, string],
) (Reservation[string, string], error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if backend.reserveError != nil {
		return Reservation[string, string]{}, backend.reserveError
	}
	key := request.Identity.Operation + "/" + request.Identity.Attempt
	if previous, ok := backend.holds[key]; ok {
		if previous.request.Scope != request.Scope || previous.request.Fingerprint != request.Fingerprint ||
			!maps.Equal(previous.request.Estimated, request.Estimated) {
			return Reservation[string, string]{}, ErrConflict
		}
		return Reservation[string, string]{Admission: Admitted, Handle: key}, nil
	}
	needed := request.Estimated["units"]
	if needed > backend.remaining {
		return Reservation[string, string]{Admission: Denied, Reason: "limit"}, nil
	}
	backend.remaining -= needed
	backend.holds[key] = &testHold{request: request, state: Reserved}
	return Reservation[string, string]{Admission: Admitted, Handle: key}, nil
}

func (backend *atomicBackend) Commit(_ context.Context, settlement Settlement[string, string]) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.commitCalls++
	hold, ok := backend.holds[settlement.Handle]
	if !ok {
		return ErrUnknownHandle
	}
	if hold.state == Released {
		return ErrConflict
	}
	if hold.state == Committed {
		if hold.settlement.ID != settlement.ID || !maps.Equal(hold.settlement.Actual, settlement.Actual) {
			return ErrConflict
		}
		return nil
	}
	hold.state, hold.settlement = Committed, &settlement
	if backend.loseCommitAck {
		backend.loseCommitAck = false
		return errors.New("ack lost")
	}
	return nil
}

func (backend *atomicBackend) Release(_ context.Context, proof ReleaseProof[string, string]) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.releaseCalls++
	hold, ok := backend.holds[proof.Handle]
	if !ok {
		return ErrUnknownHandle
	}
	if hold.state == Committed || !proof.NotExecuted {
		return ErrConflict
	}
	if hold.release != nil {
		if *hold.release != proof {
			return ErrConflict
		}
		return nil
	}
	hold.state, hold.release = Released, &proof
	backend.remaining += hold.request.Estimated["units"]
	return nil
}

func (backend *atomicBackend) Pending(_ context.Context, settlement Settlement[string, string]) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.pendingCalls++
	hold, ok := backend.holds[settlement.Handle]
	if !ok {
		return ErrUnknownHandle
	}
	if hold.state == Committed || hold.state == Released {
		return ErrConflict
	}
	hold.state = Pending
	return nil
}

func testRequest(id string) ReserveRequest[string, string] {
	return ReserveRequest[string, string]{
		Scope:       "trusted",
		Identity:    attempt.Identity{Operation: "operation", Attempt: id},
		Estimated:   map[string]uint64{"units": 1},
		Fingerprint: "limit",
	}
}

func TestAtomicAdmissionAndDuplicateIdentity(t *testing.T) {
	// Arrange.
	backend := newAtomicBackend()
	var group sync.WaitGroup
	results := make(chan Admission, 2)
	// Act.
	for _, id := range []string{"A", "B"} {
		group.Go(func() {
			reservation, _, err := Admit(t.Context(), backend, testRequest(id), FailClosed)
			if err != nil {
				t.Error(err)
			}
			results <- reservation.Admission
		})
	}
	group.Wait()
	close(results)
	admitted := 0
	for result := range results {
		if result == Admitted {
			admitted++
		}
	}
	backend2 := newAtomicBackend()
	first, _, _ := Admit(t.Context(), backend2, testRequest("A"), FailClosed)
	second, _, err := Admit(t.Context(), backend2, testRequest("A"), FailClosed)
	// Assert.
	if admitted != 1 || err != nil || first.Handle != second.Handle || backend2.remaining != 0 {
		t.Fatal("atomicity or idempotency violated")
	}
}

func TestSettlementLostAckOverageAndConflicts(t *testing.T) {
	// Arrange.
	backend := newAtomicBackend()
	backend.loseCommitAck = true
	reservation, session, err := Admit(t.Context(), backend, testRequest("A"), FailClosed)
	if err != nil {
		t.Fatal(err)
	}
	actual := map[string]uint64{"units": 4}
	// Act.
	lostAck := session.Settle(t.Context(), "settlement", actual, true)
	pending := session.State()
	retried := session.Settle(t.Context(), "settlement", actual, true)
	conflict := session.Settle(t.Context(), "settlement", map[string]uint64{"units": 1}, true)
	releaseErr := session.Release(
		t.Context(),
		ReleaseProof[string, string]{Handle: reservation.Handle, ID: "release", NotExecuted: true},
	)
	// Assert.
	if lostAck == nil || pending != Pending || retried != nil || session.State() != Committed {
		t.Fatal("ack reconciliation failed")
	}
	if backend.holds[reservation.Handle].settlement.Actual["units"] != 4 || backend.commitCalls != 2 {
		t.Fatal("overage was hidden or double committed")
	}
	if !errors.Is(conflict, ErrConflict) || !errors.Is(releaseErr, ErrConflict) {
		t.Fatal("conflicting finalization accepted")
	}
}

func TestIncompleteCancelledUsageRetainedAndResolved(t *testing.T) {
	// Arrange.
	backend := newAtomicBackend()
	reservation, session, _ := Admit(t.Context(), backend, testRequest("stream"), FailClosed)
	// Act.
	err := session.Settle(t.Context(), "settlement", nil, false)
	blocked, _, _ := Admit(t.Context(), backend, testRequest("second"), FailClosed)
	badUnits := session.Settle(t.Context(), "settlement", map[string]uint64{"currency": 2}, true)
	reconcileErr := session.Release(
		t.Context(),
		ReleaseProof[string, string]{Handle: reservation.Handle, ID: "release", NotExecuted: true},
	)
	// Assert.
	if err != nil || backend.pendingCalls != 1 || blocked.Admission != Denied ||
		!errors.Is(badUnits, ErrIncompatibleUnits) {
		t.Fatal("pending/refund invariant")
	}
	if reconcileErr != nil || session.State() != Released || backend.releaseCalls != 1 {
		t.Fatal("proven reconciliation failed")
	}
}

func TestBackendFailurePolicyAndCancellation(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		// Arrange.
		backend := newAtomicBackend()
		backend.reserveError = &ReserveError{Err: ErrBackendUnavailable, UnknownAck: unknown}
		// Act.
		reservation, session, err := Admit(t.Context(), backend, testRequest("A"), FailOpen)
		_, _, closedErr := Admit(t.Context(), backend, testRequest("A"), FailClosed)
		// Assert.
		if unknown && (err == nil || session != nil || backend.releaseCalls != 0) {
			t.Fatal("uncertain reservation masked")
		}
		if !unknown && (err != nil || reservation.Admission != Unreserved || session != nil) {
			t.Fatal("fail-open not explicit")
		}
		if closedErr == nil {
			t.Fatal("fail-closed allowed dispatch")
		}
	}
	// Arrange.
	backend := newAtomicBackend()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	// Act.
	_, _, err := Admit(ctx, backend, testRequest("A"), FailClosed)
	// Assert.
	if !errors.Is(err, context.Canceled) || len(backend.holds) != 0 {
		t.Fatal("cancelled admission invoked backend")
	}
}
