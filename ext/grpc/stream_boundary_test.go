package routerygrpc

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type failedEstablishedStream struct {
	grpc.ClientStream

	reads atomic.Int32
}

func (stream *failedEstablishedStream) RecvMsg(any) error {
	stream.reads.Add(1)
	return status.Error(codes.Unavailable, "established stream lost")
}

func (*failedEstablishedStream) CloseSend() error { return nil }

func TestEstablishedStreamFailureNeverRestartsRPC(t *testing.T) {
	t.Parallel()
	// Arrange.
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stream := &failedEstablishedStream{}
	var starts, predicates atomic.Int32
	providedContext := make(chan context.Context, 1)
	interceptor := RetryStreamInterceptor(InterceptorOptions{
		Attempts:  3,
		Predicate: func(context.Context, any, error) bool { predicates.Add(1); return true },
	})
	streamer := func(ctx context.Context, _ *grpc.StreamDesc, _ *grpc.ClientConn, _ string,
		_ ...grpc.CallOption,
	) (grpc.ClientStream, error) {
		starts.Add(1)
		providedContext <- ctx
		return stream, nil
	}
	// Act.
	result, err := interceptor(ctx, nil, nil, "/operation", streamer)
	if err != nil {
		t.Fatal(err)
	}
	providerCtx := <-providedContext
	closeSendErr := result.CloseSend()
	readErr := result.RecvMsg(nil)
	// Assert.
	if closeSendErr != nil || status.Code(readErr) != codes.Unavailable || starts.Load() != 1 ||
		predicates.Load() != 0 ||
		stream.reads.Load() != 1 {
		t.Fatalf(
			"close=%v read=%v starts=%d predicates=%d reads=%d",
			closeSendErr,
			readErr,
			starts.Load(),
			predicates.Load(),
			stream.reads.Load(),
		)
	}
	if providerCtx.Err() != nil {
		t.Fatal("opening return or CloseSend cancelled receive lifetime")
	}
	cancel()
	if !errors.Is(providerCtx.Err(), context.Canceled) {
		t.Fatal("parent cancellation lost")
	}
}
