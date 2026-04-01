package routeryredis

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestDefaultRetryPolicy(t *testing.T) {
	t.Parallel()

	type row struct {
		name string
		err  error
		want bool
	}

	tests := []row{
		{"nil", nil, false},
		{"canceled", context.Canceled, false},
		{"deadline", context.DeadlineExceeded, false},
		{"redis_nil", redis.Nil, false},
		{"tx_failed", redis.TxFailedErr, true},
		{"timeout", &net.OpError{Err: timeoutError{}}, true},
		{"eof", io.EOF, true},
		{"connection_reset", errors.New("read tcp: connection reset by peer"), true},
		{"broken_pipe", errors.New("write: broken pipe"), true},
		{"noauth", errors.New("NOAUTH Authentication required"), false},
		{"wrongpass", errors.New("WRONGPASS invalid username-password"), false},
		{"noperm", errors.New("NOPERM this user has no permissions"), false},
		{"syntax", errors.New("ERR syntax error"), false},
		{"wrongtype", errors.New("WRONGTYPE Operation against a key holding the wrong kind of value"), false},
		{"unknown_cmd", errors.New("ERR unknown command 'foo'"), false},
		{"generic", errors.New("some other error"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := DefaultRetryPolicy[struct{}](context.Background(), struct{}{}, tc.err)
			if got != tc.want {
				t.Fatalf("retry=%v, want %v for err=%v", got, tc.want, tc.err)
			}
		})
	}
}

type timeoutError struct{}

func (timeoutError) Error() string { return "i/o timeout" }

func (timeoutError) Timeout() bool { return true }

func (timeoutError) Temporary() bool { return true }

func FuzzDefaultRetryPolicyNoPanics(f *testing.F) {
	f.Add("")
	f.Fuzz(func(t *testing.T, s string) {
		t.Helper()
		_ = DefaultRetryPolicy[int](context.Background(), 0, errors.New(s))
	})
}
