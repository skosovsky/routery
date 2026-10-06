package routery_test

import (
	"context"
	"fmt"

	"github.com/skosovsky/routery"
)

func ExampleRouteTable() {
	type Request struct{ Destination string }
	type Kind string
	type Reason string
	table := routery.NewRouteTable[Request, Kind, Reason, string]()
	table.Route(
		"local",
		0,
		func(req Request) bool { return req.Destination == "local" },
		func(routery.RouteCall[Request]) (routery.RouteResult[Kind, Reason, string], error) {
			return routery.Handled(Kind("answer"), Reason("local"), "hello"), nil
		},
	)
	table.Fallback(func(routery.RouteCall[Request]) (routery.RouteResult[Kind, Reason, string], error) {
		return routery.Handled(Kind("answer"), Reason("fallback"), "remote"), nil
	})
	router, err := table.Build()
	if err != nil {
		panic(err)
	}
	result, err := router.Dispatch(context.Background(), Request{Destination: "local"})
	if err != nil {
		panic(err)
	}
	fmt.Println(result.Kind, result.Reason, result.Payload)
	// Output: answer local hello
}

func ExampleRouteTable_callerTypes() {
	type Work struct{ Size int }
	type Disposition uint8
	type Cause uint8
	type Answer struct{ Accepted int }
	table := routery.NewRouteTable[Work, Disposition, Cause, Answer]()
	table.Route(
		"work",
		0,
		nil,
		func(call routery.RouteCall[Work]) (routery.RouteResult[Disposition, Cause, Answer], error) {
			return routery.Handled(Disposition(1), Cause(0), Answer{Accepted: call.Request.Size}), nil
		},
	)
	router, err := table.Build()
	if err != nil {
		panic(err)
	}
	result, err := router.Dispatch(context.Background(), Work{Size: 2})
	if err != nil {
		panic(err)
	}
	fmt.Println(result.Payload.Accepted)
	// Output: 2
}
