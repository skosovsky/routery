// Package streamtest provides semantic ownership conformance for stream bridges.
package streamtest

import (
	"context"
	"errors"
	"iter"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/skosovsky/routery"
)

// Handle is the ownership contract under test. Events use 1 for start, 2 for finish.
type Handle interface {
	Events() iter.Seq2[int, error]
	Cancel()
	Close() error
	Lifetime() *routery.Lifetime
	Done() <-chan struct{}
}

// Factory creates a lazy handle. The source owns cleanup before iterator return.
type Factory func(context.Context, func(context.Context) iter.Seq2[int, error]) Handle

type contextKey struct{}

const (
	workerTimeout  = 10 * time.Second
	concurrentMode = "concurrent"
	errorMode      = "error"
	deadlineMode   = "deadline"
	parentMode     = "parent"
	unusedMode     = "unused"
)

// Run checks resource ownership using deterministic barriers and real routing.
func Run(t *testing.T, factory Factory) {
	t.Helper()
	t.Run("gated_cleanup", func(t *testing.T) { gated(t, factory) })
	t.Run("terminal_is_not_completion", func(t *testing.T) { terminal(t, factory) })
	t.Run("paths", func(t *testing.T) { paths(t, factory) })
	t.Run("start_close_race", func(t *testing.T) { startRace(t, factory) })
	t.Run("partial_error_and_retry", func(t *testing.T) { partial(t, factory) })
	t.Run("consumption_error_no_replay", func(t *testing.T) { noReplay(t, factory) })
	t.Run("timed_active_cancellation", func(t *testing.T) { timedCancellation(t, factory) })
	t.Run("context_request_metadata", func(t *testing.T) { contextFacts(t, factory) })
}

func wait(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(workerTimeout):
		t.Fatal("worker did not finish")
	}
}

func drain(handle Handle) {
	for range handle.Events() {
	}
}

func middleware(reverse bool) []routery.RouteMiddleware[int, routery.BasicKind, routery.BasicReason, Handle] {
	bulk := routery.Bulkhead[int, routery.BasicKind, routery.BasicReason, Handle](1)
	timeout := routery.Timeout[int, routery.BasicKind, routery.BasicReason, Handle](time.Hour)
	if reverse {
		return []routery.RouteMiddleware[int, routery.BasicKind, routery.BasicReason, Handle]{timeout, bulk}
	}
	return []routery.RouteMiddleware[int, routery.BasicKind, routery.BasicReason, Handle]{bulk, timeout}
}

//nolint:gocognit // Conformance matrix keeps arrange, act and lifecycle assertions together.
func gated(t *testing.T, factory Factory) {
	for _, reverse := range []bool{false, true} {
		t.Run(map[bool]string{false: "bulk_outer", true: "timeout_outer"}[reverse], func(t *testing.T) {
			// Arrange: source cleanup deliberately cannot finish yet.
			gate, entered, started := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var calls, hooks atomic.Int32
			base := func(call routery.RouteCall[int]) (routery.BasicRouteResult[Handle], error) {
				index := calls.Add(1)
				handle := factory(call.Context, func(ctx context.Context) iter.Seq2[int, error] {
					return func(yield func(int, error) bool) {
						if index == 1 {
							defer func() { close(entered); <-gate }()
							close(started)
						}
						if !yield(1, nil) {
							return
						}
						<-ctx.Done()
					}
				})
				handle.Lifetime().OnClose(func() { hooks.Add(1) })
				result := routery.BasicHandled(handle)
				result.Lifetime = handle.Lifetime()
				return result, nil
			}
			handler := routery.ApplyRoute(base, middleware(reverse)...)
			first, err := handler(routery.NewRouteCall(t.Context(), 0))
			if err != nil {
				t.Fatal(err)
			}
			consumer := make(chan struct{})
			go func() { defer close(consumer); drain(first.Payload) }()
			wait(t, started)
			// Act: callback-safe cancellation does not prove completed cleanup.
			first.Payload.Cancel()
			wait(t, entered)
			_, blocked := handler(routery.NewRouteCall(t.Context(), 0))
			// Assert: the permit stays held until the source actually unwinds.
			if !errors.Is(blocked, routery.ErrBulkheadFull) || hooks.Load() != 0 {
				t.Fatal("early release", blocked)
			}
			close(gate)
			wait(t, consumer)
			if err = first.Payload.Close(); err != nil || hooks.Load() != 1 {
				t.Fatal(err, hooks.Load())
			}
			next, err := handler(routery.NewRouteCall(t.Context(), 0))
			if err != nil {
				t.Fatal(err)
			}
			if err = next.Payload.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func terminal(t *testing.T, factory Factory) {
	// Arrange: hold the consumer callback after the last event.
	gate, seen, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	handle := factory(t.Context(), func(context.Context) iter.Seq2[int, error] {
		return func(yield func(int, error) bool) {
			if yield(1, nil) {
				yield(2, nil)
			}
		}
	})
	var hooks atomic.Int32
	handle.Lifetime().OnClose(func() { hooks.Add(1) })
	// Act.
	go func() {
		defer close(done)
		for event := range handle.Events() {
			if event >= 2 {
				close(seen)
				<-gate
				break
			}
		}
	}()
	wait(t, seen)
	// Assert: source return, rather than frame delivery, releases ownership.
	select {
	case <-handle.Done():
		t.Fatal("terminal frame counted as completion")
	default:
	}
	if hooks.Load() != 0 {
		t.Fatal("early hook")
	}
	close(gate)
	wait(t, done)
	if err := handle.Close(); err != nil || hooks.Load() != 1 {
		t.Fatal(err, hooks.Load())
	}
}

func paths(t *testing.T, factory Factory) {
	for _, mode := range []string{"eof", errorMode, unusedMode, "break", "callback", parentMode, deadlineMode, concurrentMode} {
		t.Run(mode, func(t *testing.T) { pathCase(t, factory, mode) })
	}
}

//nolint:gocognit // The conformance fixture keeps each termination mode and its ownership assertions together.
func pathCase(t *testing.T, factory Factory, mode string) {
	t.Helper()
	// Arrange.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var expired context.CancelFunc
	if mode == deadlineMode {
		ctx, expired = context.WithDeadline(ctx, time.Now().Add(-time.Second))
		defer expired()
	}
	var starts, cleanups atomic.Int32
	active := make(chan struct{})
	sourceErr := errors.New("source failure")
	handle := factory(ctx, func(ctx context.Context) iter.Seq2[int, error] {
		return func(yield func(int, error) bool) {
			starts.Add(1)
			close(active)
			defer cleanups.Add(1)
			if err := ctx.Err(); err != nil {
				yield(0, err)
				return
			}
			if !yield(1, nil) {
				return
			}
			if mode == errorMode {
				yield(0, sourceErr)
				return
			}
			if mode == concurrentMode {
				<-ctx.Done()
				return
			}
			if err := ctx.Err(); err != nil {
				yield(0, err)
				return
			}
			yield(2, nil)
		}
	})
	var hooks atomic.Int32
	handle.Lifetime().OnClose(func() { hooks.Add(1) })
	handler := permitProbe(handle)
	if _, err := handler(routery.NewRouteCall(t.Context(), 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := handler(routery.NewRouteCall(t.Context(), 0)); !errors.Is(err, routery.ErrBulkheadFull) {
		t.Fatal("initial permit missing", err)
	}
	// Act.
	var observed error
	if mode == unusedMode {
		handle.Cancel()
	} else {
		var group sync.WaitGroup
		if mode == concurrentMode {
			for range 20 {
				group.Go(func() { <-active; _ = handle.Close() })
			}
		}

		for _, eventErr := range handle.Events() {
			observed = errors.Join(observed, eventErr)
			switch mode {
			case "break":
				goto finished
			case "callback":
				handle.Cancel()
			case parentMode:
				cancel()
			}
		}
	finished:
		group.Wait()
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	assertTermination(t, mode, observed, sourceErr, starts.Load(), cleanups.Load(), hooks.Load())
	wait(t, handle.Done())
	if _, err := handler(routery.NewRouteCall(t.Context(), 0)); err != nil {
		t.Fatal("permit leaked", err)
	}
	var repeated error
	for _, err := range handle.Events() {
		repeated = err
	}
	if repeated == nil {
		t.Fatal("second consumption accepted")
	}
}

func assertTermination(t *testing.T, mode string, observed, sourceErr error, starts, cleanups, hooks int32) {
	t.Helper()
	// Assert.
	if mode == errorMode && !errors.Is(observed, sourceErr) {
		t.Fatal("source error lost", observed)
	}
	if mode == parentMode && !errors.Is(observed, context.Canceled) {
		t.Fatal("parent cancellation lost", observed)
	}
	if mode == deadlineMode && !errors.Is(observed, context.DeadlineExceeded) {
		t.Fatal("deadline lost", observed)
	}
	if hooks != 1 || starts != cleanups {
		t.Fatal(starts, cleanups, hooks)
	}
	if mode == unusedMode && starts != 0 {
		t.Fatal("unused source started")
	}
}

func startRace(t *testing.T, factory Factory) {
	for range 100 {
		// Arrange.
		var starts, cleanups atomic.Int32
		handle := factory(t.Context(), func(ctx context.Context) iter.Seq2[int, error] {
			return func(yield func(int, error) bool) {
				starts.Add(1)
				defer cleanups.Add(1)
				if yield(1, nil) {
					if err := ctx.Err(); err != nil {
						yield(0, err)
						return
					}
					yield(2, nil)
				}
			}
		})
		handler := permitProbe(handle)
		if _, err := handler(routery.NewRouteCall(t.Context(), 0)); err != nil {
			t.Fatal(err)
		}
		gate := make(chan struct{})
		var group sync.WaitGroup
		// Act.
		group.Go(func() { <-gate; drain(handle) })
		group.Go(func() { <-gate; _ = handle.Close() })
		group.Go(func() { <-gate; handle.Cancel() })
		close(gate)
		group.Wait()
		// Assert.
		if starts.Load() > 1 || starts.Load() != cleanups.Load() {
			t.Fatal("unbalanced source")
		}
		if err := handle.Close(); err != nil {
			t.Fatal(err)
		}
		wait(t, handle.Done())
		if _, err := handler(routery.NewRouteCall(t.Context(), 0)); err != nil {
			t.Fatal("race leaked permit", err)
		}
	}
}

func partial(t *testing.T, factory Factory) {
	// Arrange: a partial result must survive final error; retry disposes intermediates.
	failure := errors.New("handler failed with partial handle")
	var calls, closes atomic.Int32
	base := func(call routery.RouteCall[int]) (routery.BasicRouteResult[Handle], error) {
		if calls.Load() != closes.Load() {
			t.Error("retry before cleanup")
		}
		calls.Add(1)
		handle := factory(call.Context, func(ctx context.Context) iter.Seq2[int, error] {
			return func(yield func(int, error) bool) {
				if yield(1, nil) {
					if err := ctx.Err(); err != nil {
						yield(0, err)
						return
					}
					yield(2, nil)
				}
			}
		})
		handle.Lifetime().OnClose(func() { closes.Add(1) })
		result := routery.BasicHandled(handle)
		result.Lifetime = handle.Lifetime()
		return result, failure
	}
	retry := routery.RetryIf[int, routery.BasicKind, routery.BasicReason, Handle](
		2,
		0,
		func(context.Context, int, error) bool { return true },
	)(
		base,
	)
	// Act.
	result, err := retry(routery.NewRouteCall(t.Context(), 0))
	// Assert.
	if !errors.Is(err, failure) || calls.Load() != 2 || closes.Load() != 1 || result.Lifetime == nil ||
		!result.HasPayload {
		t.Fatal(err, calls.Load(), closes.Load())
	}
	if err = result.Lifetime.Close(); err != nil || closes.Load() != 2 {
		t.Fatal(err, closes.Load())
	}
	// Act: fallback owns discarded primary, final partial remains caller-owned.
	calls.Store(0)
	closes.Store(0)
	result, err = routery.Fallback(base, base)(routery.NewRouteCall(t.Context(), 0))
	// Assert.
	if !errors.Is(err, failure) || calls.Load() != 2 || closes.Load() != 1 || result.Lifetime == nil {
		t.Fatal(err)
	}
	_ = result.Lifetime.Close()
	if closes.Load() != 2 {
		t.Fatal("fallback lost final owner")
	}
}

func noReplay(t *testing.T, factory Factory) {
	// Arrange.
	failure := errors.New("consumption failure")
	var calls, fallbacks atomic.Int32
	base := func(call routery.RouteCall[int]) (routery.BasicRouteResult[Handle], error) {
		calls.Add(1)
		handle := factory(call.Context, func(context.Context) iter.Seq2[int, error] {
			return func(yield func(int, error) bool) { yield(0, failure) }
		})
		result := routery.BasicHandled(handle)
		result.Lifetime = handle.Lifetime()
		return result, nil
	}
	fallback := func(routery.RouteCall[int]) (routery.BasicRouteResult[Handle], error) {
		fallbacks.Add(1)
		return routery.AbortResult[routery.BasicKind, routery.BasicReason, Handle](), failure
	}
	retry := routery.RetryIf[int, routery.BasicKind, routery.BasicReason, Handle](
		2,
		0,
		func(context.Context, int, error) bool { return true },
	)(
		base,
	)
	// Act.
	result, err := routery.Fallback(retry, fallback)(routery.NewRouteCall(t.Context(), 0))
	if err != nil {
		t.Fatal(err)
	}
	var observed error
	for _, eventErr := range result.Payload.Events() {
		observed = errors.Join(observed, eventErr)
	}
	// Assert.
	if !errors.Is(observed, failure) || calls.Load() != 1 || fallbacks.Load() != 0 {
		t.Fatal(observed, calls.Load(), fallbacks.Load())
	}
	_ = result.Lifetime.Close()
}

func contextFacts(t *testing.T, factory Factory) {
	// Arrange.
	type request struct{ Required string }
	ctx := context.WithValue(t.Context(), contextKey{}, "preserved")
	contexts := make(chan context.Context, 1)
	base := func(call routery.RouteCall[request]) (routery.BasicRouteResult[Handle], error) {
		if call.Request.Required != "required-capability" || call.Match.RouteID != "chosen" {
			t.Error("request/match lost")
		}
		contexts <- call.Context
		handle := factory(call.Context, func(ctx context.Context) iter.Seq2[int, error] {
			return func(yield func(int, error) bool) {
				if ctx.Value(contextKey{}) != "preserved" {
					t.Error("context value lost")
				}
				if yield(1, nil) {
					if err := ctx.Err(); err != nil {
						yield(0, err)
						return
					}
					yield(2, nil)
				}
			}
		})
		result := routery.BasicHandled(handle)
		result.Lifetime = handle.Lifetime()
		return result, nil
	}
	handler := routery.Timeout[request, routery.BasicKind, routery.BasicReason, Handle](time.Hour)(base)
	call := routery.NewRouteCall(ctx, request{Required: "required-capability"})
	call.Match.RouteID = "chosen"
	// Act.
	result, err := handler(call)
	captured := <-contexts
	// Assert.
	if err != nil || captured.Err() != nil {
		t.Fatal("context canceled on handle return", err)
	}
	if _, ok := captured.Deadline(); !ok {
		t.Fatal("deadline lost")
	}
	drain(result.Payload)
	_ = result.Lifetime.Close()
	if !errors.Is(captured.Err(), context.Canceled) {
		t.Fatal("timeout context retained after cleanup")
	}
}

func timedCancellation(t *testing.T, factory Factory) {
	for _, mode := range []string{parentMode, deadlineMode} {
		t.Run(mode, func(t *testing.T) { timedCancellationCase(t, factory, mode) })
	}
}

func timedCancellationCase(t *testing.T, factory Factory, mode string) {
	t.Helper()
	// Arrange: Timeout must retain the earlier parent deadline for a running source.
	original := time.Now().Add(time.Second)
	ctx, cancel := context.WithDeadline(t.Context(), original)
	defer cancel()
	captured := make(chan context.Context, 1)
	base := func(call routery.RouteCall[int]) (routery.BasicRouteResult[Handle], error) {
		handle := factory(call.Context, func(ctx context.Context) iter.Seq2[int, error] {
			return func(yield func(int, error) bool) {
				captured <- ctx
				if !yield(1, nil) {
					return
				}
				<-ctx.Done()
				yield(0, ctx.Err())
			}
		})
		result := routery.BasicHandled(handle)
		result.Lifetime = handle.Lifetime()
		return result, nil
	}
	handler := routery.ApplyRoute(
		base,
		routery.Bulkhead[int, routery.BasicKind, routery.BasicReason, Handle](1),
		routery.Timeout[int, routery.BasicKind, routery.BasicReason, Handle](time.Hour),
	)
	result, err := handler(routery.NewRouteCall(ctx, 0))
	if err != nil {
		t.Fatal(err)
	}
	completed := make(chan struct{})
	failures := make(chan error, 1)
	// Act.
	go func() {
		defer close(completed)
		var failure error
		for _, eventErr := range result.Payload.Events() {
			failure = errors.Join(failure, eventErr)
		}
		failures <- failure
	}()
	var sourceCtx context.Context
	select {
	case sourceCtx = <-captured:
	case <-time.After(workerTimeout):
		t.Fatal("source did not start")
	}
	// Assert: the source starts with a live timed context and exact original deadline.
	actual, ok := sourceCtx.Deadline()
	if !ok || !actual.Equal(original) || sourceCtx.Err() != nil {
		t.Fatal("original deadline/context lost", actual, sourceCtx.Err())
	}
	if mode == parentMode {
		cancel()
	}
	wait(t, completed)
	want := context.DeadlineExceeded
	if mode == parentMode {
		want = context.Canceled
	}
	if observed := <-failures; !errors.Is(observed, want) {
		t.Fatal("active cancellation lost", observed)
	}
	if err = result.Lifetime.Close(); err != nil {
		t.Fatal(err)
	}
	probe, probeErr := handler(routery.NewRouteCall(t.Context(), 0))
	if probeErr != nil {
		t.Fatal("active cancellation leaked permit", probeErr)
	}
	if err = probe.Lifetime.Close(); err != nil {
		t.Fatal(err)
	}
}

// permitProbe admits the owned handle once, then permits value-only probes.
func permitProbe(handle Handle) routery.BasicRouteHandler[int, Handle] {
	var admissions atomic.Int32
	return routery.Bulkhead[int, routery.BasicKind, routery.BasicReason, Handle](
		1,
	)(
		func(routery.RouteCall[int]) (routery.BasicRouteResult[Handle], error) {
			if admissions.Add(1) != 1 {
				return routery.BasicHandled[Handle](nil), nil
			}
			result := routery.BasicHandled(handle)
			result.Lifetime = handle.Lifetime()
			return result, nil
		},
	)
}
