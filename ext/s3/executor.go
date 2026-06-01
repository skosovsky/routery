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

// NewPutObjectHandler wraps api.PutObject.
func NewPutObjectHandler(api PutObjectAPI) routery.Handler[*s3.PutObjectInput, *s3.PutObjectOutput] {
	if api == nil {
		return invalidPutHandler(configError("s3 PutObject client is nil"))
	}
	return routery.HandlerFunc[*s3.PutObjectInput, *s3.PutObjectOutput](
		func(ctx context.Context, in *s3.PutObjectInput) (routery.RouteResult[*s3.PutObjectOutput], error) {
			output, err := api.PutObject(ctx, in)
			if err != nil {
				return routery.RouteResult[*s3.PutObjectOutput]{}, err
			}

			return routery.Handled(output), nil
		},
	)
}

// NewGetObjectHandler wraps api.GetObject.
func NewGetObjectHandler(api GetObjectAPI) routery.Handler[*s3.GetObjectInput, *s3.GetObjectOutput] {
	if api == nil {
		return invalidGetHandler(configError("s3 GetObject client is nil"))
	}
	return routery.HandlerFunc[*s3.GetObjectInput, *s3.GetObjectOutput](
		func(ctx context.Context, in *s3.GetObjectInput) (routery.RouteResult[*s3.GetObjectOutput], error) {
			output, err := api.GetObject(ctx, in)
			if err != nil {
				return routery.RouteResult[*s3.GetObjectOutput]{}, err
			}

			return routery.Handled(output), nil
		},
	)
}

func configError(detail string) error {
	return fmt.Errorf("%w: %s", routery.ErrInvalidConfig, detail)
}

func invalidPutHandler(err error) routery.Handler[*s3.PutObjectInput, *s3.PutObjectOutput] {
	return routery.HandlerFunc[*s3.PutObjectInput, *s3.PutObjectOutput](
		func(context.Context, *s3.PutObjectInput) (routery.RouteResult[*s3.PutObjectOutput], error) {
			return routery.RouteResult[*s3.PutObjectOutput]{}, err
		},
	)
}

func invalidGetHandler(err error) routery.Handler[*s3.GetObjectInput, *s3.GetObjectOutput] {
	return routery.HandlerFunc[*s3.GetObjectInput, *s3.GetObjectOutput](
		func(context.Context, *s3.GetObjectInput) (routery.RouteResult[*s3.GetObjectOutput], error) {
			return routery.RouteResult[*s3.GetObjectOutput]{}, err
		},
	)
}
