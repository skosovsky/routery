package routeryredis_test

import (
	"context"
	"fmt"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/skosovsky/routery"
	routeryredis "github.com/skosovsky/routery/ext/redis"
	"github.com/skosovsky/routery/policy/attempt"
)

func ExampleNewStringRouteHandler_withRetryIf() {
	mr, err := miniredis.Run()
	if err != nil {
		fmt.Println("miniredis:", err)
		return
	}
	defer mr.Close()
	_ = mr.Set("user:1", "alice")

	client := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1})
	defer client.Close()
	base := routeryredis.NewStringRouteHandler(func(ctx context.Context, id int) (redis.Cmder, error) {
		return client.Get(ctx, fmt.Sprintf("user:%d", id)), nil
	})

	retry := routery.RetryIf[
		int,
		routery.BasicKind,
		routery.BasicReason,
		string,
	](3, 0, routeryredis.RetryPolicy(readEvidence))
	handler := routery.ApplyRoute(base, retry)

	outcome, err := routery.InvokeRouteHandler(context.Background(), 1, handler)
	if err != nil {
		fmt.Println("err", err)
		return
	}
	if !outcome.HasPayload {
		fmt.Println("no payload")
		return
	}
	fmt.Println(outcome.Payload)
	// Output: alice
}

// This GET has no side effects. Identity/facts belong to the enclosing host operation.
func readEvidence(context.Context, int, error) (attempt.Event, attempt.Replay, error) {
	return attempt.Event{Identity: attempt.Identity{Operation: "user-read", Attempt: "failed-read"},
			Phase: attempt.Terminal, Outcome: attempt.Unknown},
		attempt.Replay{Retryable: true, Replayable: true, SafeDuplicate: true}, nil
}
