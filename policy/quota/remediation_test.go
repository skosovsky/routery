package quota

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type blockingBackend struct {
	*atomicBackend

	started chan struct{}
	proceed chan struct{}
	commits atomic.Int32
	state   func() State
}

func (backend *blockingBackend) Commit(ctx context.Context, settlement Settlement[string, string]) error {
	backend.commits.Add(1)
	close(backend.started)
	<-backend.proceed
	if backend.state != nil {
		_ = backend.state()
	}
	return backend.atomicBackend.Commit(ctx, settlement)
}

func TestSessionCancellationAndStateDuringIO(t *testing.T) {
	// Arrange.
	backend := &blockingBackend{
		atomicBackend: newAtomicBackend(),
		started:       make(chan struct{}),
		proceed:       make(chan struct{}),
	}
	_, session, err := Admit(t.Context(), backend, testRequest("blocked"), FailClosed)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- session.Settle(context.Background(), "settlement", map[string]uint64{"units": 1}, true)
	}()
	<-backend.started
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	defer func() {
		close(backend.proceed)
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	// Act.
	waiters := make(chan error, 2)
	go func() { waiters <- session.Settle(ctx, "settlement", map[string]uint64{"units": 1}, true) }()
	go func() {
		waiters <- session.Release(ctx, ReleaseProof[string, string]{Handle: "blocked", ID: "release", NotExecuted: true})
	}()
	state := make(chan State, 1)
	go func() { state <- session.State() }()
	// Assert.
	for range 2 {
		select {
		case err := <-waiters:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("waiter err=%v", err)
			}
		case <-time.After(time.Second):
			t.Error("cancelled waiter blocked behind backend")
		}
	}
	select {
	case <-state:
	case <-time.After(time.Second):
		t.Error("State blocked behind backend")
	}
}

func TestSessionConcurrentCommitAndBackendStateCallback(t *testing.T) {
	// Arrange.
	backend := &blockingBackend{
		atomicBackend: newAtomicBackend(),
		started:       make(chan struct{}),
		proceed:       make(chan struct{}),
	}
	_, session, err := Admit(t.Context(), backend, testRequest("blocked"), FailClosed)
	if err != nil {
		t.Fatal(err)
	}
	backend.state = session.State
	done := make(chan error, 2)
	// Act.
	for range 2 {
		go func() { done <- session.Settle(t.Context(), "settlement", map[string]uint64{"units": 1}, true) }()
	}
	<-backend.started
	close(backend.proceed)
	// Assert.
	for range 2 {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("backend State callback or concurrent settlement deadlocked")
		}
	}
	if backend.commits.Load() != 1 || session.State() != Committed {
		t.Fatalf("commits=%d state=%v", backend.commits.Load(), session.State())
	}
}
