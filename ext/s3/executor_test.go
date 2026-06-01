package routerys3

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/skosovsky/routery"
)

type fakePut struct {
	calls atomic.Int32
	out   *s3.PutObjectOutput
	err   error
}

func (f *fakePut) PutObject(
	ctx context.Context,
	params *s3.PutObjectInput,
	optFns ...func(*s3.Options),
) (*s3.PutObjectOutput, error) {
	f.calls.Add(1)
	_ = ctx
	_ = params
	_ = optFns
	return f.out, f.err
}

func TestNewPutObjectHandlerNil(t *testing.T) {
	t.Parallel()
	ex := NewPutObjectHandler(nil)
	_, err := ex.Handle(context.Background(), &s3.PutObjectInput{})
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("got %v", err)
	}
}

func TestNewPutObjectHandlerOK(t *testing.T) {
	t.Parallel()
	fp := &fakePut{out: &s3.PutObjectOutput{}}
	ex := NewPutObjectHandler(fp)
	b, k := "b", "k"
	outResult, err := ex.Handle(context.Background(), &s3.PutObjectInput{Bucket: &b, Key: &k})
	if err != nil || outResult.Payload != fp.out {
		t.Fatalf("out=%v err=%v", outResult.Payload, err)
	}
	if fp.calls.Load() != 1 {
		t.Fatalf("calls=%d", fp.calls.Load())
	}
}

type fakeGet struct {
	calls int
	out   *s3.GetObjectOutput
	err   error
}

func (f *fakeGet) GetObject(
	ctx context.Context,
	params *s3.GetObjectInput,
	optFns ...func(*s3.Options),
) (*s3.GetObjectOutput, error) {
	f.calls++
	_ = ctx
	_ = params
	_ = optFns
	return f.out, f.err
}

func TestNewGetObjectHandlerNil(t *testing.T) {
	t.Parallel()
	ex := NewGetObjectHandler(nil)
	_, err := ex.Handle(context.Background(), &s3.GetObjectInput{})
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("got %v", err)
	}
}

func TestNewGetObjectHandlerOK(t *testing.T) {
	t.Parallel()
	fg := &fakeGet{out: &s3.GetObjectOutput{}}
	ex := NewGetObjectHandler(fg)
	b, k := "b", "k"
	outResult, err := ex.Handle(context.Background(), &s3.GetObjectInput{Bucket: &b, Key: &k})
	if err != nil || outResult.Payload != fg.out {
		t.Fatalf("out=%v err=%v", outResult.Payload, err)
	}
}

func TestPutExecutorConcurrent(t *testing.T) {
	t.Parallel()
	fp := &fakePut{out: &s3.PutObjectOutput{}}
	ex := NewPutObjectHandler(fp)
	const workers = 128
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			_, e := ex.Handle(context.Background(), &s3.PutObjectInput{})
			if e != nil {
				t.Error(e)
			}
		})
	}
	wg.Wait()
	if got := int(fp.calls.Load()); got != workers {
		t.Fatalf("want %d calls, got %d", workers, got)
	}
}
