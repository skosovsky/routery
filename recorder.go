package routery

import "sync"

const (
	reasonAsyncAccepted = "async_accepted"
	reasonNoMatch       = "no_match"
)

// RouteAction describes control flow after a handler invocation.
type RouteAction string

const (
	// ActionNext continues to the next route in the table.
	ActionNext RouteAction = "next"
	// ActionStop terminates routing with an outcome recorded in ResultRecorder.
	ActionStop RouteAction = "stop"
	// ActionAbort indicates a system failure reported by the engine when a handler returns an error.
	// It is not stored in ResultRecorder; abort is signaled via the handler error return value.
	ActionAbort RouteAction = "abort"
)

// ResultRecorder records typed route outcomes without storing data in [context.WithValue].
type ResultRecorder[Res any] interface {
	Stop(payload Res, reasonCode string)
	Next(reasonCode string)
	Ignore(reasonCode string)
	Async(payload Res, reasonCode string)

	Action() RouteAction
	ReasonCode() string
	Payload() (Res, bool)
}

// NewResultRecorder returns a lock-free result recorder for a single dispatch goroutine.
func NewResultRecorder[Res any]() ResultRecorder[Res] {
	//nolint:exhaustruct // zero values are the initial non-terminal state.
	return &resultRecorder[Res]{
		action: ActionNext,
	}
}

type resultRecorder[Res any] struct {
	action     RouteAction
	reasonCode string
	payload    Res
	hasPayload bool
	terminal   bool
}

func (rec *resultRecorder[Res]) Stop(payload Res, reasonCode string) {
	if rec.terminal {
		return
	}

	rec.action = ActionStop
	rec.reasonCode = reasonCode
	rec.payload = payload
	rec.hasPayload = true
	rec.terminal = true
}

func (rec *resultRecorder[Res]) Next(reasonCode string) {
	if rec.terminal {
		return
	}

	rec.action = ActionNext
	rec.reasonCode = reasonCode
}

func (rec *resultRecorder[Res]) Ignore(reasonCode string) {
	if rec.terminal {
		return
	}

	rec.action = ActionStop
	rec.reasonCode = reasonCode
	rec.hasPayload = false
	rec.terminal = true
}

func (rec *resultRecorder[Res]) Async(payload Res, reasonCode string) {
	if rec.terminal {
		return
	}

	if reasonCode == "" {
		reasonCode = reasonAsyncAccepted
	}

	rec.action = ActionStop
	rec.reasonCode = reasonCode
	rec.payload = payload
	rec.hasPayload = true
	rec.terminal = true
}

func (rec *resultRecorder[Res]) Action() RouteAction {
	if rec.terminal {
		return ActionStop
	}

	return rec.action
}

func (rec *resultRecorder[Res]) ReasonCode() string {
	return rec.reasonCode
}

func (rec *resultRecorder[Res]) Payload() (Res, bool) {
	return rec.payload, rec.hasPayload
}

// threadSafeRecorder serializes writes to a shared recorder for parallel primitives.
type threadSafeRecorder[Res any] struct {
	mu  sync.Mutex
	rec ResultRecorder[Res]
}

func newThreadSafeRecorder[Res any](rec ResultRecorder[Res]) ResultRecorder[Res] {
	//nolint:exhaustruct // mu is zero-initialized.
	return &threadSafeRecorder[Res]{rec: rec}
}

func (rec *threadSafeRecorder[Res]) Stop(payload Res, reasonCode string) {
	rec.mu.Lock()
	defer rec.mu.Unlock()

	if rec.rec.Action() == ActionStop {
		return
	}

	rec.rec.Stop(payload, reasonCode)
}

func (rec *threadSafeRecorder[Res]) Next(reasonCode string) {
	rec.mu.Lock()
	defer rec.mu.Unlock()

	if rec.rec.Action() == ActionStop {
		return
	}

	rec.rec.Next(reasonCode)
}

func (rec *threadSafeRecorder[Res]) Ignore(reasonCode string) {
	rec.mu.Lock()
	defer rec.mu.Unlock()

	if rec.rec.Action() == ActionStop {
		return
	}

	rec.rec.Ignore(reasonCode)
}

func (rec *threadSafeRecorder[Res]) Async(payload Res, reasonCode string) {
	rec.mu.Lock()
	defer rec.mu.Unlock()

	if rec.rec.Action() == ActionStop {
		return
	}

	rec.rec.Async(payload, reasonCode)
}

func (rec *threadSafeRecorder[Res]) Action() RouteAction {
	rec.mu.Lock()
	defer rec.mu.Unlock()

	return rec.rec.Action()
}

func (rec *threadSafeRecorder[Res]) ReasonCode() string {
	rec.mu.Lock()
	defer rec.mu.Unlock()

	return rec.rec.ReasonCode()
}

func (rec *threadSafeRecorder[Res]) Payload() (Res, bool) {
	rec.mu.Lock()
	defer rec.mu.Unlock()

	return rec.rec.Payload()
}

// CopyRecorderOutcome copies disposition and payload from src into dest.
// Unknown or initial src states are no-ops; ActionAbort is not stored in recorders.
func CopyRecorderOutcome[Res any](dest, src ResultRecorder[Res]) {
	switch src.Action() {
	case ActionStop:
		if payload, ok := src.Payload(); ok {
			dest.Stop(payload, src.ReasonCode())
		} else {
			dest.Ignore(src.ReasonCode())
		}
	case ActionNext:
		dest.Next(src.ReasonCode())
	default:
	}
}
