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
		base := routery.RouteHandler[any, struct{}](
			func(ctx context.Context, reqVal any, rec routery.ResultRecorder[struct{}]) error {
				err := invoker(ctx, method, reqVal, reply, cc, callOpts...)
				if err != nil {
					return err
				}

				rec.Stop(struct{}{}, "")
				return nil
			},
		)
		wrapped := routery.ApplyRoute(base, routery.RetryIf[any, struct{}](attempts, backoff, pred))
		rec := routery.NewResultRecorder[struct{}]()
		return wrapped(ctx, req, rec)
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
		streamHandler := routery.RouteHandler[struct{}, grpc.ClientStream](
			func(ctx context.Context, _ struct{}, rec routery.ResultRecorder[grpc.ClientStream]) error {
				stream, err := streamer(ctx, desc, cc, method, callOpts...)
				if err != nil {
					return err
				}

				rec.Stop(stream, "")
				return nil
			},
		)
		wrapped := routery.ApplyRoute(
			streamHandler,
			routery.RetryIf[struct{}, grpc.ClientStream](attempts, backoff, streamPred),
		)
		rec := routery.NewResultRecorder[grpc.ClientStream]()
		if err := wrapped(ctx, struct{}{}, rec); err != nil {
			return nil, err
		}

		return clientStreamFromRecorder(rec)
	}
}

func clientStreamFromRecorder(rec routery.ResultRecorder[grpc.ClientStream]) (grpc.ClientStream, error) {
	stream, ok := rec.Payload()
	if !ok {
		return nil, configError("stream interceptor did not record payload")
	}

	return stream, nil
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
