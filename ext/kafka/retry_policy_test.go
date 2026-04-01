package routerykafka

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"

	"github.com/segmentio/kafka-go"
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
		{"leader_na", kafka.LeaderNotAvailable, true},
		{"not_enough", kafka.NotEnoughReplicas, true},
		{"request_timeout", kafka.RequestTimedOut, true},
		{"unknown_topic", kafka.UnknownTopicOrPartition, false},
		{"msg_too_large", kafka.MessageSizeTooLarge, false},
		{"invalid_msg", kafka.InvalidMessage, false},
		{"topic_auth", kafka.TopicAuthorizationFailed, false},
		{"eof", io.EOF, true},
		{"timeout_net", &net.OpError{Err: kafkaTimeoutError{}}, true},
		{"plain", errors.New("other"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := DefaultRetryPolicy[struct{}](context.Background(), struct{}{}, tc.err)
			if got != tc.want {
				t.Fatalf("got %v want %v for %v", got, tc.want, tc.err)
			}
		})
	}
}

type kafkaTimeoutError struct{}

func (kafkaTimeoutError) Error() string { return "timeout" }

func (kafkaTimeoutError) Timeout() bool { return true }

func (kafkaTimeoutError) Temporary() bool { return true }

func FuzzDefaultRetryPolicyNoPanics(f *testing.F) {
	f.Add(int32(0))
	f.Fuzz(func(t *testing.T, code int32) {
		t.Helper()
		e := kafka.Error(code % 120)
		_ = DefaultRetryPolicy[int](context.Background(), 0, e)
	})
}
