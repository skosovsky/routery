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

func TestNewExecutorNilClient(t *testing.T) {
	t.Parallel()
	ex := NewExecutor[int, string](
		nil,
		func(context.Context, int) (redis.Cmder, error) {
			panic("unreachable")
		},
		nil,
	)
	_, err := ex.Execute(context.Background(), 0)
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestNewExecutorNilExtractor(t *testing.T) {
	t.Parallel()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	ex := NewExecutor[int, string](client, nil, func(context.Context, redis.Cmder) (string, error) { return "", nil })
	_, err = ex.Execute(context.Background(), 0)
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestNewExecutorNilScan(t *testing.T) {
	t.Parallel()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	ex := NewExecutor[int, string](client, func(context.Context, int) (redis.Cmder, error) {
		return client.Get(context.Background(), "k"), nil
	}, nil)
	_, err = ex.Execute(context.Background(), 0)
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestNewExecutorGetHit(t *testing.T) {
	t.Parallel()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	_ = mr.Set("k", "v")

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	ex := NewStringExecutor(client, func(ctx context.Context, _ int) (redis.Cmder, error) {
		return client.Get(ctx, "k"), nil
	})

	got, err := ex.Execute(context.Background(), 0)
	if err != nil || got != "v" {
		t.Fatalf("got %q err=%v", got, err)
	}
}

func TestNewExecutorGetMiss(t *testing.T) {
	t.Parallel()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	ex := NewStringExecutor(client, func(ctx context.Context, _ int) (redis.Cmder, error) {
		return client.Get(ctx, "missing"), nil
	})

	_, err = ex.Execute(context.Background(), 0)
	if !errors.Is(err, redis.Nil) {
		t.Fatalf("want redis.Nil, got %v", err)
	}
}

func TestNewExecutorExtractorError(t *testing.T) {
	t.Parallel()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	want := errors.New("extract fail")
	ex := NewStringExecutor(client, func(context.Context, int) (redis.Cmder, error) {
		return nil, want
	})
	_, err = ex.Execute(context.Background(), 0)
	if !errors.Is(err, want) {
		t.Fatalf("got %v", err)
	}
}

func TestNewExecutorNilCmdFromExtractor(t *testing.T) {
	t.Parallel()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	ex := NewStringExecutor(client, func(context.Context, int) (redis.Cmder, error) {
		return nil, nil //nolint:nilnil // exercise nil command after successful extraction
	})
	_, err = ex.Execute(context.Background(), 0)
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestNewStringExecutorWrongCmdType(t *testing.T) {
	t.Parallel()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	ex := NewStringExecutor(client, func(ctx context.Context, _ int) (redis.Cmder, error) {
		return client.Set(ctx, "a", "b", 0), nil
	})
	_, err = ex.Execute(context.Background(), 0)
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
	ex := NewStringExecutor(client, func(ctx context.Context, _ int) (redis.Cmder, error) {
		return client.Get(ctx, "n"), nil
	})

	const workers = 128
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			_, e := ex.Execute(context.Background(), 0)
			if e != nil {
				t.Error(e)
			}
		})
	}
	wg.Wait()
}
