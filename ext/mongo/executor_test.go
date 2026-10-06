package routerymongo

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/skosovsky/routery"
)

type fakeFind struct {
	calls  atomic.Int32
	cursor *mongo.Cursor
	err    error
}

func (f *fakeFind) Find(ctx context.Context, filter any, opts ...*options.FindOptions) (*mongo.Cursor, error) {
	f.calls.Add(1)
	_ = ctx
	_ = filter
	_ = opts
	return f.cursor, f.err
}

func TestCursorResultHasExplicitOwnership(t *testing.T) {
	t.Parallel()
	// Arrange.
	cursor, err := mongo.NewCursorFromDocuments([]any{map[string]any{"value": "document"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	api := &fakeFind{cursor: cursor}
	var closed atomic.Int32
	// Act.
	result, err := routery.InvokeRouteHandler(context.Background(), FindRequest{},
		routery.FirstSuccessfulPayload(NewFindRouteHandler(api)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = result.Lifetime.Close() })
	// Assert.
	if result.Lifetime == nil || !cursor.Next(context.Background()) {
		t.Fatal("cursor winner is not owned/readable")
	}
	result.Lifetime.OnClose(func() { closed.Add(1) })
	if err := result.Lifetime.Close(); err != nil {
		t.Fatal(err)
	}
	if err := result.Lifetime.Close(); err != nil {
		t.Fatal(err)
	}
	if closed.Load() != 1 || cursor.Next(context.Background()) {
		t.Fatalf("cursor close callbacks=%d", closed.Load())
	}
}

func TestNewFindRouteHandlerNil(t *testing.T) {
	t.Parallel()
	ex := NewFindRouteHandler(nil)
	_, err := routery.InvokeRouteHandler(
		context.Background(),
		FindRequest{Filter: map[string]any{}},
		ex,
	)
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("got %v", err)
	}
}

func TestNewFindRouteHandlerDelegates(t *testing.T) {
	t.Parallel()
	ff := &fakeFind{}
	ex := NewFindRouteHandler(ff)
	want := errors.New("boom")
	ff.err = want
	opts := options.Find().SetBatchSize(2)
	outcome, err := routery.InvokeRouteHandler(
		context.Background(),
		FindRequest{Filter: map[string]any{"a": 1}, Options: opts},
		ex,
	)
	if !errors.Is(err, want) {
		t.Fatalf("got %v", err)
	}
	if outcome.Action != routery.ActionAbort {
		t.Fatalf("got action %q; want abort", outcome.Action)
	}
	if ff.calls.Load() != 1 {
		t.Fatalf("calls=%d", ff.calls.Load())
	}
}

func TestNewInsertOneRouteHandlerDelegates(t *testing.T) {
	t.Parallel()
	fi := &fakeInsert{}
	ex := NewInsertOneRouteHandler(fi)
	want := errors.New("ins")
	fi.err = want
	opt := options.InsertOne().SetComment("c")
	_, err := routery.InvokeRouteHandler(
		context.Background(),
		InsertOneRequest{Document: map[string]int{"x": 1}, Options: opt},
		ex,
	)
	if !errors.Is(err, want) {
		t.Fatalf("got %v", err)
	}
	if fi.calls != 1 {
		t.Fatalf("calls=%d", fi.calls)
	}
}

func TestNewUpdateOneRouteHandlerDelegates(t *testing.T) {
	t.Parallel()
	fu := &fakeUpdate{}
	ex := NewUpdateOneRouteHandler(fu)
	want := errors.New("up")
	fu.err = want
	opt := options.Update().SetUpsert(true)
	_, err := routery.InvokeRouteHandler(context.Background(), UpdateOneRequest{
		Filter:  map[string]any{},
		Update:  map[string]any{"$set": map[string]int{"a": 1}},
		Options: opt,
	}, ex)
	if !errors.Is(err, want) {
		t.Fatalf("got %v", err)
	}
	if fu.calls != 1 {
		t.Fatalf("calls=%d", fu.calls)
	}
}

func TestNewDeleteOneRouteHandlerDelegates(t *testing.T) {
	t.Parallel()
	fd := &fakeDelete{}
	ex := NewDeleteOneRouteHandler(fd)
	want := errors.New("del")
	fd.err = want
	opt := options.Delete().SetComment("d")
	_, err := routery.InvokeRouteHandler(
		context.Background(),
		DeleteOneRequest{Filter: map[string]any{}, Options: opt},
		ex,
	)
	if !errors.Is(err, want) {
		t.Fatalf("got %v", err)
	}
	if fd.calls != 1 {
		t.Fatalf("calls=%d", fd.calls)
	}
}

type fakeInsert struct {
	calls int
	res   *mongo.InsertOneResult
	err   error
}

func (f *fakeInsert) InsertOne(
	ctx context.Context,
	document any,
	opts ...*options.InsertOneOptions,
) (*mongo.InsertOneResult, error) {
	f.calls++
	_ = ctx
	_ = document
	_ = opts
	return f.res, f.err
}

func TestNewInsertOneRouteHandlerNil(t *testing.T) {
	t.Parallel()
	ex := NewInsertOneRouteHandler(nil)
	_, err := routery.InvokeRouteHandler(
		context.Background(),
		InsertOneRequest{Document: map[string]any{}},
		ex,
	)
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("got %v", err)
	}
}

type fakeUpdate struct {
	calls int
	res   *mongo.UpdateResult
	err   error
}

func (f *fakeUpdate) UpdateOne(
	ctx context.Context,
	filter any,
	update any,
	opts ...*options.UpdateOptions,
) (*mongo.UpdateResult, error) {
	f.calls++
	_ = ctx
	_ = filter
	_ = update
	_ = opts
	return f.res, f.err
}

func TestNewUpdateOneRouteHandlerNil(t *testing.T) {
	t.Parallel()
	ex := NewUpdateOneRouteHandler(nil)
	_, err := routery.InvokeRouteHandler(context.Background(), UpdateOneRequest{}, ex)
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("got %v", err)
	}
}

type fakeDelete struct {
	calls int
	res   *mongo.DeleteResult
	err   error
}

func (f *fakeDelete) DeleteOne(
	ctx context.Context,
	filter any,
	opts ...*options.DeleteOptions,
) (*mongo.DeleteResult, error) {
	f.calls++
	_ = ctx
	_ = filter
	_ = opts
	return f.res, f.err
}

func TestNewDeleteOneRouteHandlerNil(t *testing.T) {
	t.Parallel()
	ex := NewDeleteOneRouteHandler(nil)
	_, err := routery.InvokeRouteHandler(context.Background(), DeleteOneRequest{}, ex)
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("got %v", err)
	}
}

func TestFindRouteHandlerConcurrent(t *testing.T) {
	t.Parallel()
	ff := &fakeFind{}
	ex := NewFindRouteHandler(ff)
	const workers = 128
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			_, _ = routery.InvokeRouteHandler(
				context.Background(),
				FindRequest{Filter: map[string]any{}},
				ex,
			)
		})
	}
	wg.Wait()
	if got := int(ff.calls.Load()); got != workers {
		t.Fatalf("want %d calls, got %d", workers, got)
	}
}

func TestWritesRetainPartialResultsOnError(t *testing.T) {
	for _, partial := range []bool{false, true} {
		// Arrange.
		writeErr := mongo.WriteException{
			WriteConcernError: &mongo.WriteConcernError{Code: 64, Message: "replication timeout"},
		}
		var insert *mongo.InsertOneResult
		var update *mongo.UpdateResult
		var deleted *mongo.DeleteResult
		if partial {
			insert = &mongo.InsertOneResult{InsertedID: "id"}
			update = &mongo.UpdateResult{ModifiedCount: 1}
			deleted = &mongo.DeleteResult{DeletedCount: 1}
		}
		// Act.
		ir, ie := NewInsertOneRouteHandler(
			&fakeInsert{res: insert, err: writeErr},
		)(
			routery.NewRouteCall(t.Context(), InsertOneRequest{}),
		)
		ur, ue := NewUpdateOneRouteHandler(
			&fakeUpdate{res: update, err: writeErr},
		)(
			routery.NewRouteCall(t.Context(), UpdateOneRequest{}),
		)
		dr, de := NewDeleteOneRouteHandler(
			&fakeDelete{res: deleted, err: writeErr},
		)(
			routery.NewRouteCall(t.Context(), DeleteOneRequest{}),
		)
		// Assert.
		if ir.HasPayload != partial || ur.HasPayload != partial || dr.HasPayload != partial || ir.Payload != insert ||
			ur.Payload != update ||
			dr.Payload != deleted {
			t.Fatalf("partial=%v results=%+v %+v %+v", partial, ir, ur, dr)
		}
		for _, err := range []error{ie, ue, de} {
			var got mongo.WriteException
			if !errors.As(err, &got) || got.WriteConcernError != writeErr.WriteConcernError {
				t.Fatalf("lost original error: %v", err)
			}
		}
	}
}
