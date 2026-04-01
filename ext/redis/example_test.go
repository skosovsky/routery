package routeryredis_test

import (
	"context"
	"fmt"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/skosovsky/routery"
	routeryredis "github.com/skosovsky/routery/ext/redis"
)

func ExampleNewStringExecutor_withRetryIf() {
	mr, err := miniredis.Run()
	if err != nil {
		fmt.Println("miniredis:", err)
		return
	}
	defer mr.Close()
	_ = mr.Set("user:1", "alice")

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	base := routeryredis.NewStringExecutor(client, func(ctx context.Context, id int) (redis.Cmder, error) {
		return client.Get(ctx, fmt.Sprintf("user:%d", id)), nil
	})

	executor := routery.Apply(
		base,
		routery.RetryIf[int, string](3, 0, routeryredis.DefaultRetryPolicy[int]),
	)

	v, err := executor.Execute(context.Background(), 1)
	if err != nil {
		fmt.Println("err", err)
		return
	}
	fmt.Println(v)
	// Output: alice
}
