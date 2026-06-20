package routerys3

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/skosovsky/routery"
)

// PutObjectAPI matches the PutObject method of [*s3.Client].
type PutObjectAPI interface {
	PutObject(
		ctx context.Context,
		params *s3.PutObjectInput,
		optFns ...func(*s3.Options),
	) (*s3.PutObjectOutput, error)
}

// GetObjectAPI matches the GetObject method of [*s3.Client].
type GetObjectAPI interface {
	GetObject(
		ctx context.Context,
		params *s3.GetObjectInput,
		optFns ...func(*s3.Options),
	) (*s3.GetObjectOutput, error)
}

// NewPutObjectRouteHandler wraps api.PutObject.
func NewPutObjectRouteHandler(api PutObjectAPI) routery.BasicRouteHandler[*s3.PutObjectInput, *s3.PutObjectOutput] {
	if api == nil {
		return invalidPutRouteHandler(configError("s3 PutObject client is nil"))
	}

	return func(call routery.RouteCall[*s3.PutObjectInput]) (routery.BasicRouteResult[*s3.PutObjectOutput], error) {
		output, err := api.PutObject(call.Context, call.Request)
		if err != nil {
			return routery.AbortResult[routery.BasicKind, routery.BasicReason, *s3.PutObjectOutput](), err
		}

		return routery.BasicHandled(output), nil
	}
}

// NewGetObjectRouteHandler wraps api.GetObject.
func NewGetObjectRouteHandler(api GetObjectAPI) routery.BasicRouteHandler[*s3.GetObjectInput, *s3.GetObjectOutput] {
	if api == nil {
		return invalidGetRouteHandler(configError("s3 GetObject client is nil"))
	}

	return func(call routery.RouteCall[*s3.GetObjectInput]) (routery.BasicRouteResult[*s3.GetObjectOutput], error) {
		output, err := api.GetObject(call.Context, call.Request)
		if err != nil {
			return routery.AbortResult[routery.BasicKind, routery.BasicReason, *s3.GetObjectOutput](), err
		}

		return routery.BasicHandled(output), nil
	}
}

func configError(detail string) error {
	return fmt.Errorf("%w: %s", routery.ErrInvalidConfig, detail)
}

func invalidPutRouteHandler(err error) routery.BasicRouteHandler[*s3.PutObjectInput, *s3.PutObjectOutput] {
	return func(routery.RouteCall[*s3.PutObjectInput]) (routery.BasicRouteResult[*s3.PutObjectOutput], error) {
		return routery.AbortResult[routery.BasicKind, routery.BasicReason, *s3.PutObjectOutput](), err
	}
}

func invalidGetRouteHandler(err error) routery.BasicRouteHandler[*s3.GetObjectInput, *s3.GetObjectOutput] {
	return func(routery.RouteCall[*s3.GetObjectInput]) (routery.BasicRouteResult[*s3.GetObjectOutput], error) {
		return routery.AbortResult[routery.BasicKind, routery.BasicReason, *s3.GetObjectOutput](), err
	}
}
