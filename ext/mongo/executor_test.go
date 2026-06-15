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
	calls atomic.Int32
	err   error
}

func (f *fakeFind) Find(ctx context.Context, filter any, opts ...*options.FindOptions) (*mongo.Cursor, error) {
	f.calls.Add(1)
	_ = ctx
	_ = filter
	_ = opts
	return nil, f.err
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
