// Package stream binds caller-owned lazy iterators to routing resource lifetimes.
package stream

import (
	"errors"
	"iter"
	"sync"

	"github.com/skosovsky/routery"
)

// ErrConsumed reports a second consumption or a start after cancellation.
var ErrConsumed = errors.New(
	"routery/stream: iterator already consumed or closed",
)

// ErrInvalidPorts reports a missing ownership operation.
var ErrInvalidPorts = errors.New(
	"routery/stream: events, cancel and discard are required",
)

// Owner owns single consumption and completed resource cleanup. Do not copy it.
// Close must not be called from the iterator, its callbacks, ports or lifetime hooks.
type Owner[Event any] struct {
	mu         sync.Mutex
	started    bool
	stopped    bool
	events     iter.Seq2[Event, error]
	cancel     func() error
	discard    func() error
	cancelOnce sync.Once
	cancelErr  error
	discardErr error
	done       chan struct{}
	life       *routery.Lifetime
}

// New transfers exclusive ownership of events to a new owner.
// cancel must request cancellation without waiting or reentering the owner.
// discard synchronously cleans an unused resource without starting consumption.
// All ports must be non-nil; cancel and discard must not panic.
func New[Event any](events iter.Seq2[Event, error], cancel, discard func() error) (*Owner[Event], error) {
	if events == nil || cancel == nil || discard == nil {
		return nil, ErrInvalidPorts
	}
	owner := &Owner[Event]{mu: sync.Mutex{}, started: false, stopped: false, events: events,
		cancel: cancel, discard: discard, cancelOnce: sync.Once{}, cancelErr: nil, discardErr: nil,
		done: make(chan struct{}), life: nil}
	owner.life = routery.NewLifetime(owner.cleanup)
	return owner, nil
}

// Lifetime is the canonical result owner. Attach it even to partial error results.
func (owner *Owner[Event]) Lifetime() *routery.Lifetime { return owner.life }

// Done signals actual resource cleanup, before lifetime hooks finish.
func (owner *Owner[Event]) Done() <-chan struct{} { return owner.done }

// Cancel stops a created owner or requests cancellation of a running iterator.
// It is safe in event callbacks; it does not wait for active consumption.
func (owner *Owner[Event]) Cancel() {
	owner.mu.Lock()
	if owner.stopped {
		owner.mu.Unlock()
		return
	}
	owner.stopped = true
	unused := !owner.started
	owner.mu.Unlock()
	owner.requestCancel()
	if unused {
		owner.discardErr = owner.discard()
		close(owner.done)
	}
}

// Close requests cancellation and waits for source cleanup and lifetime hooks.
// Call outside the consumption stack; Cancel is the callback-safe operation.
func (owner *Owner[Event]) Close() error { return owner.life.Close() }

// Events consumes once and closes ownership after the iterator actually unwinds.
// Consumption errors are yielded unchanged and are distinct from cleanup errors.
func (owner *Owner[Event]) Events() iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		owner.mu.Lock()
		if owner.started || owner.stopped {
			owner.mu.Unlock()
			var zero Event
			yield(zero, ErrConsumed)
			return
		}
		owner.started = true
		owner.mu.Unlock()
		defer owner.finish()
		owner.events(yield)
	}
}

func (owner *Owner[Event]) requestCancel() {
	owner.cancelOnce.Do(func() { owner.cancelErr = owner.cancel() })
}

func (owner *Owner[Event]) finish() {
	owner.mu.Lock()
	owner.stopped = true
	owner.mu.Unlock()
	owner.requestCancel()
	close(owner.done)
	// cleanup errors remain available from Close; iteration errors are not replaced.
	_ = owner.life.Close()
}

func (owner *Owner[Event]) cleanup() error {
	owner.Cancel()
	<-owner.done
	return errors.Join(owner.cancelErr, owner.discardErr)
}
