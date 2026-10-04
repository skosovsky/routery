package routerys3_test

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/skosovsky/routery"
	routerys3 "github.com/skosovsky/routery/ext/s3"
)

type noopPut struct{}

func (noopPut) PutObject(
	ctx context.Context,
	params *s3.PutObjectInput,
	optFns ...func(*s3.Options),
) (*s3.PutObjectOutput, error) {
	_ = ctx
	_ = params
	_ = optFns
	return &s3.PutObjectOutput{}, nil
}

func ExampleNewPutObjectRouteHandler_withRetryIf() {
	base := routerys3.NewPutObjectRouteHandler(noopPut{})
	handler := routery.ApplyRoute(
		base,
		routery.RetryIf[routerys3.PutRequest, routery.BasicKind, routery.BasicReason, *s3.PutObjectOutput](
			2,
			0,
			routerys3.RetryPolicy[routerys3.PutRequest](nil),
		),
	)
	outcome, err := routery.InvokeRouteHandler(context.Background(), routerys3.PutRequest{}, handler)
	if err != nil {
		fmt.Println("err", err)
		return
	}
	fmt.Println(outcome.HasPayload)
	// Output: true
}
