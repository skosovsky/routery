package routerygrpc

import (
	"context"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/skosovsky/routery"
)

const bufSize = 1024 * 1024

func TestRetryUnaryInterceptorRetriesUnavailable(t *testing.T) {
	t.Parallel()

	lis := bufconn.Listen(bufSize)
	var calls atomic.Int32
	srv := grpc.NewServer()
	grpc_testing.RegisterTestServiceServer(srv, &flakyEmptyServer{calls: &calls})
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
			Attempts: 4,
			Backoff:  time.Millisecond,
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	client := grpc_testing.NewTestServiceClient(conn)
	_, err = client.EmptyCall(ctx, &grpc_testing.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatalf("want 3 server attempts, got %d", calls.Load())
	}
}

func TestRetryStreamInterceptorOK(t *testing.T) {
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
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	client := grpc_testing.NewTestServiceClient(conn)
	stream, err := client.StreamingOutputCall(ctx, &grpc_testing.StreamingOutputCallRequest{})
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.CloseSend()
}

func TestRetryStreamInterceptorMissingPayload(t *testing.T) {
	t.Parallel()

	rec := routery.NewResultRecorder[grpc.ClientStream]()
	_, err := clientStreamFromRecorder(rec)
	if err == nil {
		t.Fatal("expected error when recorder has no payload")
	}
	if !strings.Contains(err.Error(), "did not record payload") {
		t.Fatalf("got err %v", err)
	}
}

type flakyEmptyServer struct {
	grpc_testing.UnimplementedTestServiceServer

	calls *atomic.Int32
}

func (s *flakyEmptyServer) EmptyCall(context.Context, *grpc_testing.Empty) (*grpc_testing.Empty, error) {
	if s.calls.Add(1) < 3 {
		return nil, status.Error(codes.Unavailable, "retry")
	}
	return &grpc_testing.Empty{}, nil
}

type streamOKServer struct {
	grpc_testing.UnimplementedTestServiceServer
}

func (s *streamOKServer) StreamingOutputCall(
	_ *grpc_testing.StreamingOutputCallRequest,
	stream grpc.ServerStreamingServer[grpc_testing.StreamingOutputCallResponse],
) error {
	return stream.Send(&grpc_testing.StreamingOutputCallResponse{})
}
