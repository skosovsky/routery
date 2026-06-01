package routerykafka

import (
	"context"
	"fmt"

	"github.com/segmentio/kafka-go"

	"github.com/skosovsky/routery"
)

// PublishRequest is the request type for [NewProducerHandler].
type PublishRequest struct {
	Messages []kafka.Message
}

// MessageWriter is the subset of producer APIs supported by [NewProducerHandler].
// [*kafka.Writer] implements this interface.
type MessageWriter interface {
	WriteMessages(ctx context.Context, msgs ...kafka.Message) error
}

// NewProducerHandler wraps w so each [routery.Handler.Handle] calls [MessageWriter.WriteMessages].
func NewProducerHandler(w MessageWriter) routery.Handler[PublishRequest, struct{}] {
	if w == nil {
		return invalidHandler(configError("kafka writer is nil"))
	}
	return routery.HandlerFunc[PublishRequest, struct{}](
		func(ctx context.Context, req PublishRequest) (routery.RouteResult[struct{}], error) {
			if len(req.Messages) == 0 {
				return routery.Handled(struct{}{}), nil
			}
			if err := w.WriteMessages(ctx, req.Messages...); err != nil {
				return routery.RouteResult[struct{}]{}, err
			}

			return routery.Handled(struct{}{}), nil
		},
	)
}

func configError(detail string) error {
	return fmt.Errorf("%w: %s", routery.ErrInvalidConfig, detail)
}

func invalidHandler(err error) routery.Handler[PublishRequest, struct{}] {
	return routery.HandlerFunc[PublishRequest, struct{}](
		func(context.Context, PublishRequest) (routery.RouteResult[struct{}], error) {
			return routery.RouteResult[struct{}]{}, err
		},
	)
}
