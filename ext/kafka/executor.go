package routerykafka

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/segmentio/kafka-go"

	"github.com/skosovsky/routery"
)

// PublishRequest is the request type for [NewProducerRouteHandler].
type PublishRequest struct {
	Messages []kafka.Message
}

// MessageWriter must be synchronous: nil means broker-acknowledged delivery.
// Messages and their byte slices are borrowed only until return. The host guarantees
// this for custom writers; hidden enqueue-only implementations cannot be detected.
type MessageWriter interface {
	WriteMessages(ctx context.Context, msgs ...kafka.Message) error
}

// NewProducerRouteHandler wraps w so each dispatch calls [MessageWriter.WriteMessages].
func NewProducerRouteHandler(w MessageWriter) routery.BasicRouteHandler[PublishRequest, PublishResult] {
	if w == nil {
		return invalidRouteHandler(configError("kafka writer is nil"))
	}

	if concrete, ok := w.(*kafka.Writer); ok &&
		(concrete == nil || concrete.Async || concrete.RequiredAcks == kafka.RequireNone) {
		return invalidRouteHandler(configError("writer must be synchronous with broker acknowledgements"))
	}
	return func(call routery.RouteCall[PublishRequest]) (routery.BasicRouteResult[PublishResult], error) {
		if len(call.Request.Messages) == 0 {
			return routery.BasicHandled(PublishResult{Messages: nil}), nil
		}
		err := w.WriteMessages(call.Context, cloneMessages(call.Request.Messages)...)
		result := publishResult(len(call.Request.Messages), err)
		return routery.BasicHandled(result), err
	}
}

func configError(detail string) error {
	return fmt.Errorf("%w: %s", routery.ErrInvalidConfig, detail)
}

func invalidRouteHandler(err error) routery.BasicRouteHandler[PublishRequest, PublishResult] {
	return func(routery.RouteCall[PublishRequest]) (routery.BasicRouteResult[PublishResult], error) {
		return routery.AbortResult[routery.BasicKind, routery.BasicReason, PublishResult](), err
	}
}

// MessageState separates broker acknowledgment from error classification.
// Failure states do not prove the absence of a remote effect.
type MessageState uint8

const (
	Unknown MessageState = iota
	Acknowledged
	PermanentFailure
	TransientFailure
)

// MessageResult retains the original index and error for host batch reconstruction.
type MessageResult struct {
	Index int
	State MessageState
	Err   error
}

// PublishResult preserves per-message facts, including partial batches on error.
type PublishResult struct{ Messages []MessageResult }

func publishResult(count int, err error) PublishResult {
	result := PublishResult{Messages: make([]MessageResult, count)}
	batch, isBatch := errors.AsType[kafka.WriteErrors](err)
	for index := range count {
		messageErr := err
		state := Unknown
		if err == nil {
			state = Acknowledged
		}
		if isBatch && len(batch) == count {
			messageErr = batch[index]
			if messageErr == nil {
				state = Acknowledged
			}
		}
		if messageErr != nil && (!isBatch || len(batch) == count) {
			if IsTransientError(messageErr) {
				state = TransientFailure
			} else if _, known := errors.AsType[kafka.Error](messageErr); known {
				state = PermanentFailure
			}
		}
		result.Messages[index] = MessageResult{Index: index, State: state, Err: messageErr}
	}
	return result
}

// Copy mutable SDK buffers because kafka.Writer may retain them after cancellation.
// WriterData remains host-owned and must outlive the writer Completion callback.
func cloneMessages(messages []kafka.Message) []kafka.Message {
	copied := make([]kafka.Message, len(messages))
	for index, message := range messages {
		copied[index] = message
		copied[index].Key = bytes.Clone(message.Key)
		copied[index].Value = bytes.Clone(message.Value)
		copied[index].Headers = make([]kafka.Header, len(message.Headers))
		for headerIndex, header := range message.Headers {
			copied[index].Headers[headerIndex] = kafka.Header{Key: header.Key, Value: bytes.Clone(header.Value)}
		}
	}
	return copied
}
