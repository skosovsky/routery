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

// NewFindRouteHandler wraps coll.Find.
func NewFindRouteHandler(coll FindRunner) routery.RouteHandler[FindRequest, *mongo.Cursor] {
	if coll == nil {
		return invalidFindRouteHandler(nilCollectionError())
	}

	return func(ctx context.Context, req FindRequest, rec routery.ResultRecorder[*mongo.Cursor]) error {
		var opts []*options.FindOptions
		if req.Options != nil {
			opts = append(opts, req.Options)
		}
		cursor, err := coll.Find(ctx, req.Filter, opts...)
		if err != nil {
			return err
		}

		rec.Stop(cursor, "")
		return nil
	}
}

// NewInsertOneRouteHandler wraps coll.InsertOne.
func NewInsertOneRouteHandler(coll InsertOneRunner) routery.RouteHandler[InsertOneRequest, *mongo.InsertOneResult] {
	if coll == nil {
		return invalidInsertRouteHandler(nilCollectionError())
	}

	return func(ctx context.Context, req InsertOneRequest, rec routery.ResultRecorder[*mongo.InsertOneResult]) error {
		var opts []*options.InsertOneOptions
		if req.Options != nil {
			opts = append(opts, req.Options)
		}
		result, err := coll.InsertOne(ctx, req.Document, opts...)
		if err != nil {
			return err
		}

		rec.Stop(result, "")
		return nil
	}
}

// NewUpdateOneRouteHandler wraps coll.UpdateOne.
func NewUpdateOneRouteHandler(coll UpdateOneRunner) routery.RouteHandler[UpdateOneRequest, *mongo.UpdateResult] {
	if coll == nil {
		return invalidUpdateRouteHandler(nilCollectionError())
	}

	return func(ctx context.Context, req UpdateOneRequest, rec routery.ResultRecorder[*mongo.UpdateResult]) error {
		var opts []*options.UpdateOptions
		if req.Options != nil {
			opts = append(opts, req.Options)
		}
		result, err := coll.UpdateOne(ctx, req.Filter, req.Update, opts...)
		if err != nil {
			return err
		}

		rec.Stop(result, "")
		return nil
	}
}

// NewDeleteOneRouteHandler wraps coll.DeleteOne.
func NewDeleteOneRouteHandler(coll DeleteOneRunner) routery.RouteHandler[DeleteOneRequest, *mongo.DeleteResult] {
	if coll == nil {
		return invalidDeleteRouteHandler(nilCollectionError())
	}

	return func(ctx context.Context, req DeleteOneRequest, rec routery.ResultRecorder[*mongo.DeleteResult]) error {
		var opts []*options.DeleteOptions
		if req.Options != nil {
			opts = append(opts, req.Options)
		}
		result, err := coll.DeleteOne(ctx, req.Filter, opts...)
		if err != nil {
			return err
		}

		rec.Stop(result, "")
		return nil
	}
}

func nilCollectionError() error {
	return fmt.Errorf("%w: collection is nil", routery.ErrInvalidConfig)
}

func invalidFindRouteHandler(err error) routery.RouteHandler[FindRequest, *mongo.Cursor] {
	return func(context.Context, FindRequest, routery.ResultRecorder[*mongo.Cursor]) error {
		return err
	}
}

func invalidInsertRouteHandler(err error) routery.RouteHandler[InsertOneRequest, *mongo.InsertOneResult] {
	return func(context.Context, InsertOneRequest, routery.ResultRecorder[*mongo.InsertOneResult]) error {
		return err
	}
}

func invalidUpdateRouteHandler(err error) routery.RouteHandler[UpdateOneRequest, *mongo.UpdateResult] {
	return func(context.Context, UpdateOneRequest, routery.ResultRecorder[*mongo.UpdateResult]) error {
		return err
	}
}

func invalidDeleteRouteHandler(err error) routery.RouteHandler[DeleteOneRequest, *mongo.DeleteResult] {
	return func(context.Context, DeleteOneRequest, routery.ResultRecorder[*mongo.DeleteResult]) error {
		return err
	}
}
