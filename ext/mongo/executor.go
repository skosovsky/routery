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
func NewFindRouteHandler(coll FindRunner) routery.BasicRouteHandler[FindRequest, *mongo.Cursor] {
	if coll == nil {
		return invalidFindRouteHandler(nilCollectionError())
	}

	return func(call routery.RouteCall[FindRequest]) (routery.BasicRouteResult[*mongo.Cursor], error) {
		var opts []*options.FindOptions
		if call.Request.Options != nil {
			opts = append(opts, call.Request.Options)
		}
		cursor, err := coll.Find(call.Context, call.Request.Filter, opts...)
		if err != nil {
			return routery.AbortResult[routery.BasicKind, routery.BasicReason, *mongo.Cursor](), err
		}

		return routery.BasicHandled(cursor), nil
	}
}

// NewInsertOneRouteHandler wraps coll.InsertOne.
func NewInsertOneRouteHandler(
	coll InsertOneRunner,
) routery.BasicRouteHandler[InsertOneRequest, *mongo.InsertOneResult] {
	if coll == nil {
		return invalidInsertRouteHandler(nilCollectionError())
	}

	return func(call routery.RouteCall[InsertOneRequest]) (routery.BasicRouteResult[*mongo.InsertOneResult], error) {
		var opts []*options.InsertOneOptions
		if call.Request.Options != nil {
			opts = append(opts, call.Request.Options)
		}
		result, err := coll.InsertOne(call.Context, call.Request.Document, opts...)
		if err != nil {
			return routery.AbortResult[routery.BasicKind, routery.BasicReason, *mongo.InsertOneResult](), err
		}

		return routery.BasicHandled(result), nil
	}
}

// NewUpdateOneRouteHandler wraps coll.UpdateOne.
func NewUpdateOneRouteHandler(coll UpdateOneRunner) routery.BasicRouteHandler[UpdateOneRequest, *mongo.UpdateResult] {
	if coll == nil {
		return invalidUpdateRouteHandler(nilCollectionError())
	}

	return func(call routery.RouteCall[UpdateOneRequest]) (routery.BasicRouteResult[*mongo.UpdateResult], error) {
		var opts []*options.UpdateOptions
		if call.Request.Options != nil {
			opts = append(opts, call.Request.Options)
		}
		result, err := coll.UpdateOne(call.Context, call.Request.Filter, call.Request.Update, opts...)
		if err != nil {
			return routery.AbortResult[routery.BasicKind, routery.BasicReason, *mongo.UpdateResult](), err
		}

		return routery.BasicHandled(result), nil
	}
}

// NewDeleteOneRouteHandler wraps coll.DeleteOne.
func NewDeleteOneRouteHandler(coll DeleteOneRunner) routery.BasicRouteHandler[DeleteOneRequest, *mongo.DeleteResult] {
	if coll == nil {
		return invalidDeleteRouteHandler(nilCollectionError())
	}

	return func(call routery.RouteCall[DeleteOneRequest]) (routery.BasicRouteResult[*mongo.DeleteResult], error) {
		var opts []*options.DeleteOptions
		if call.Request.Options != nil {
			opts = append(opts, call.Request.Options)
		}
		result, err := coll.DeleteOne(call.Context, call.Request.Filter, opts...)
		if err != nil {
			return routery.AbortResult[routery.BasicKind, routery.BasicReason, *mongo.DeleteResult](), err
		}

		return routery.BasicHandled(result), nil
	}
}

func nilCollectionError() error {
	return fmt.Errorf("%w: collection is nil", routery.ErrInvalidConfig)
}

func invalidFindRouteHandler(err error) routery.BasicRouteHandler[FindRequest, *mongo.Cursor] {
	return func(routery.RouteCall[FindRequest]) (routery.BasicRouteResult[*mongo.Cursor], error) {
		return routery.AbortResult[routery.BasicKind, routery.BasicReason, *mongo.Cursor](), err
	}
}

func invalidInsertRouteHandler(err error) routery.BasicRouteHandler[InsertOneRequest, *mongo.InsertOneResult] {
	return func(routery.RouteCall[InsertOneRequest]) (routery.BasicRouteResult[*mongo.InsertOneResult], error) {
		return routery.AbortResult[routery.BasicKind, routery.BasicReason, *mongo.InsertOneResult](), err
	}
}

func invalidUpdateRouteHandler(err error) routery.BasicRouteHandler[UpdateOneRequest, *mongo.UpdateResult] {
	return func(routery.RouteCall[UpdateOneRequest]) (routery.BasicRouteResult[*mongo.UpdateResult], error) {
		return routery.AbortResult[routery.BasicKind, routery.BasicReason, *mongo.UpdateResult](), err
	}
}

func invalidDeleteRouteHandler(err error) routery.BasicRouteHandler[DeleteOneRequest, *mongo.DeleteResult] {
	return func(routery.RouteCall[DeleteOneRequest]) (routery.BasicRouteResult[*mongo.DeleteResult], error) {
		return routery.AbortResult[routery.BasicKind, routery.BasicReason, *mongo.DeleteResult](), err
	}
}
