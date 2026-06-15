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
func NewPutObjectRouteHandler(api PutObjectAPI) routery.RouteHandler[*s3.PutObjectInput, *s3.PutObjectOutput] {
	if api == nil {
		return invalidPutRouteHandler(configError("s3 PutObject client is nil"))
	}

	return func(ctx context.Context, in *s3.PutObjectInput, rec routery.ResultRecorder[*s3.PutObjectOutput]) error {
		output, err := api.PutObject(ctx, in)
		if err != nil {
			return err
		}

		rec.Stop(output, "")
		return nil
	}
}

// NewGetObjectRouteHandler wraps api.GetObject.
func NewGetObjectRouteHandler(api GetObjectAPI) routery.RouteHandler[*s3.GetObjectInput, *s3.GetObjectOutput] {
	if api == nil {
		return invalidGetRouteHandler(configError("s3 GetObject client is nil"))
	}

	return func(ctx context.Context, in *s3.GetObjectInput, rec routery.ResultRecorder[*s3.GetObjectOutput]) error {
		output, err := api.GetObject(ctx, in)
		if err != nil {
			return err
		}

		rec.Stop(output, "")
		return nil
	}
}

func configError(detail string) error {
	return fmt.Errorf("%w: %s", routery.ErrInvalidConfig, detail)
}

func invalidPutRouteHandler(err error) routery.RouteHandler[*s3.PutObjectInput, *s3.PutObjectOutput] {
	return func(context.Context, *s3.PutObjectInput, routery.ResultRecorder[*s3.PutObjectOutput]) error {
		return err
	}
}

func invalidGetRouteHandler(err error) routery.RouteHandler[*s3.GetObjectInput, *s3.GetObjectOutput] {
	return func(context.Context, *s3.GetObjectInput, routery.ResultRecorder[*s3.GetObjectOutput]) error {
		return err
	}
}
