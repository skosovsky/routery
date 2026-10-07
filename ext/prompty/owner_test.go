package routeryprompty_test

import (
	"context"
	"errors"
	"iter"
	"sync/atomic"
	"testing"
	"time"

	"github.com/skosovsky/prompty"

	"github.com/skosovsky/routery"
	routeryprompty "github.com/skosovsky/routery/ext/prompty"
	"github.com/skosovsky/routery/stream"
	"github.com/skosovsky/routery/stream/streamtest"
)

type testHandle struct {
	owner *stream.Owner[*prompty.ResponseChunk]
}

func (handle testHandle) Cancel()                     { handle.owner.Cancel() }
func (handle testHandle) Close() error                { return handle.owner.Close() }
func (handle testHandle) Lifetime() *routery.Lifetime { return handle.owner.Lifetime() }
func (handle testHandle) Done() <-chan struct{}       { return handle.owner.Done() }
func (handle testHandle) Events() iter.Seq2[int, error] {
	return func(yield func(int, error) bool) {
		for chunk, err := range handle.owner.Events() {
			event := 0
			if chunk != nil {
				switch chunk.Kind {
				case prompty.StreamStart:
					event = 1
				case prompty.StreamFinish:
					event = 2
				case prompty.StreamTerminal:
					event = 3
				default:
				}
			}
			if !yield(event, err) {
				return
			}
		}
	}
}

func TestRealStreamConformance(t *testing.T) {
	streamtest.Run(t, func(ctx context.Context, source func(context.Context) iter.Seq2[int, error]) streamtest.Handle {
		return realHandle(ctx, t, source)
	})
}

func realHandle(
	ctx context.Context,
	t *testing.T,
	source func(context.Context) iter.Seq2[int, error],
) streamtest.Handle {
	t.Helper()
	raw := prompty.NewStream(
		ctx,
		prompty.StreamNative,
		func(ctx context.Context) iter.Seq2[*prompty.ResponseChunk, error] {
			return func(yield func(*prompty.ResponseChunk, error) bool) {
				for event, err := range source(ctx) {
					var chunk *prompty.ResponseChunk
					if err == nil {
						chunk = &prompty.ResponseChunk{Kind: prompty.StreamStart}
						if event == 2 {
							chunk.Kind = prompty.StreamFinish
							chunk.Outcome = prompty.OutcomeCompleted
						}
					}
					if !yield(chunk, err) {
						return
					}
				}
			}
		},
	)
	owner, err := routeryprompty.New(raw)
	if err != nil {
		t.Fatal(err)
	}
	return testHandle{owner: owner}
}

func TestRealTerminalBeforeLifecycleCloseout(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(
			map[bool]string{false: "bulk_outer", true: "timeout_outer"}[reverse],
			func(t *testing.T) { realTerminalCloseout(t, reverse) },
		)
	}
}

func realTerminalCloseout(t *testing.T, reverse bool) {
	t.Helper()
	// Arrange: the source emits finish; terminal delivery precedes lifecycle closeout.
	gate, entered, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	raw := prompty.NewStream(
		t.Context(),
		prompty.StreamNative,
		func(context.Context) iter.Seq2[*prompty.ResponseChunk, error] {
			return func(yield func(*prompty.ResponseChunk, error) bool) {
				yield(&prompty.ResponseChunk{Kind: prompty.StreamFinish, Outcome: prompty.OutcomeCompleted}, nil)
			}
		},
	)
	if err := raw.ObserveLifecycle(func(ctx context.Context) (context.Context, func(prompty.StreamStatus)) {
		return ctx, func(prompty.StreamStatus) { close(entered); <-gate }
	}); err != nil {
		t.Fatal(err)
	}
	owner, err := routeryprompty.New(raw)
	if err != nil {
		t.Fatal(err)
	}
	var terminals, hooks atomic.Int32
	owner.Lifetime().OnClose(func() { hooks.Add(1) })
	base := func(routery.RouteCall[int]) (routery.BasicRouteResult[int], error) {
		result := routery.BasicHandled(1)
		result.Lifetime = owner.Lifetime()
		return result, nil
	}
	bulk := routery.Bulkhead[int, routery.BasicKind, routery.BasicReason, int](1)
	timeout := routery.Timeout[int, routery.BasicKind, routery.BasicReason, int](time.Hour)
	handler := routery.ApplyRoute(base, bulk, timeout)
	if reverse {
		handler = routery.ApplyRoute(base, timeout, bulk)
	}
	_, err = handler(routery.NewRouteCall(t.Context(), 0))
	if err != nil {
		t.Fatal(err)
	}
	// Act.
	go func() {
		defer close(done)
		for chunk := range owner.Events() {
			if chunk != nil && chunk.Kind == prompty.StreamTerminal {
				terminals.Add(1)
			}
		}
	}()
	select {
	case <-entered:
	case <-time.After(time.Second * 10):
		t.Fatal("closeout not reached")
	}
	_, blocked := handler(routery.NewRouteCall(t.Context(), 0))
	// Assert: even terminal delivery cannot free a still-running iterator.
	if terminals.Load() != 1 || hooks.Load() != 0 || !errors.Is(blocked, routery.ErrBulkheadFull) {
		t.Fatal(terminals.Load(), hooks.Load(), blocked)
	}
	close(gate)
	<-done
	if err = owner.Close(); err != nil || hooks.Load() != 1 {
		t.Fatal(err, hooks.Load())
	}
}

func TestRealBareAndCancelOnlyNegative(t *testing.T) {
	// Arrange: bare handles intentionally violate routing ownership.
	rawFactory := func(ctx context.Context) *prompty.Stream {
		return prompty.ErrorStream(ctx, prompty.StreamNative, errors.New("source"))
	}
	bare := func(call routery.RouteCall[int]) (routery.BasicRouteResult[*prompty.Stream], error) {
		return routery.BasicHandled(rawFactory(call.Context)), nil
	}
	handler := routery.Timeout[int, routery.BasicKind, routery.BasicReason, *prompty.Stream](time.Hour)(bare)
	// Act.
	result, err := handler(routery.NewRouteCall(t.Context(), 0))
	if err != nil {
		t.Fatal(err)
	}
	var observed error
	for _, eventErr := range result.Payload.Events() {
		observed = errors.Join(observed, eventErr)
	}
	// Assert.
	if !errors.Is(observed, context.Canceled) {
		t.Fatal("bare handle remained live", observed)
	}
	_ = result.Payload.Close()
	// Arrange: source cleanup remains gated after cancel-only Close returns.
	gate, entered, started, done := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	raw := prompty.NewStream(
		t.Context(),
		prompty.StreamNative,
		func(ctx context.Context) iter.Seq2[*prompty.ResponseChunk, error] {
			return func(func(*prompty.ResponseChunk, error) bool) {
				defer func() { close(entered); <-gate }()
				close(started)
				<-ctx.Done()
			}
		},
	)
	wrong := routery.Bulkhead[int, routery.BasicKind, routery.BasicReason, int](
		1,
	)(
		func(routery.RouteCall[int]) (routery.BasicRouteResult[int], error) {
			r := routery.BasicHandled(1)
			r.Lifetime = routery.NewLifetime(raw.Close)
			return r, nil
		},
	)
	first, _ := wrong(routery.NewRouteCall(t.Context(), 0))
	go func() {
		defer close(done)
		for range raw.Events() {
		}
	}()
	<-started
	// Act.
	_ = first.Lifetime.Close()
	<-entered
	second, secondErr := wrong(routery.NewRouteCall(t.Context(), 0))
	// Assert: wrong glue releases the permit before source cleanup.
	if secondErr != nil {
		t.Fatal("negative not reproduced", secondErr)
	}
	close(gate)
	<-done
	_ = second.Lifetime.Close()
}

func TestNilSource(t *testing.T) {
	// Arrange / Act.
	owner, err := routeryprompty.New(nil)
	// Assert.
	if owner != nil || err == nil {
		t.Fatal(owner, err)
	}
}

func TestRejectUsedSource(t *testing.T) {
	// Arrange.
	raw := prompty.ErrorStream(t.Context(), prompty.StreamNative, errors.New("failed"))
	for range raw.Events() {
	}
	// Act.
	owner, err := routeryprompty.New(raw)
	// Assert.
	if owner != nil || !errors.Is(err, prompty.ErrStreamConsumed) {
		t.Fatal(owner, err)
	}
}
