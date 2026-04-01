package routerykafka

import (
	"context"
	"fmt"

	"github.com/segmentio/kafka-go"

	"github.com/skosovsky/routery"
)

// PublishRequest is the request type for [NewProducerExecutor].
type PublishRequest struct {
	Messages []kafka.Message
}

// MessageWriter is the subset of producer APIs supported by [NewProducerExecutor].
// [*kafka.Writer] implements this interface.
type MessageWriter interface {
	WriteMessages(ctx context.Context, msgs ...kafka.Message) error
}

// NewProducerExecutor wraps w so each [routery.Executor.Execute] calls [MessageWriter.WriteMessages].
func NewProducerExecutor(w MessageWriter) routery.Executor[PublishRequest, struct{}] {
	if w == nil {
		return invalidExecutor(configError("kafka writer is nil"))
	}
	return routery.ExecutorFunc[PublishRequest, struct{}](
		func(ctx context.Context, req PublishRequest) (struct{}, error) {
			if len(req.Messages) == 0 {
				return struct{}{}, nil
			}
			return struct{}{}, w.WriteMessages(ctx, req.Messages...)
		},
	)
}

func configError(detail string) error {
	return fmt.Errorf("%w: %s", routery.ErrInvalidConfig, detail)
}

func invalidExecutor(err error) routery.Executor[PublishRequest, struct{}] {
	return routery.ExecutorFunc[PublishRequest, struct{}](func(context.Context, PublishRequest) (struct{}, error) {
		return struct{}{}, err
	})
}
