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

func TestNewFindExecutorNil(t *testing.T) {
	t.Parallel()
	ex := NewFindExecutor(nil)
	_, err := ex.Execute(context.Background(), FindRequest{Filter: map[string]any{}})
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("got %v", err)
	}
}

func TestNewFindExecutorDelegates(t *testing.T) {
	t.Parallel()
	ff := &fakeFind{}
	ex := NewFindExecutor(ff)
	want := errors.New("boom")
	ff.err = want
	opts := options.Find().SetBatchSize(2)
	_, err := ex.Execute(context.Background(), FindRequest{Filter: map[string]any{"a": 1}, Options: opts})
	if !errors.Is(err, want) {
		t.Fatalf("got %v", err)
	}
	if ff.calls.Load() != 1 {
		t.Fatalf("calls=%d", ff.calls.Load())
	}
}

func TestNewInsertOneExecutorDelegates(t *testing.T) {
	t.Parallel()
	fi := &fakeInsert{}
	ex := NewInsertOneExecutor(fi)
	want := errors.New("ins")
	fi.err = want
	opt := options.InsertOne().SetComment("c")
	_, err := ex.Execute(context.Background(), InsertOneRequest{Document: map[string]int{"x": 1}, Options: opt})
	if !errors.Is(err, want) {
		t.Fatalf("got %v", err)
	}
	if fi.calls != 1 {
		t.Fatalf("calls=%d", fi.calls)
	}
}

func TestNewUpdateOneExecutorDelegates(t *testing.T) {
	t.Parallel()
	fu := &fakeUpdate{}
	ex := NewUpdateOneExecutor(fu)
	want := errors.New("up")
	fu.err = want
	opt := options.Update().SetUpsert(true)
	_, err := ex.Execute(context.Background(), UpdateOneRequest{
		Filter:  map[string]any{},
		Update:  map[string]any{"$set": map[string]int{"a": 1}},
		Options: opt,
	})
	if !errors.Is(err, want) {
		t.Fatalf("got %v", err)
	}
	if fu.calls != 1 {
		t.Fatalf("calls=%d", fu.calls)
	}
}

func TestNewDeleteOneExecutorDelegates(t *testing.T) {
	t.Parallel()
	fd := &fakeDelete{}
	ex := NewDeleteOneExecutor(fd)
	want := errors.New("del")
	fd.err = want
	opt := options.Delete().SetComment("d")
	_, err := ex.Execute(context.Background(), DeleteOneRequest{Filter: map[string]any{}, Options: opt})
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

func TestNewInsertOneExecutorNil(t *testing.T) {
	t.Parallel()
	ex := NewInsertOneExecutor(nil)
	_, err := ex.Execute(context.Background(), InsertOneRequest{Document: map[string]any{}})
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

func TestNewUpdateOneExecutorNil(t *testing.T) {
	t.Parallel()
	ex := NewUpdateOneExecutor(nil)
	_, err := ex.Execute(context.Background(), UpdateOneRequest{})
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

func TestNewDeleteOneExecutorNil(t *testing.T) {
	t.Parallel()
	ex := NewDeleteOneExecutor(nil)
	_, err := ex.Execute(context.Background(), DeleteOneRequest{})
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("got %v", err)
	}
}

func TestFindExecutorConcurrent(t *testing.T) {
	t.Parallel()
	ff := &fakeFind{}
	ex := NewFindExecutor(ff)
	const workers = 128
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			_, _ = ex.Execute(context.Background(), FindRequest{Filter: map[string]any{}})
		})
	}
	wg.Wait()
	if got := int(ff.calls.Load()); got != workers {
		t.Fatalf("want %d calls, got %d", workers, got)
	}
}
