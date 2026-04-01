package routerymongo_test

import (
	"context"
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/skosovsky/routery"
	routerymongo "github.com/skosovsky/routery/ext/mongo"
)

type noopFind struct{}

func (noopFind) Find(ctx context.Context, filter any, opts ...*options.FindOptions) (*mongo.Cursor, error) {
	_ = ctx
	_ = filter
	_ = opts
	return nil, errors.New("noop")
}

func ExampleNewFindExecutor_withRetryIf() {
	base := routerymongo.NewFindExecutor(noopFind{})
	executor := routery.Apply(
		base,
		routery.RetryIf[routerymongo.FindRequest, *mongo.Cursor](
			2,
			0,
			routerymongo.DefaultRetryPolicy[routerymongo.FindRequest],
		),
	)
	_, err := executor.Execute(context.Background(), routerymongo.FindRequest{Filter: map[string]any{}})
	fmt.Println(err != nil)
	// Output: true
}
