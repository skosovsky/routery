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

// NewPutObjectExecutor wraps api.PutObject.
func NewPutObjectExecutor(api PutObjectAPI) routery.Executor[*s3.PutObjectInput, *s3.PutObjectOutput] {
	if api == nil {
		return invalidPutExecutor(configError("s3 PutObject client is nil"))
	}
	return routery.ExecutorFunc[*s3.PutObjectInput, *s3.PutObjectOutput](
		func(ctx context.Context, in *s3.PutObjectInput) (*s3.PutObjectOutput, error) {
			return api.PutObject(ctx, in)
		},
	)
}

// NewGetObjectExecutor wraps api.GetObject.
func NewGetObjectExecutor(api GetObjectAPI) routery.Executor[*s3.GetObjectInput, *s3.GetObjectOutput] {
	if api == nil {
		return invalidGetExecutor(configError("s3 GetObject client is nil"))
	}
	return routery.ExecutorFunc[*s3.GetObjectInput, *s3.GetObjectOutput](
		func(ctx context.Context, in *s3.GetObjectInput) (*s3.GetObjectOutput, error) {
			return api.GetObject(ctx, in)
		},
	)
}

func configError(detail string) error {
	return fmt.Errorf("%w: %s", routery.ErrInvalidConfig, detail)
}

func invalidPutExecutor(err error) routery.Executor[*s3.PutObjectInput, *s3.PutObjectOutput] {
	return routery.ExecutorFunc[*s3.PutObjectInput, *s3.PutObjectOutput](
		func(context.Context, *s3.PutObjectInput) (*s3.PutObjectOutput, error) {
			return nil, err
		},
	)
}

func invalidGetExecutor(err error) routery.Executor[*s3.GetObjectInput, *s3.GetObjectOutput] {
	return routery.ExecutorFunc[*s3.GetObjectInput, *s3.GetObjectOutput](
		func(context.Context, *s3.GetObjectInput) (*s3.GetObjectOutput, error) {
			return nil, err
		},
	)
}
