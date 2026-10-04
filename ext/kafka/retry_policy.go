package routerykafka

import (
	"context"
	"errors"
	"io"
	"net"

	"github.com/skosovsky/routery"
	"github.com/skosovsky/routery/policy/attempt"

	"github.com/segmentio/kafka-go"
)

// IsTransientError classifies provider errors without authorizing repetition.
// Replay safety must be provided separately through RetryPolicy or execution.Sequence.
func IsTransientError(err error) bool {
	if err == nil || errors.Is(err, routery.ErrInvalidConfig) {
		return false
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	if batch, ok := errors.AsType[kafka.WriteErrors](err); ok {
		return allTransient(batch)
	}
	if ke, ok := errors.AsType[kafka.Error](err); ok {
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

// RetryPolicy requires explicit host evidence; nil evidence denies repetition.
func RetryPolicy[Req any](evidence attempt.Evidence[Req]) routery.RetryPredicate[Req] {
	predicate := attempt.RetryPredicate(IsTransientError, evidence)
	return func(ctx context.Context, req Req, err error) bool { return predicate(ctx, req, err) }
}

func allTransient(batch kafka.WriteErrors) bool {
	if len(batch) == 0 {
		return false
	}
	for _, err := range batch {
		if err == nil || !IsTransientError(err) {
			return false
		}
	}
	return true
}
