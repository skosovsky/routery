package routery

import "sync"

// Lifetime owns a resource and releases it exactly once, including across result copies.
// Callbacks must not call Close recursively. A nil Lifetime represents a value payload.
type Lifetime struct {
	mu       sync.Mutex
	once     sync.Once
	closed   bool
	cleanup  func() error
	hooks    []func()
	closeErr error
}

// NewLifetime creates explicit ownership for a caller-defined resource.
func NewLifetime(cleanup func() error) *Lifetime {
	return &Lifetime{
		mu: sync.Mutex{}, once: sync.Once{}, closed: false,
		cleanup: cleanup, hooks: nil, closeErr: nil,
	}
}

// OnClose attaches a callback after resource cleanup completes.
// Registrations during cleanup are queued; registrations after cleanup run immediately.
func (life *Lifetime) OnClose(fn func()) {
	if life == nil || fn == nil {
		return
	}
	life.mu.Lock()
	if !life.closed {
		life.hooks = append(life.hooks, fn)
		life.mu.Unlock()
		return
	}
	life.mu.Unlock()
	fn()
}

// Close releases the owned resource and attached callbacks exactly once.
func (life *Lifetime) Close() error {
	if life == nil {
		return nil
	}
	life.once.Do(func() {
		// Mark completion and collect hooks after cleanup, including on panic.
		defer func() {
			life.mu.Lock()
			life.closed = true
			hooks := life.hooks
			life.hooks = nil
			life.mu.Unlock()
			for _, fn := range hooks {
				fn()
			}
		}()
		if life.cleanup != nil {
			life.closeErr = life.cleanup()
		}
	})
	return life.closeErr
}
