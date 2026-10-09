package routeryredis

import (
	"context"
	"errors"
	"testing"

	"github.com/redis/go-redis/v9"

	"github.com/skosovsky/routery"
)

func TestNewRouteHandlerNilInvoker(t *testing.T) {
	// Arrange.
	handler := NewRouteHandler[int, string](nil, func(context.Context, redis.Cmder) (string, error) { return "", nil })
	// Act.
	_, err := routery.InvokeRouteHandler(t.Context(), 0, handler)
	// Assert.
	if !errors.Is(err, routery.ErrInvalidConfig) {
		t.Fatalf("err=%v", err)
	}
}

func TestNewRouteHandlerExtractorError(t *testing.T) {
	t.Parallel()
	want := errors.New("extract fail")
	ex := NewStringRouteHandler(func(context.Context, int) (redis.Cmder, error) {
		return nil, want
	})
	_, err := routery.InvokeRouteHandler(context.Background(), 0, ex)
	if !errors.Is(err, want) {
		t.Fatalf("got %v", err)
	}
}
