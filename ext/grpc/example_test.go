package routerygrpc_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/skosovsky/routery"
	routerygrpc "github.com/skosovsky/routery/ext/grpc"
)

func ExampleNewUnaryRouteHandler_withRetryIf() {
	base := routerygrpc.NewUnaryRouteHandler(func(_ context.Context, x int) (int, error) {
		if x < 0 {
			return 0, errors.New("negative")
		}
		return x * 2, nil
	})

	handler := routery.ApplyRoute(
		base,
		routery.RetryIf[int, int](2, 0, func(context.Context, int, error) bool { return false }),
	)

	outcome, err := routery.InvokeRouteHandler(context.Background(), 21, handler)
	if err != nil {
		fmt.Println("err", err)
		return
	}
	if !outcome.HasPayload {
		fmt.Println("no payload")
		return
	}
	fmt.Println(outcome.Payload)
	// Output: 42
}
