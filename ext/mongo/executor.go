package routerymongo

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/skosovsky/routery"
)

// FindRunner matches [*mongo.Collection.Find].
type FindRunner interface {
	Find(ctx context.Context, filter any, opts ...*options.FindOptions) (*mongo.Cursor, error)
}

// InsertOneRunner matches [*mongo.Collection.InsertOne].
type InsertOneRunner interface {
	InsertOne(ctx context.Context, document any, opts ...*options.InsertOneOptions) (*mongo.InsertOneResult, error)
}

// UpdateOneRunner matches [*mongo.Collection.UpdateOne].
type UpdateOneRunner interface {
	UpdateOne(ctx context.Context, filter any, update any, opts ...*options.UpdateOptions) (*mongo.UpdateResult, error)
}

// DeleteOneRunner matches [*mongo.Collection.DeleteOne].
type DeleteOneRunner interface {
	DeleteOne(ctx context.Context, filter any, opts ...*options.DeleteOptions) (*mongo.DeleteResult, error)
}

// NewFindHandler wraps coll.Find.
func NewFindHandler(coll FindRunner) routery.Handler[FindRequest, *mongo.Cursor] {
	if coll == nil {
		return invalidFindHandler(nilCollectionError())
	}
	return routery.HandlerFunc[FindRequest, *mongo.Cursor](
		func(ctx context.Context, req FindRequest) (routery.RouteResult[*mongo.Cursor], error) {
			var opts []*options.FindOptions
			if req.Options != nil {
				opts = append(opts, req.Options)
			}
			cursor, err := coll.Find(ctx, req.Filter, opts...)
			if err != nil {
				return routery.RouteResult[*mongo.Cursor]{}, err
			}

			return routery.Handled(cursor), nil
		},
	)
}

// NewInsertOneHandler wraps coll.InsertOne.
func NewInsertOneHandler(coll InsertOneRunner) routery.Handler[InsertOneRequest, *mongo.InsertOneResult] {
	if coll == nil {
		return invalidInsertHandler(nilCollectionError())
	}
	return routery.HandlerFunc[InsertOneRequest, *mongo.InsertOneResult](
		func(ctx context.Context, req InsertOneRequest) (routery.RouteResult[*mongo.InsertOneResult], error) {
			var opts []*options.InsertOneOptions
			if req.Options != nil {
				opts = append(opts, req.Options)
			}
			result, err := coll.InsertOne(ctx, req.Document, opts...)
			if err != nil {
				return routery.RouteResult[*mongo.InsertOneResult]{}, err
			}

			return routery.Handled(result), nil
		},
	)
}

// NewUpdateOneHandler wraps coll.UpdateOne.
func NewUpdateOneHandler(coll UpdateOneRunner) routery.Handler[UpdateOneRequest, *mongo.UpdateResult] {
	if coll == nil {
		return invalidUpdateHandler(nilCollectionError())
	}
	return routery.HandlerFunc[UpdateOneRequest, *mongo.UpdateResult](
		func(ctx context.Context, req UpdateOneRequest) (routery.RouteResult[*mongo.UpdateResult], error) {
			var opts []*options.UpdateOptions
			if req.Options != nil {
				opts = append(opts, req.Options)
			}
			result, err := coll.UpdateOne(ctx, req.Filter, req.Update, opts...)
			if err != nil {
				return routery.RouteResult[*mongo.UpdateResult]{}, err
			}

			return routery.Handled(result), nil
		},
	)
}

// NewDeleteOneHandler wraps coll.DeleteOne.
func NewDeleteOneHandler(coll DeleteOneRunner) routery.Handler[DeleteOneRequest, *mongo.DeleteResult] {
	if coll == nil {
		return invalidDeleteHandler(nilCollectionError())
	}
	return routery.HandlerFunc[DeleteOneRequest, *mongo.DeleteResult](
		func(ctx context.Context, req DeleteOneRequest) (routery.RouteResult[*mongo.DeleteResult], error) {
			var opts []*options.DeleteOptions
			if req.Options != nil {
				opts = append(opts, req.Options)
			}
			result, err := coll.DeleteOne(ctx, req.Filter, opts...)
			if err != nil {
				return routery.RouteResult[*mongo.DeleteResult]{}, err
			}

			return routery.Handled(result), nil
		},
	)
}

func nilCollectionError() error {
	return fmt.Errorf("%w: collection is nil", routery.ErrInvalidConfig)
}

func invalidFindHandler(err error) routery.Handler[FindRequest, *mongo.Cursor] {
	return routery.HandlerFunc[FindRequest, *mongo.Cursor](
		func(context.Context, FindRequest) (routery.RouteResult[*mongo.Cursor], error) {
			return routery.RouteResult[*mongo.Cursor]{}, err
		},
	)
}

func invalidInsertHandler(err error) routery.Handler[InsertOneRequest, *mongo.InsertOneResult] {
	return routery.HandlerFunc[InsertOneRequest, *mongo.InsertOneResult](
		func(context.Context, InsertOneRequest) (routery.RouteResult[*mongo.InsertOneResult], error) {
			return routery.RouteResult[*mongo.InsertOneResult]{}, err
		},
	)
}

func invalidUpdateHandler(err error) routery.Handler[UpdateOneRequest, *mongo.UpdateResult] {
	return routery.HandlerFunc[UpdateOneRequest, *mongo.UpdateResult](
		func(context.Context, UpdateOneRequest) (routery.RouteResult[*mongo.UpdateResult], error) {
			return routery.RouteResult[*mongo.UpdateResult]{}, err
		},
	)
}

func invalidDeleteHandler(err error) routery.Handler[DeleteOneRequest, *mongo.DeleteResult] {
	return routery.HandlerFunc[DeleteOneRequest, *mongo.DeleteResult](
		func(context.Context, DeleteOneRequest) (routery.RouteResult[*mongo.DeleteResult], error) {
			return routery.RouteResult[*mongo.DeleteResult]{}, err
		},
	)
}
