package routerykafka_test

import (
	"context"
	"fmt"

	"github.com/segmentio/kafka-go"

	"github.com/skosovsky/routery"
	routerykafka "github.com/skosovsky/routery/ext/kafka"
)

type stdoutWriter struct{}

func (stdoutWriter) WriteMessages(ctx context.Context, msgs ...kafka.Message) error {
	_ = ctx
	if len(msgs) == 0 {
		return nil
	}
	fmt.Println(string(msgs[0].Value))
	return nil
}

func ExampleNewProducerExecutor_withRetryIf() {
	base := routerykafka.NewProducerExecutor(stdoutWriter{})

	executor := routery.Apply(
		base,
		routery.RetryIf[routerykafka.PublishRequest, struct{}](
			2,
			0,
			routerykafka.DefaultRetryPolicy[routerykafka.PublishRequest],
		),
	)

	_, err := executor.Execute(context.Background(), routerykafka.PublishRequest{
		Messages: []kafka.Message{{Value: []byte("ok")}},
	})
	if err != nil {
		fmt.Println("err", err)
		return
	}
	// Output: ok
}
