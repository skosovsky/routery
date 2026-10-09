//go:build integration

package routeryredis

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/skosovsky/routery"
)

func TestIntegrationNewRouteHandlerNilScan(t *testing.T) {
	t.Parallel()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	ex := NewRouteHandler[int, string](func(context.Context, int) (redis.Cmder, error) {
		return client.Get(context.Background(), "k"), nil
	}, nil)
	_, err = routery.InvokeRouteHandler(context.Background(), 0, ex)
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestIntegrationNewRouteHandlerGetHit(t *testing.T) {
	t.Parallel()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	_ = mr.Set("k", "v")

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	ex := NewStringRouteHandler(func(ctx context.Context, _ int) (redis.Cmder, error) {
		return client.Get(ctx, "k"), nil
	})

	outcome, err := routery.InvokeRouteHandler(context.Background(), 0, ex)
	if err != nil || !outcome.HasPayload || outcome.Payload != "v" {
		t.Fatalf("got %q err=%v", outcome.Payload, err)
	}
}

func TestIntegrationNewRouteHandlerGetMiss(t *testing.T) {
	t.Parallel()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	ex := NewStringRouteHandler(func(ctx context.Context, _ int) (redis.Cmder, error) {
		return client.Get(ctx, "missing"), nil
	})

	_, err = routery.InvokeRouteHandler(context.Background(), 0, ex)
	if !errors.Is(err, redis.Nil) {
		t.Fatalf("want redis.Nil, got %v", err)
	}
}

func TestIntegrationNewRouteHandlerNilCmdFromExtractor(t *testing.T) {
	t.Parallel()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)

	ex := NewStringRouteHandler(func(context.Context, int) (redis.Cmder, error) {
		return nil, nil //nolint:nilnil // exercise nil command after successful extraction
	})
	_, err = routery.InvokeRouteHandler(context.Background(), 0, ex)
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestIntegrationNewStringRouteHandlerWrongCmdType(t *testing.T) {
	t.Parallel()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	ex := NewStringRouteHandler(func(ctx context.Context, _ int) (redis.Cmder, error) {
		return client.Set(ctx, "a", "b", 0), nil
	})
	_, err = routery.InvokeRouteHandler(context.Background(), 0, ex)
	if err == nil || err.Error() == "" {
		t.Fatalf("expected type error, got %v", err)
	}
}

func TestIntegrationRouteHandlerConcurrent(t *testing.T) {
	t.Parallel()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	_ = mr.Set("n", "0")

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	ex := NewStringRouteHandler(func(ctx context.Context, _ int) (redis.Cmder, error) {
		return client.Get(ctx, "n"), nil
	})

	const workers = 128
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			_, e := routery.InvokeRouteHandler(context.Background(), 0, ex)
			if e != nil {
				t.Error(e)
			}
		})
	}
	wg.Wait()
}
