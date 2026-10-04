package routerykafka

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"testing"

	"github.com/segmentio/kafka-go"

	"github.com/skosovsky/routery"
)

func TestPublishPreservesIndexedBatchFacts(t *testing.T) {
	t.Parallel()
	for _, wrapped := range []bool{false, true} {
		t.Run(strconv.FormatBool(wrapped), func(t *testing.T) {
			t.Parallel()
			// Arrange.
			unknown := errors.New("lost outcome")
			batch := kafka.WriteErrors{nil, kafka.MessageSizeTooLarge, kafka.RequestTimedOut, unknown}
			var providerErr error = batch
			if wrapped {
				providerErr = fmt.Errorf("batch: %w", batch)
			}
			writer := &fakeWriter{err: providerErr}
			handler := routery.ApplyRoute(
				NewProducerRouteHandler(writer),
				routery.RetryIf[PublishRequest, routery.BasicKind, routery.BasicReason, PublishResult](
					3,
					0,
					RetryPolicy(replayEvidence[PublishRequest](true, 0)),
				),
			)
			// Act.
			result, err := routery.InvokeRouteHandler(
				context.Background(),
				PublishRequest{Messages: make([]kafka.Message, 4)},
				handler,
			)
			// Assert: even blanket host duplicate permission does not override mixed batch classification.
			if _, ok := errors.AsType[kafka.WriteErrors](err); !ok || !result.HasPayload || writer.calls.Load() != 1 {
				t.Fatalf("error=%v result=%+v calls=%d", err, result, writer.calls.Load())
			}
			for index, want := range []MessageState{Acknowledged, PermanentFailure, TransientFailure, Unknown} {
				fact := result.Payload.Messages[index]
				if fact.Index != index || fact.State != want || !errors.Is(fact.Err, batch[index]) {
					t.Fatalf("index=%d fact=%+v", index, fact)
				}
			}
		})
	}
}
func TestMalformedBatchAndSingleErrors(t *testing.T) {
	t.Parallel()
	// Arrange/Act/Assert.
	for _, err := range []error{kafka.WriteErrors{nil}, io.EOF, context.Canceled, fmt.Errorf("wrapped: %w", kafka.MessageSizeTooLarge)} {
		result := publishResult(2, err)
		for _, fact := range result.Messages {
			if fact.State == Acknowledged || fact.Err == nil {
				t.Fatalf("invented acknowledgment: %+v", fact)
			}
		}
	}
}
func TestConcreteWriterRejectsEnqueueAndNoAck(t *testing.T) {
	t.Parallel()
	for _, writer := range []*kafka.Writer{{Async: true, RequiredAcks: kafka.RequireAll}, {RequiredAcks: kafka.RequireNone}, nil} {
		// Arrange.
		handler := NewProducerRouteHandler(writer)
		// Act.
		result, err := routery.InvokeRouteHandler(
			context.Background(),
			PublishRequest{Messages: []kafka.Message{{}}},
			handler,
		)
		// Assert: no network/config call, no inferred delivery from enqueue.
		if !errors.Is(err, routery.ErrInvalidConfig) || result.HasPayload {
			t.Fatalf("error=%v result=%+v", err, result)
		}
	}
}
func TestMessageBuffersAreIndependentAfterCancelledWriter(t *testing.T) {
	t.Parallel()
	// Arrange.
	messages := []kafka.Message{
		{Key: []byte("key"), Value: []byte("value"), Headers: []kafka.Header{{Key: "header", Value: []byte("data")}}},
	}
	// Act: model the SDK retaining copies after a cancelled WriteMessages return.
	copied := cloneMessages(messages)
	messages[0].Key[0] = 'X'
	messages[0].Value[0] = 'X'
	messages[0].Headers[0].Value[0] = 'X'
	// Assert.
	if string(copied[0].Key) != "key" || string(copied[0].Value) != "value" ||
		string(copied[0].Headers[0].Value) != "data" {
		t.Fatal("SDK buffers alias host")
	}
}
