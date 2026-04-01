package routerygrpc

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/skosovsky/routery"
)

func TestNewUnaryExecutorNilInvoker(t *testing.T) {
	t.Parallel()
	ex := NewUnaryExecutor[int, int](nil)
	_, err := ex.Execute(context.Background(), 0)
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestNewUnaryExecutorOK(t *testing.T) {
	t.Parallel()
	ex := NewUnaryExecutor(func(_ context.Context, x int) (int, error) {
		return x + 1, nil
	})
	v, err := ex.Execute(context.Background(), 41)
	if err != nil || v != 42 {
		t.Fatalf("got %d err=%v", v, err)
	}
}

func TestNewUnaryExecutorConcurrent(t *testing.T) {
	t.Parallel()
	ex := NewUnaryExecutor(func(_ context.Context, x int) (int, error) {
		return x, nil
	})
	const workers = 128
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			_, e := ex.Execute(context.Background(), 7)
			if e != nil {
				t.Error(e)
			}
		})
	}
	wg.Wait()
}
