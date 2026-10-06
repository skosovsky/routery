package routerygrpc

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"

	"github.com/skosovsky/routery"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func TestRetryUnaryInterceptorNormalizesAttempts(t *testing.T) {
	t.Parallel()

	lis := bufconn.Listen(bufSize)
	var calls atomic.Int32
	srv := grpc.NewServer()
	grpc_testing.RegisterTestServiceServer(srv, &alwaysFailEmpty{calls: &calls})
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
	})

	ctx := context.Background()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(RetryUnaryInterceptor(InterceptorOptions{
			Attempts: 0,
			Backoff:  0,
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	client := grpc_testing.NewTestServiceClient(conn)
	_, err = client.EmptyCall(ctx, &grpc_testing.Empty{})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls.Load() != 1 {
		t.Fatalf("want 1 attempt (normalized), got %d", calls.Load())
	}
}

func TestRetryStreamInterceptorCustomPredicate(t *testing.T) {
	t.Parallel()

	lis := bufconn.Listen(bufSize)
	srv := grpc.NewServer()
	grpc_testing.RegisterTestServiceServer(srv, &streamOKServer{})
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(func() {
		srv.Stop()
		_ = lis.Close()
	})

	ctx := context.Background()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStreamInterceptor(RetryStreamInterceptor(InterceptorOptions{
			Attempts: 2,
			Backoff:  0,
			Predicate: func(ctx context.Context, req any, err error) bool {
				_ = ctx
				_ = req
				_ = err
				return false
			},
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	client := grpc_testing.NewTestServiceClient(conn)
	_, err = client.StreamingOutputCall(ctx, &grpc_testing.StreamingOutputCallRequest{})
	if err != nil {
		t.Fatal(err)
	}
}

type alwaysFailEmpty struct {
	grpc_testing.UnimplementedTestServiceServer

	calls *atomic.Int32
}

func (s *alwaysFailEmpty) EmptyCall(context.Context, *grpc_testing.Empty) (*grpc_testing.Empty, error) {
	s.calls.Add(1)
	return nil, status.Error(codes.InvalidArgument, "fail")
}

func TestNegativeInterceptorOptionsRejectBeforeDispatch(t *testing.T) {
	for _, options := range []InterceptorOptions{{Attempts: -1}, {Backoff: -1}} {
		// Arrange.
		calls := 0
		unary := RetryUnaryInterceptor(options)
		stream := RetryStreamInterceptor(options)
		// Act.
		err := unary(
			t.Context(),
			"method",
			nil,
			nil,
			nil,
			func(context.Context, string, any, any, *grpc.ClientConn, ...grpc.CallOption) error {
				calls++
				return nil
			},
		)
		_, streamErr := stream(
			t.Context(),
			nil,
			nil,
			"method",
			func(context.Context, *grpc.StreamDesc, *grpc.ClientConn, string, ...grpc.CallOption) (grpc.ClientStream, error) {
				calls++
				return nil, errors.New("unexpected transport invocation")
			},
		)
		// Assert.
		if !errors.Is(err, routery.ErrInvalidConfig) || !errors.Is(streamErr, routery.ErrInvalidConfig) || calls != 0 {
			t.Fatalf("calls=%d errors=%v %v", calls, err, streamErr)
		}
	}
}
