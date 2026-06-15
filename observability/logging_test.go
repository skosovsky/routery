package observability

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/skosovsky/routery"
)

func TestLoggingNoOpWhenHandlerIsNil(t *testing.T) {
	t.Parallel()

	calls := 0
	base := routery.FromFunc(func(context.Context, int) (int, error) {
		calls++
		return 1, nil
	})

	handler := routery.ApplyRoute(base, Logging[int, int]("operation", nil, nil))
	outcome, err := routery.InvokeRouteHandler(context.Background(), 0, handler)
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if !outcome.HasPayload || outcome.Payload != 1 {
		t.Fatalf("unexpected result: got %d, want 1", outcome.Payload)
	}
	if calls != 1 {
		t.Fatalf("unexpected call count: got %d, want 1", calls)
	}
}

func TestLoggingEmitsEvent(t *testing.T) {
	t.Parallel()

	eventChan := make(chan Event[int, int], 1)
	base := routery.FromFunc(func(context.Context, int) (int, error) {
		return 2, nil
	})

	handler := routery.ApplyRoute(base, Logging[int, int]("primary", func(_ context.Context, event Event[int, int]) {
		eventChan <- event
	}, nil))

	outcome, err := routery.InvokeRouteHandler(context.Background(), 1, handler)
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if !outcome.HasPayload || outcome.Payload != 2 {
		t.Fatalf("unexpected result: got %d, want 2", outcome.Payload)
	}

	select {
	case event := <-eventChan:
		assertLoggingEvent(t, event)
	case <-time.After(16 * time.Millisecond):
		t.Fatal("expected one logging event")
	}
}

func assertLoggingEvent(t *testing.T, event Event[int, int]) {
	t.Helper()

	if event.Name != "primary" {
		t.Fatalf("unexpected name: got %q, want %q", event.Name, "primary")
	}
	if event.Request != 1 {
		t.Fatalf("unexpected request: got %d, want 1", event.Request)
	}
	if event.Outcome.Action != routery.ActionStop {
		t.Fatalf("unexpected action: got %q, want stop", event.Outcome.Action)
	}
	if event.Outcome.PayloadMeta.Shape != "int" {
		t.Fatalf("unexpected payload shape: got %q, want int", event.Outcome.PayloadMeta.Shape)
	}
	if event.Err != nil {
		t.Fatalf("unexpected event error: %v", event.Err)
	}
	if event.StartTime.IsZero() {
		t.Fatal("expected non-zero start time")
	}
	if event.Duration < 0 {
		t.Fatalf("expected non-negative duration, got %v", event.Duration)
	}
}

func TestLoggingEmitsActionNext(t *testing.T) {
	t.Parallel()

	eventChan := make(chan Event[int, int], 1)
	base := func(_ context.Context, _ int, rec routery.ResultRecorder[int]) error {
		rec.Next("delegate")
		return nil
	}

	handler := routery.ApplyRoute(base, Logging[int, int]("next", func(_ context.Context, event Event[int, int]) {
		eventChan <- event
	}, nil))

	outcome, err := routery.InvokeRouteHandler(context.Background(), 0, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Action != routery.ActionNext {
		t.Fatalf("got action %q; want next", outcome.Action)
	}

	select {
	case event := <-eventChan:
		if event.Outcome.Action != routery.ActionNext {
			t.Fatalf("got event action %q; want next", event.Outcome.Action)
		}
		if event.Outcome.ReasonCode != "delegate" {
			t.Fatalf("got reason %q; want delegate", event.Outcome.ReasonCode)
		}
	case <-time.After(16 * time.Millisecond):
		t.Fatal("expected one logging event")
	}
}

func TestLoggingEmitsPartialOutcomeOnError(t *testing.T) {
	t.Parallel()

	eventChan := make(chan Event[int, int], 1)
	handlerErr := errors.New("handler failed")
	base := func(_ context.Context, _ int, _ routery.ResultRecorder[int]) error {
		return handlerErr
	}

	handler := routery.ApplyRoute(base, Logging[int, int]("error", func(_ context.Context, event Event[int, int]) {
		eventChan <- event
	}, nil))

	_, err := routery.InvokeRouteHandler(context.Background(), 0, handler)
	if !errors.Is(err, handlerErr) {
		t.Fatalf("got err %v; want %v", err, handlerErr)
	}

	select {
	case event := <-eventChan:
		if !errors.Is(event.Err, handlerErr) {
			t.Fatalf("got event err %v; want %v", event.Err, handlerErr)
		}
		if event.Outcome.Action != routery.ActionAbort {
			t.Fatalf("got action %q; want abort on error", event.Outcome.Action)
		}
		if event.Outcome.PayloadMeta.Shape != shapeEmpty {
			t.Fatalf("got shape %q; want empty", event.Outcome.PayloadMeta.Shape)
		}
	case <-time.After(16 * time.Millisecond):
		t.Fatal("expected one logging event")
	}
}

func TestLoggingEmitsIgnoreDisposition(t *testing.T) {
	t.Parallel()

	eventChan := make(chan Event[int, int], 1)
	base := func(_ context.Context, _ int, rec routery.ResultRecorder[int]) error {
		rec.Ignore("declined")
		return nil
	}

	handler := routery.ApplyRoute(base, Logging[int, int]("ignore", func(_ context.Context, event Event[int, int]) {
		eventChan <- event
	}, nil))

	outcome, err := routery.InvokeRouteHandler(context.Background(), 0, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Action != routery.ActionStop {
		t.Fatalf("got outcome action %q; want stop", outcome.Action)
	}

	select {
	case event := <-eventChan:
		if event.Outcome.Action != routery.ActionStop {
			t.Fatalf("got action %q; want stop", event.Outcome.Action)
		}
		if event.Outcome.PayloadMeta.Shape != shapeEmpty {
			t.Fatalf("got shape %q; want empty", event.Outcome.PayloadMeta.Shape)
		}
	case <-time.After(16 * time.Millisecond):
		t.Fatal("expected one logging event")
	}
}

func TestLoggingReturnsConfigErrorWhenNextRouteHandlerIsNil(t *testing.T) {
	t.Parallel()

	handler := routery.ApplyRoute[int, int](
		nil,
		Logging[int, int]("primary", func(context.Context, Event[int, int]) {}, nil),
	)
	_, err := routery.InvokeRouteHandler(context.Background(), 0, handler)
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestLoggingDoesNotConsumeStreamLikeValues(t *testing.T) {
	t.Parallel()

	request := &readCounter{}
	response := &readCounter{}

	base := routery.FromFunc(func(_ context.Context, _ *readCounter) (*readCounter, error) {
		return response, nil
	})

	handler := routery.ApplyRoute(base, Logging[*readCounter, *readCounter](
		"stream",
		func(context.Context, Event[*readCounter, *readCounter]) {},
		nil,
	))

	_, err := routery.InvokeRouteHandler(context.Background(), request, handler)
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}

	if request.reads.Load() != 0 {
		t.Fatalf("request stream was consumed unexpectedly: reads=%d", request.reads.Load())
	}
	if response.reads.Load() != 0 {
		t.Fatalf("response stream was consumed unexpectedly: reads=%d", response.reads.Load())
	}
}

type readCounter struct {
	reads atomic.Int32
}

func (counter *readCounter) Read([]byte) (int, error) {
	counter.reads.Add(1)
	return 0, io.EOF
}
