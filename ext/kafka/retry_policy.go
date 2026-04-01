package routerykafka

import (
	"context"
	"errors"
	"io"
	"net"

	"github.com/segmentio/kafka-go"
)

// DefaultRetryPolicy classifies Kafka producer errors for [github.com/skosovsky/routery.RetryIf].
func DefaultRetryPolicy[Req any](_ context.Context, _ Req, err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	var ke kafka.Error
	if errors.As(err, &ke) {
		switch ke {
		case kafka.MessageSizeTooLarge,
			kafka.UnknownTopicOrPartition,
			kafka.InvalidMessage,
			kafka.InvalidMessageSize,
			kafka.TopicAuthorizationFailed,
			kafka.ClusterAuthorizationFailed,
			kafka.InvalidTopic,
			kafka.RecordListTooLarge:
			return false
		default:
			if ke.Temporary() {
				return true
			}
			return false
		}
	}

	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}

	if errors.Is(err, io.EOF) {
		return true
	}

	return false
}
