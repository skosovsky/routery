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
// If opts.Predicate is nil, [DefaultRetryPolicy] is used (req is the RPC request value).
func RetryUnaryInterceptor(opts InterceptorOptions) grpc.UnaryClientInterceptor {
	attempts, backoff, pred := normalizeInterceptorOpts(opts)

	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, callOpts ...grpc.CallOption) error {
		base := routery.HandlerFunc[any, struct{}](
			func(ctx context.Context, reqVal any) (routery.RouteResult[struct{}], error) {
				err := invoker(ctx, method, reqVal, reply, cc, callOpts...)
				if err != nil {
					return routery.RouteResult[struct{}]{}, err
				}

				return routery.Handled(struct{}{}), nil
			},
		)
		wrapped := routery.RetryIf[any, struct{}](attempts, backoff, pred)(base)
		_, err := wrapped.Handle(ctx, req)
		return err
	}
}

// RetryStreamInterceptor retries the initial [grpc.Streamer] call that creates the client stream.
// If opts.Predicate is nil, [DefaultRetryPolicy][struct{}] is used with an empty request (only
// error classification via status/EOF applies).
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
		streamHandler := routery.HandlerFunc[struct{}, grpc.ClientStream](
			func(ctx context.Context, _ struct{}) (routery.RouteResult[grpc.ClientStream], error) {
				stream, err := streamer(ctx, desc, cc, method, callOpts...)
				if err != nil {
					return routery.RouteResult[grpc.ClientStream]{}, err
				}

				return routery.Handled(stream), nil
			},
		)
		wrapped := routery.RetryIf[struct{}, grpc.ClientStream](attempts, backoff, streamPred)(streamHandler)
		result, err := wrapped.Handle(ctx, struct{}{})
		if err != nil {
			return nil, err
		}

		return result.Payload, nil
	}
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
