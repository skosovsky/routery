package routerykafka

import (
	"context"
	"fmt"

	"github.com/segmentio/kafka-go"

	"github.com/skosovsky/routery"
)

// PublishRequest is the request type for [NewProducerRouteHandler].
type PublishRequest struct {
	Messages []kafka.Message
}

// MessageWriter is the subset of producer APIs supported by [NewProducerRouteHandler].
type MessageWriter interface {
	WriteMessages(ctx context.Context, msgs ...kafka.Message) error
}

// NewProducerRouteHandler wraps w so each dispatch calls [MessageWriter.WriteMessages].
func NewProducerRouteHandler(w MessageWriter) routery.RouteHandler[PublishRequest, struct{}] {
	if w == nil {
		return invalidRouteHandler(configError("kafka writer is nil"))
	}

	return func(ctx context.Context, req PublishRequest, rec routery.ResultRecorder[struct{}]) error {
		if len(req.Messages) == 0 {
			rec.Stop(struct{}{}, "")
			return nil
		}
		if err := w.WriteMessages(ctx, req.Messages...); err != nil {
			return err
		}

		rec.Stop(struct{}{}, "")
		return nil
	}
}

func configError(detail string) error {
	return fmt.Errorf("%w: %s", routery.ErrInvalidConfig, detail)
}

func invalidRouteHandler(err error) routery.RouteHandler[PublishRequest, struct{}] {
	return func(context.Context, PublishRequest, routery.ResultRecorder[struct{}]) error {
		return err
	}
}
