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
		routery.RetryIf[*s3.PutObjectInput, *s3.PutObjectOutput](
			2,
			0,
			routerys3.DefaultRetryPolicy[*s3.PutObjectInput],
		),
	)
	outcome, err := routery.InvokeRouteHandler(context.Background(), &s3.PutObjectInput{}, handler)
	if err != nil {
		fmt.Println("err", err)
		return
	}
	fmt.Println(outcome.HasPayload)
	// Output: true
}
