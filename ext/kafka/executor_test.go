package routerykafka

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/segmentio/kafka-go"

	"github.com/skosovsky/routery"
)

type fakeWriter struct {
	calls atomic.Int32
	err   error
}

func (f *fakeWriter) WriteMessages(ctx context.Context, msgs ...kafka.Message) error {
	f.calls.Add(1)
	_ = ctx
	_ = msgs
	return f.err
}

func TestNewProducerExecutorNil(t *testing.T) {
	t.Parallel()
	ex := NewProducerExecutor(nil)
	_, err := ex.Execute(context.Background(), PublishRequest{})
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestNewProducerExecutorEmptyMessages(t *testing.T) {
	t.Parallel()
	fw := &fakeWriter{}
	ex := NewProducerExecutor(fw)
	_, err := ex.Execute(context.Background(), PublishRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if fw.calls.Load() != 0 {
		t.Fatalf("unexpected WriteMessages: %d", fw.calls.Load())
	}
}

func TestNewProducerExecutorWrites(t *testing.T) {
	t.Parallel()
	fw := &fakeWriter{}
	ex := NewProducerExecutor(fw)
	_, err := ex.Execute(context.Background(), PublishRequest{
		Messages: []kafka.Message{{Value: []byte("a")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if fw.calls.Load() != 1 {
		t.Fatalf("want 1 call, got %d", fw.calls.Load())
	}
}

func TestNewProducerExecutorPropagatesError(t *testing.T) {
	t.Parallel()
	want := errors.New("fail")
	fw := &fakeWriter{err: want}
	ex := NewProducerExecutor(fw)
	_, err := ex.Execute(context.Background(), PublishRequest{
		Messages: []kafka.Message{{}},
	})
	if !errors.Is(err, want) {
		t.Fatalf("got %v", err)
	}
}

func TestProducerExecutorConcurrent(t *testing.T) {
	t.Parallel()
	fw := &fakeWriter{}
	ex := NewProducerExecutor(fw)
	const workers = 128
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			_, e := ex.Execute(context.Background(), PublishRequest{
				Messages: []kafka.Message{{Value: []byte("x")}},
			})
			if e != nil {
				t.Error(e)
			}
		})
	}
	wg.Wait()
	if fw.calls.Load() != workers {
		t.Fatalf("want %d calls, got %d", workers, fw.calls.Load())
	}
}
