package routerymongo

import "go.mongodb.org/mongo-driver/mongo/options"

// FindRequest parameterizes [FindRunner.Find].
type FindRequest struct {
	Filter  any
	Options *options.FindOptions
}

// InsertOneRequest parameterizes [InsertOneRunner.InsertOne].
type InsertOneRequest struct {
	Document any
	Options  *options.InsertOneOptions
}

// UpdateOneRequest parameterizes [UpdateOneRunner.UpdateOne].
type UpdateOneRequest struct {
	Filter  any
	Update  any
	Options *options.UpdateOptions
}

// DeleteOneRequest parameterizes [DeleteOneRunner.DeleteOne].
type DeleteOneRequest struct {
	Filter  any
	Options *options.DeleteOptions
}
