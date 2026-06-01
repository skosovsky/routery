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

func TestNewHandlerNilClient(t *testing.T) {
	t.Parallel()
	ex := NewHandler[int, string](
		nil,
		func(context.Context, int) (redis.Cmder, error) {
			panic("unreachable")
		},
		nil,
	)
	_, err := ex.Handle(context.Background(), 0)
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestNewHandlerNilExtractor(t *testing.T) {
	t.Parallel()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	ex := NewHandler[int, string](client, nil, func(context.Context, redis.Cmder) (string, error) { return "", nil })
	_, err = ex.Handle(context.Background(), 0)
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestNewHandlerNilScan(t *testing.T) {
	t.Parallel()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	ex := NewHandler[int, string](client, func(context.Context, int) (redis.Cmder, error) {
		return client.Get(context.Background(), "k"), nil
	}, nil)
	_, err = ex.Handle(context.Background(), 0)
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestNewHandlerGetHit(t *testing.T) {
	t.Parallel()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	_ = mr.Set("k", "v")

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	ex := NewStringHandler(client, func(ctx context.Context, _ int) (redis.Cmder, error) {
		return client.Get(ctx, "k"), nil
	})

	gotResult, err := ex.Handle(context.Background(), 0)
	if err != nil || gotResult.Payload != "v" {
		t.Fatalf("got %q err=%v", gotResult.Payload, err)
	}
}

func TestNewHandlerGetMiss(t *testing.T) {
	t.Parallel()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	ex := NewStringHandler(client, func(ctx context.Context, _ int) (redis.Cmder, error) {
		return client.Get(ctx, "missing"), nil
	})

	_, err = ex.Handle(context.Background(), 0)
	if !errors.Is(err, redis.Nil) {
		t.Fatalf("want redis.Nil, got %v", err)
	}
}

func TestNewHandlerExtractorError(t *testing.T) {
	t.Parallel()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	want := errors.New("extract fail")
	ex := NewStringHandler(client, func(context.Context, int) (redis.Cmder, error) {
		return nil, want
	})
	_, err = ex.Handle(context.Background(), 0)
	if !errors.Is(err, want) {
		t.Fatalf("got %v", err)
	}
}

func TestNewHandlerNilCmdFromExtractor(t *testing.T) {
	t.Parallel()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	ex := NewStringHandler(client, func(context.Context, int) (redis.Cmder, error) {
		return nil, nil //nolint:nilnil // exercise nil command after successful extraction
	})
	_, err = ex.Handle(context.Background(), 0)
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestNewStringHandlerWrongCmdType(t *testing.T) {
	t.Parallel()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	ex := NewStringHandler(client, func(ctx context.Context, _ int) (redis.Cmder, error) {
		return client.Set(ctx, "a", "b", 0), nil
	})
	_, err = ex.Handle(context.Background(), 0)
	if err == nil || err.Error() == "" {
		t.Fatalf("expected type error, got %v", err)
	}
}

func TestExecutorConcurrent(t *testing.T) {
	t.Parallel()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	_ = mr.Set("n", "0")

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	ex := NewStringHandler(client, func(ctx context.Context, _ int) (redis.Cmder, error) {
		return client.Get(ctx, "n"), nil
	})

	const workers = 128
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			_, e := ex.Handle(context.Background(), 0)
			if e != nil {
				t.Error(e)
			}
		})
	}
	wg.Wait()
}
