package routerykafka_test

import (
	"context"
	"fmt"

	"github.com/segmentio/kafka-go"

	"github.com/skosovsky/routery"
	routerykafka "github.com/skosovsky/routery/ext/kafka"
)

type acknowledgedFixtureWriter struct{}

func (acknowledgedFixtureWriter) WriteMessages(ctx context.Context, msgs ...kafka.Message) error {
	_ = ctx
	if len(msgs) == 0 {
		return nil
	}
	fmt.Println(string(msgs[0].Value))
	return nil
}

func ExampleNewProducerRouteHandler_withRetryIf() {
	base := routerykafka.NewProducerRouteHandler(acknowledgedFixtureWriter{})

	handler := routery.ApplyRoute(
		base,
		routery.RetryIf[routerykafka.PublishRequest, routery.BasicKind, routery.BasicReason, routerykafka.PublishResult](
			2,
			0,
			routerykafka.RetryPolicy[routerykafka.PublishRequest](nil),
		),
	)

	outcome, err := routery.InvokeRouteHandler(context.Background(), routerykafka.PublishRequest{
		Messages: []kafka.Message{{Value: []byte("ok")}},
	}, handler)
	if err != nil {
		fmt.Println("err", err)
		return
	}
	if outcome.HasPayload {
		fmt.Println("acknowledged")
	}
	// Output: ok
	// acknowledged
}
