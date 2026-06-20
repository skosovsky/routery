package routerygrpc

import (
	"context"
	"time"

	"google.golang.org/grpc"

	"github.com/skosovsky/routery"
)

// InterceptorOptions configures retrying client interceptors.
type InterceptorOptions struct {
	Attempts  int
	Backoff   time.Duration
	Predicate routery.RetryPredicate[any]
}

// RetryUnaryInterceptor wraps the unary invoker with [routery.RetryIf].
func RetryUnaryInterceptor(opts InterceptorOptions) grpc.UnaryClientInterceptor {
	attempts, backoff, pred := normalizeInterceptorOpts(opts)

	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, callOpts ...grpc.CallOption) error {
		base := routery.BasicRouteHandler[any, struct{}](
			func(call routery.RouteCall[any]) (routery.BasicRouteResult[struct{}], error) {
				err := invoker(call.Context, method, call.Request, reply, cc, callOpts...)
				if err != nil {
					return routery.AbortResult[routery.BasicKind, routery.BasicReason, struct{}](), err
				}

				return routery.BasicHandled(struct{}{}), nil
			},
		)
		wrapped := routery.ApplyRoute(
			base,
			routery.RetryIf[any, routery.BasicKind, routery.BasicReason, struct{}](attempts, backoff, pred),
		)
		_, err := routery.InvokeRouteHandler(ctx, req, wrapped)
		return err
	}
}

// RetryStreamInterceptor retries the initial [grpc.Streamer] call that creates the client stream.
func RetryStreamInterceptor(opts InterceptorOptions) grpc.StreamClientInterceptor {
	attempts, backoff, _ := normalizeInterceptorOpts(opts)
	streamPred := routery.RetryPredicate[struct{}](func(ctx context.Context, _ struct{}, err error) bool {
		if opts.Predicate != nil {
			return opts.Predicate(ctx, nil, err)
		}
		return DefaultRetryPolicy[any](ctx, nil, err)
	})

	return func(
		ctx context.Context,
		desc *grpc.StreamDesc,
		cc *grpc.ClientConn,
		method string,
		streamer grpc.Streamer,
		callOpts ...grpc.CallOption,
	) (grpc.ClientStream, error) {
		streamHandler := routery.BasicRouteHandler[struct{}, grpc.ClientStream](
			func(call routery.RouteCall[struct{}]) (routery.BasicRouteResult[grpc.ClientStream], error) {
				stream, err := streamer(call.Context, desc, cc, method, callOpts...)
				if err != nil {
					return routery.AbortResult[routery.BasicKind, routery.BasicReason, grpc.ClientStream](), err
				}

				return routery.BasicHandled(stream), nil
			},
		)
		retry := routery.RetryIf[
			struct{},
			routery.BasicKind,
			routery.BasicReason,
			grpc.ClientStream,
		](attempts, backoff, streamPred)
		wrapped := routery.ApplyRoute(streamHandler, retry)
		result, err := routery.InvokeRouteHandler(ctx, struct{}{}, wrapped)
		if err != nil {
			return nil, err
		}

		return clientStreamFromResult(result)
	}
}

func clientStreamFromResult(result routery.BasicRouteResult[grpc.ClientStream]) (grpc.ClientStream, error) {
	if !result.HasPayload {
		return nil, configError("stream interceptor did not return payload")
	}

	return result.Payload, nil
}

func normalizeInterceptorOpts(opts InterceptorOptions) (int, time.Duration, routery.RetryPredicate[any]) {
	attempts := max(opts.Attempts, 1)
	backoff := max(opts.Backoff, 0)
	pred := opts.Predicate
	if pred == nil {
		pred = DefaultRetryPolicy[any]
	}

	return attempts, backoff, pred
}
