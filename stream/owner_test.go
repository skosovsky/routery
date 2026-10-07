package stream_test

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"sync/atomic"
	"testing"
	"time"

	"github.com/skosovsky/routery"
	"github.com/skosovsky/routery/stream"
	"github.com/skosovsky/routery/stream/streamtest"
)

func TestConformance(t *testing.T) {
	streamtest.Run(t, func(ctx context.Context, source func(context.Context) iter.Seq2[int, error]) streamtest.Handle {
		child, cancel := context.WithCancel(ctx)
		owner, err := stream.New(source(child), func() error { cancel(); return nil }, func() error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		return owner
	})
}

func TestCleanupErrorsAndUnusedDiscard(t *testing.T) {
	// Arrange.
	cancelErr, discardErr, dispatchErr := errors.New("cancel"), errors.New("discard"), errors.New("dispatch")
	var cancels, discards, starts atomic.Int32
	owner, err := stream.New(
		func(func(int, error) bool) { starts.Add(1) },
		func() error { cancels.Add(1); return cancelErr },
		func() error { discards.Add(1); return discardErr },
	)
	if err != nil {
		t.Fatal(err)
	}
	result := routery.BasicHandled(owner)
	result.Lifetime = owner.Lifetime()
	// Act.
	combined := errors.Join(dispatchErr, result.Lifetime.Close(), owner.Close())
	// Assert.
	if !errors.Is(combined, cancelErr) || !errors.Is(combined, discardErr) || !errors.Is(combined, dispatchErr) ||
		cancels.Load() != 1 ||
		discards.Load() != 1 ||
		starts.Load() != 0 {
		t.Fatal(combined)
	}
}

func TestInvalidPorts(t *testing.T) {
	// Arrange.
	noop := func() error { return nil }
	events := func(func(int, error) bool) {}
	for _, ports := range []struct {
		events          iter.Seq2[int, error]
		cancel, discard func() error
	}{{nil, noop, noop}, {events, nil, noop}, {events, noop, nil}} {
		// Act.
		owner, err := stream.New(ports.events, ports.cancel, ports.discard)
		// Assert.
		if owner != nil || !errors.Is(err, stream.ErrInvalidPorts) {
			t.Fatal(owner, err)
		}
	}
}

func TestSourcePanicUnwindsBeforeRelease(t *testing.T) {
	// Arrange.
	cleaned := false
	owner, err := stream.New(
		func(func(int, error) bool) { defer func() { cleaned = true }(); panic("source") },
		func() error { return nil },
		func() error { return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	owner.Lifetime().OnClose(func() {
		if !cleaned {
			t.Error("hook before source cleanup")
		}
	})
	// Act.
	func() {
		defer func() {
			if recover() != "source" {
				t.Error("source panic lost")
			}
		}()
		for range owner.Events() {
		}
	}()
	// Assert.
	if !cleaned {
		t.Fatal("source not unwound")
	}
	if err = owner.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestIncorrectBareAndCancelOnlyComposition(t *testing.T) {
	// Arrange: these are negative examples, deliberately violating ownership.
	var captured func() error
	bare := func(call routery.RouteCall[int]) (routery.BasicRouteResult[iter.Seq2[int, error]], error) {
		captured = call.Context.Err
		return routery.BasicHandled(
			iter.Seq2[int, error](func(yield func(int, error) bool) { yield(0, call.Context.Err()) }),
		), nil
	}
	timed := routery.Timeout[int, routery.BasicKind, routery.BasicReason, iter.Seq2[int, error]](time.Hour)(bare)
	// Act.
	result, err := timed(routery.NewRouteCall(t.Context(), 0))
	if err != nil {
		t.Fatal(err)
	}
	// Assert: handler return killed the lazy context.
	for _, eventErr := range result.Payload {
		if !errors.Is(eventErr, context.Canceled) {
			t.Fatal(eventErr)
		}
	}
	if captured() == nil {
		t.Fatal("bare negative not reproduced")
	}
	// Arrange: nonblocking Close is not completed cleanup.
	gate, started, entered, done := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	handler := routery.Bulkhead[int, routery.BasicKind, routery.BasicReason, int](
		1,
	)(
		func(routery.RouteCall[int]) (routery.BasicRouteResult[int], error) {
			r := routery.BasicHandled(1)
			r.Lifetime = routery.NewLifetime(func() error { cancel(); return nil })
			return r, nil
		},
	)
	first, _ := handler(routery.NewRouteCall(t.Context(), 0))
	go func() { defer close(done); defer func() { close(entered); <-gate }(); close(started); <-ctx.Done() }()
	<-started
	// Act.
	_ = first.Lifetime.Close()
	<-entered
	second, secondErr := handler(routery.NewRouteCall(t.Context(), 0))
	// Assert: permit was incorrectly released while cleanup is still blocked.
	if secondErr != nil {
		t.Fatal("cancel-only negative not reproduced", secondErr)
	}
	close(gate)
	<-done
	_ = second.Lifetime.Close()
}

func ExampleOwner() {
	// Arrange: this host owns cancellation, caller event types and the lazy source.
	base := func(call routery.RouteCall[string]) (routery.BasicRouteResult[*stream.Owner[string]], error) {
		ctx, cancel := context.WithCancel(call.Context)
		events := func(yield func(string, error) bool) {
			if ctx.Err() == nil {
				yield("hello", nil)
			}
		}
		owner, err := stream.New(events, func() error { cancel(); return nil }, func() error { return nil })
		if err != nil {
			return routery.AbortResult[routery.BasicKind, routery.BasicReason, *stream.Owner[string]](), err
		}
		result := routery.BasicHandled(owner)
		result.Lifetime = owner.Lifetime()
		return result, nil
	}
	// The host owns retry; consumption errors never trigger automatic replay.
	handler := routery.Timeout[string, routery.BasicKind, routery.BasicReason, *stream.Owner[string]](time.Hour)(base)
	// Act.
	result, dispatchErr := handler(routery.NewRouteCall(context.Background(), "request"))
	if dispatchErr != nil {
		fmt.Println(errors.Join(dispatchErr, result.Lifetime.Close()))
		return
	}
	var consumptionErr error
	for event, err := range result.Payload.Events() {
		consumptionErr = errors.Join(consumptionErr, err)
		fmt.Println(event)
		result.Payload.Cancel()
		break
	}
	// Close outside the callback, including partial result/error paths.
	fmt.Println(errors.Join(dispatchErr, consumptionErr, result.Lifetime.Close()))
	// Output:
	// hello
	// <nil>
}

func TestRunningCleanupErrorRetainsConsumptionError(t *testing.T) {
	// Arrange.
	sourceErr, cleanupErr := errors.New("source"), errors.New("cleanup")
	var cancels atomic.Int32
	owner, err := stream.New(
		func(yield func(int, error) bool) { yield(1, sourceErr) },
		func() error { cancels.Add(1); return cleanupErr },
		func() error { t.Error("running source discarded as unused"); return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	// Act.
	var observed error
	for _, eventErr := range owner.Events() {
		observed = errors.Join(observed, eventErr)
	}
	combined := errors.Join(observed, owner.Close())
	// Assert.
	if !errors.Is(combined, sourceErr) || !errors.Is(combined, cleanupErr) || cancels.Load() != 1 {
		t.Fatal(combined, cancels.Load())
	}
}
