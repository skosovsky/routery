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

// NewFindExecutor wraps coll.Find.
func NewFindExecutor(coll FindRunner) routery.Executor[FindRequest, *mongo.Cursor] {
	if coll == nil {
		return invalidFindExecutor(nilCollectionError())
	}
	return routery.ExecutorFunc[FindRequest, *mongo.Cursor](
		func(ctx context.Context, req FindRequest) (*mongo.Cursor, error) {
			var opts []*options.FindOptions
			if req.Options != nil {
				opts = append(opts, req.Options)
			}
			return coll.Find(ctx, req.Filter, opts...)
		},
	)
}

// NewInsertOneExecutor wraps coll.InsertOne.
func NewInsertOneExecutor(coll InsertOneRunner) routery.Executor[InsertOneRequest, *mongo.InsertOneResult] {
	if coll == nil {
		return invalidInsertExecutor(nilCollectionError())
	}
	return routery.ExecutorFunc[InsertOneRequest, *mongo.InsertOneResult](
		func(ctx context.Context, req InsertOneRequest) (*mongo.InsertOneResult, error) {
			var opts []*options.InsertOneOptions
			if req.Options != nil {
				opts = append(opts, req.Options)
			}
			return coll.InsertOne(ctx, req.Document, opts...)
		},
	)
}

// NewUpdateOneExecutor wraps coll.UpdateOne.
func NewUpdateOneExecutor(coll UpdateOneRunner) routery.Executor[UpdateOneRequest, *mongo.UpdateResult] {
	if coll == nil {
		return invalidUpdateExecutor(nilCollectionError())
	}
	return routery.ExecutorFunc[UpdateOneRequest, *mongo.UpdateResult](
		func(ctx context.Context, req UpdateOneRequest) (*mongo.UpdateResult, error) {
			var opts []*options.UpdateOptions
			if req.Options != nil {
				opts = append(opts, req.Options)
			}
			return coll.UpdateOne(ctx, req.Filter, req.Update, opts...)
		},
	)
}

// NewDeleteOneExecutor wraps coll.DeleteOne.
func NewDeleteOneExecutor(coll DeleteOneRunner) routery.Executor[DeleteOneRequest, *mongo.DeleteResult] {
	if coll == nil {
		return invalidDeleteExecutor(nilCollectionError())
	}
	return routery.ExecutorFunc[DeleteOneRequest, *mongo.DeleteResult](
		func(ctx context.Context, req DeleteOneRequest) (*mongo.DeleteResult, error) {
			var opts []*options.DeleteOptions
			if req.Options != nil {
				opts = append(opts, req.Options)
			}
			return coll.DeleteOne(ctx, req.Filter, opts...)
		},
	)
}

func nilCollectionError() error {
	return fmt.Errorf("%w: collection is nil", routery.ErrInvalidConfig)
}

func invalidFindExecutor(err error) routery.Executor[FindRequest, *mongo.Cursor] {
	return routery.ExecutorFunc[FindRequest, *mongo.Cursor](
		func(context.Context, FindRequest) (*mongo.Cursor, error) {
			return nil, err
		},
	)
}

func invalidInsertExecutor(err error) routery.Executor[InsertOneRequest, *mongo.InsertOneResult] {
	return routery.ExecutorFunc[InsertOneRequest, *mongo.InsertOneResult](
		func(context.Context, InsertOneRequest) (*mongo.InsertOneResult, error) {
			return nil, err
		},
	)
}

func invalidUpdateExecutor(err error) routery.Executor[UpdateOneRequest, *mongo.UpdateResult] {
	return routery.ExecutorFunc[UpdateOneRequest, *mongo.UpdateResult](
		func(context.Context, UpdateOneRequest) (*mongo.UpdateResult, error) {
			return nil, err
		},
	)
}

func invalidDeleteExecutor(err error) routery.Executor[DeleteOneRequest, *mongo.DeleteResult] {
	return routery.ExecutorFunc[DeleteOneRequest, *mongo.DeleteResult](
		func(context.Context, DeleteOneRequest) (*mongo.DeleteResult, error) {
			return nil, err
		},
	)
}
