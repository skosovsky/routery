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
	base := routery.ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		calls++
		return 1, nil
	})

	result, err := Logging[int, int]("operation", nil)(base).Execute(context.Background(), 0)
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if result != 1 {
		t.Fatalf("unexpected result: got %d, want 1", result)
	}
	if calls != 1 {
		t.Fatalf("unexpected call count: got %d, want 1", calls)
	}
}

func TestLoggingEmitsEvent(t *testing.T) {
	t.Parallel()

	eventChan := make(chan Event[int, int], 1)
	base := routery.ExecutorFunc[int, int](func(context.Context, int) (int, error) {
		return 2, nil
	})

	executor := Logging[int, int]("primary", func(_ context.Context, event Event[int, int]) {
		eventChan <- event
	})(base)

	result, err := executor.Execute(context.Background(), 1)
	if err != nil {
		t.Fatalf("execute returned unexpected error: %v", err)
	}
	if result != 2 {
		t.Fatalf("unexpected result: got %d, want 2", result)
	}

	select {
	case event := <-eventChan:
		if event.Name != "primary" {
			t.Fatalf("unexpected name: got %q, want %q", event.Name, "primary")
		}
		if event.Request != 1 {
			t.Fatalf("unexpected request: got %d, want 1", event.Request)
		}
		if event.Response != 2 {
			t.Fatalf("unexpected response: got %d, want 2", event.Response)
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
	case <-time.After(16 * time.Millisecond):
		t.Fatal("expected one logging event")
	}
}

func TestLoggingReturnsConfigErrorWhenNextExecutorIsNil(t *testing.T) {
	t.Parallel()

	executor := Logging[int, int]("primary", func(context.Context, Event[int, int]) {})(nil)
	_, err := executor.Execute(context.Background(), 0)
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestLoggingDoesNotConsumeStreamLikeValues(t *testing.T) {
	t.Parallel()

	request := &readCounter{}
	response := &readCounter{}

	base := routery.ExecutorFunc[*readCounter, *readCounter](func(context.Context, *readCounter) (*readCounter, error) {
		return response, nil
	})

	executor := Logging[*readCounter, *readCounter](
		"stream",
		func(context.Context, Event[*readCounter, *readCounter]) {},
	)(base)

	_, err := executor.Execute(context.Background(), request)
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
