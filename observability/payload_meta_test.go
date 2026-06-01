package observability

import (
	"context"
	"testing"

	"github.com/skosovsky/routery"
)

func TestDefaultPayloadMetaShape(t *testing.T) {
	t.Parallel()

	meta := DefaultPayloadMeta(context.Background(), routery.Handled(42))
	if meta.Shape != "int" {
		t.Fatalf("got shape=%q, want int", meta.Shape)
	}
	if meta.Fingerprint != "" {
		t.Fatalf("got fingerprint=%q, want empty", meta.Fingerprint)
	}
}

func TestDefaultPayloadMetaEmptyForIgnored(t *testing.T) {
	t.Parallel()

	meta := DefaultPayloadMeta(context.Background(), routery.Ignored[int]("skip"))
	if meta.Shape != shapeEmpty {
		t.Fatalf("got shape=%q, want empty", meta.Shape)
	}
}

func TestDefaultPayloadMetaNilTypedPointerPayload(t *testing.T) {
	t.Parallel()

	meta := DefaultPayloadMeta(context.Background(), routery.Handled[*int](nil))
	if meta.Shape != shapeNil {
		t.Fatalf("got shape=%q, want nil", meta.Shape)
	}
}

func TestDefaultPayloadMetaNilInterfacePayload(t *testing.T) {
	t.Parallel()

	meta := DefaultPayloadMeta(context.Background(), routery.RouteResult[any]{
		Status:  routery.StatusHandled,
		Payload: nil,
	})
	if meta.Shape != shapeNil {
		t.Fatalf("got shape=%q, want nil", meta.Shape)
	}
}

func TestDefaultPayloadMetaAnyTypedPayload(t *testing.T) {
	t.Parallel()

	meta := DefaultPayloadMeta(context.Background(), routery.RouteResult[any]{
		Status:  routery.StatusHandled,
		Payload: 42,
	})
	if meta.Shape != "int" {
		t.Fatalf("got shape=%q, want int", meta.Shape)
	}
}
