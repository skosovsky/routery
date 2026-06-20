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
func NewProducerRouteHandler(w MessageWriter) routery.BasicRouteHandler[PublishRequest, struct{}] {
	if w == nil {
		return invalidRouteHandler(configError("kafka writer is nil"))
	}

	return func(call routery.RouteCall[PublishRequest]) (routery.BasicRouteResult[struct{}], error) {
		if len(call.Request.Messages) == 0 {
			return routery.BasicHandled(struct{}{}), nil
		}
		if err := w.WriteMessages(call.Context, call.Request.Messages...); err != nil {
			return routery.AbortResult[routery.BasicKind, routery.BasicReason, struct{}](), err
		}

		return routery.BasicHandled(struct{}{}), nil
	}
}

func configError(detail string) error {
	return fmt.Errorf("%w: %s", routery.ErrInvalidConfig, detail)
}

func invalidRouteHandler(err error) routery.BasicRouteHandler[PublishRequest, struct{}] {
	return func(routery.RouteCall[PublishRequest]) (routery.BasicRouteResult[struct{}], error) {
		return routery.AbortResult[routery.BasicKind, routery.BasicReason, struct{}](), err
	}
}
