package routerys3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

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

// NewPutObjectRouteHandler creates and owns a fresh body for each invocation.
// Input must have no Body; BodyFactory transfers ownership even on error.
func NewPutObjectRouteHandler(api PutObjectAPI) routery.BasicRouteHandler[PutRequest, *s3.PutObjectOutput] {
	if api == nil {
		return invalidPutRouteHandler(configError("s3 PutObject client is nil"))
	}
	return func(call routery.RouteCall[PutRequest]) (routery.BasicRouteResult[*s3.PutObjectOutput], error) {
		if call.Request.Input.Body != nil {
			return routery.AbortResult[routery.BasicKind, routery.BasicReason, *s3.PutObjectOutput](), configError(
				"prepare PutObject body or provide a BodyFactory",
			)
		}
		if err := call.Context.Err(); err != nil {
			return routery.AbortResult[routery.BasicKind, routery.BasicReason, *s3.PutObjectOutput](), err
		}
		input := call.Request.Input
		body, openErr := openBody(call.Context, call.Request.BodyFactory)
		if openErr != nil {
			return routery.AbortResult[routery.BasicKind, routery.BasicReason, *s3.PutObjectOutput](), openErr
		}
		input.Body = body
		if err := call.Context.Err(); err != nil {
			if body != nil {
				err = errors.Join(err, body.Close())
			}
			return routery.AbortResult[routery.BasicKind, routery.BasicReason, *s3.PutObjectOutput](), err
		}
		output, err := api.PutObject(call.Context, &input)
		if body != nil {
			err = errors.Join(err, body.Close())
		}
		if output != nil {
			return routery.BasicHandled(output), err
		}
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
		if err != nil && output == nil {
			return routery.AbortResult[routery.BasicKind, routery.BasicReason, *s3.GetObjectOutput](), err
		}

		result := routery.BasicHandled(output)
		if output != nil && output.Body != nil {
			life := routery.NewLifetime(output.Body.Close)
			output.Body = &ownedBody{ReadCloser: output.Body, life: life}
			result.Lifetime = life
		}
		return result, err
	}
}

type ownedBody struct {
	io.ReadCloser

	life *routery.Lifetime
}

func (body *ownedBody) Close() error {
	return body.life.Close()
}

func configError(detail string) error {
	return fmt.Errorf("%w: %s", routery.ErrInvalidConfig, detail)
}

func invalidPutRouteHandler(err error) routery.BasicRouteHandler[PutRequest, *s3.PutObjectOutput] {
	return func(routery.RouteCall[PutRequest]) (routery.BasicRouteResult[*s3.PutObjectOutput], error) {
		return routery.AbortResult[routery.BasicKind, routery.BasicReason, *s3.PutObjectOutput](), err
	}
}

func invalidGetRouteHandler(err error) routery.BasicRouteHandler[*s3.GetObjectInput, *s3.GetObjectOutput] {
	return func(routery.RouteCall[*s3.GetObjectInput]) (routery.BasicRouteResult[*s3.GetObjectOutput], error) {
		return routery.AbortResult[routery.BasicKind, routery.BasicReason, *s3.GetObjectOutput](), err
	}
}

func openBody(ctx context.Context, factory BodyFactory) (io.ReadCloser, error) {
	if factory == nil {
		return &preparedBody{Reader: bytes.NewReader(nil)}, nil
	}
	body, err := factory(ctx)
	if err != nil {
		if body != nil {
			err = errors.Join(err, body.Close())
		}
		return nil, err
	}
	if body == nil {
		return nil, configError("nil factory body")
	}
	return body, nil
}
