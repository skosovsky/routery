package quota

import (
	"errors"
	"sync"
	"testing"
)

func TestIndependentSessionsRejectConcurrentFinalization(t *testing.T) {
	// Arrange: duplicate admission creates one durable hold but two local views.
	backend := newAtomicBackend()
	reservation, commitSession, err := Admit(t.Context(), backend, testRequest("A"), FailClosed)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, releaseSession, err := Admit(t.Context(), backend, testRequest("A"), FailClosed)
	if err != nil || duplicate.Handle != reservation.Handle || len(backend.holds) != 1 {
		t.Fatal("duplicate reserve created a different hold")
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var group sync.WaitGroup
	// Act: the backend, not a shared Session mutex, arbitrates competing clients.
	group.Go(func() {
		<-start
		results <- commitSession.Settle(t.Context(), "commit", map[string]uint64{"units": 4}, true)
	})
	group.Go(func() {
		<-start
		results <- releaseSession.Release(t.Context(), ReleaseProof[string, string]{
			Handle: reservation.Handle, ID: "release", NotExecuted: true,
		})
	})
	close(start)
	group.Wait()
	close(results)
	// Assert: exactly one finalization succeeds; the conflict is explicit.
	successes, conflicts := 0, 0
	for result := range results {
		switch {
		case result == nil:
			successes++
		case errors.Is(result, ErrConflict):
			conflicts++
		default:
			t.Fatal(result)
		}
	}
	if successes != 1 || conflicts != 1 || len(backend.holds) != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
	hold := backend.holds[reservation.Handle]
	if hold.state == Committed {
		if hold.settlement.Actual["units"] != 4 || backend.remaining != 0 {
			t.Fatal("commit overage lost or release also applied")
		}
	} else if hold.state != Released || backend.remaining != 1 {
		t.Fatal("release was not exclusive")
	}
}

func TestExpiredHandleNeverBecomesZeroUsage(t *testing.T) {
	for _, name := range []string{"complete", "incomplete", "release"} {
		t.Run(name, func(t *testing.T) {
			// Arrange: host TTL removed the handle, without proof of absent provider usage.
			backend := newAtomicBackend()
			reservation, session, err := Admit(t.Context(), backend, testRequest("A"), FailClosed)
			if err != nil {
				t.Fatal(err)
			}
			delete(backend.holds, reservation.Handle)
			// Act.
			if name == "release" {
				err = session.Release(t.Context(), ReleaseProof[string, string]{
					Handle: reservation.Handle, ID: "release", NotExecuted: true,
				})
			} else {
				err = session.Settle(t.Context(), "settlement", map[string]uint64{"units": 4}, name == "complete")
			}
			// Assert: the error and pending uncertainty survive; no automatic refund.
			if !errors.Is(err, ErrUnknownHandle) || session.State() != Pending || backend.remaining != 0 {
				t.Fatalf("err=%v state=%v remaining=%d", err, session.State(), backend.remaining)
			}
		})
	}
}

func TestDuplicateReserveRejectsChangedFacts(t *testing.T) {
	for _, name := range []string{"scope", "policy", "estimate"} {
		t.Run(name, func(t *testing.T) {
			// Arrange.
			backend := newAtomicBackend()
			request := testRequest("A")
			first, _, err := Admit(t.Context(), backend, request, FailClosed)
			if err != nil {
				t.Fatal(err)
			}
			switch name {
			case "scope":
				request.Scope = "other"
			case "policy":
				request.Fingerprint = "changed"
			case "estimate":
				request.Estimated["units"] = 2
			}
			// Act.
			_, _, err = Admit(t.Context(), backend, request, FailClosed)
			// Assert: mismatched idempotent requests cannot silently overwrite a hold.
			if !errors.Is(err, ErrConflict) || len(backend.holds) != 1 || backend.remaining != 0 ||
				backend.holds[first.Handle].request.Estimated["units"] != 1 {
				t.Fatalf("err=%v remaining=%d", err, backend.remaining)
			}
		})
	}
}
