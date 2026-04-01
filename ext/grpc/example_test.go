package routerygrpc_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/skosovsky/routery"
	routerygrpc "github.com/skosovsky/routery/ext/grpc"
)

func ExampleNewUnaryExecutor_withRetryIf() {
	base := routerygrpc.NewUnaryExecutor(func(_ context.Context, x int) (int, error) {
		if x < 0 {
			return 0, errors.New("negative")
		}
		return x * 2, nil
	})

	executor := routery.Apply(
		base,
		routery.RetryIf[int, int](2, 0, func(context.Context, int, error) bool { return false }),
	)

	v, err := executor.Execute(context.Background(), 21)
	if err != nil {
		fmt.Println("err", err)
		return
	}
	fmt.Println(v)
	// Output: 42
}
