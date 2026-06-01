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
	base := routery.HandlerFunc[int, int](func(context.Context, int) (routery.RouteResult[int], error) {
		calls++
		return routery.Handled(1), nil
	})

	result, err := Logging[int, int]("operation", nil, nil)(base).Handle(context.Background(), 0)
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if result.Payload != 1 {
		t.Fatalf("unexpected result: got %d, want 1", result.Payload)
	}
	if calls != 1 {
		t.Fatalf("unexpected call count: got %d, want 1", calls)
	}
}

func TestLoggingEmitsEvent(t *testing.T) {
	t.Parallel()

	eventChan := make(chan Event[int, int], 1)
	base := routery.HandlerFunc[int, int](func(context.Context, int) (routery.RouteResult[int], error) {
		return routery.Handled(2), nil
	})

	executor := Logging[int, int]("primary", func(_ context.Context, event Event[int, int]) {
		eventChan <- event
	}, nil)(base)

	result, err := executor.Handle(context.Background(), 1)
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if result.Payload != 2 {
		t.Fatalf("unexpected result: got %d, want 2", result.Payload)
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
	if event.Result.Payload != 2 {
		t.Fatalf("unexpected response: got %d, want 2", event.Result.Payload)
	}
	if event.Result.Status != routery.StatusHandled {
		t.Fatalf("unexpected status: got %q, want handled", event.Result.Status)
	}
	if event.PayloadMeta.Shape != "int" {
		t.Fatalf("unexpected payload shape: got %q, want int", event.PayloadMeta.Shape)
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

func TestLoggingReturnsConfigErrorWhenNextExecutorIsNil(t *testing.T) {
	t.Parallel()

	executor := Logging[int, int]("primary", func(context.Context, Event[int, int]) {}, nil)(nil)
	_, err := executor.Handle(context.Background(), 0)
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestLoggingDoesNotConsumeStreamLikeValues(t *testing.T) {
	t.Parallel()

	request := &readCounter{}
	response := &readCounter{}

	base := routery.HandlerFunc[*readCounter, *readCounter](
		func(context.Context, *readCounter) (routery.RouteResult[*readCounter], error) {
			return routery.Handled(response), nil
		},
	)

	executor := Logging[*readCounter, *readCounter](
		"stream",
		func(context.Context, Event[*readCounter, *readCounter]) {},
		nil,
	)(base)

	_, err := executor.Handle(context.Background(), request)
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
