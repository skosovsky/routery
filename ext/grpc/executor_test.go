package routerygrpc

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/skosovsky/routery"
)

func TestNewUnaryRouteHandlerNilInvoker(t *testing.T) {
	t.Parallel()
	ex := NewUnaryRouteHandler[int, int](nil)
	_, err := routery.InvokeRouteHandler(context.Background(), 0, ex)
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestNewUnaryRouteHandlerOK(t *testing.T) {
	t.Parallel()
	ex := NewUnaryRouteHandler(func(_ context.Context, x int) (int, error) {
		return x + 1, nil
	})
	outcome, err := routery.InvokeRouteHandler(context.Background(), 41, ex)
	if err != nil || !outcome.HasPayload || outcome.Payload != 42 {
		t.Fatalf("got %d err=%v", outcome.Payload, err)
	}
}

func TestNewUnaryRouteHandlerConcurrent(t *testing.T) {
	t.Parallel()
	ex := NewUnaryRouteHandler(func(_ context.Context, x int) (int, error) {
		return x, nil
	})
	const workers = 128
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			_, e := routery.InvokeRouteHandler(context.Background(), 7, ex)
			if e != nil {
				t.Error(e)
			}
		})
	}
	wg.Wait()
}
