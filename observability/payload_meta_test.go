package observability

import (
	"context"
	"testing"

	"github.com/skosovsky/routery"
)

func TestDefaultPayloadMetaShape(t *testing.T) {
	t.Parallel()

	rec := routery.NewResultRecorder[int]()
	rec.Stop(42, "")

	meta := DefaultPayloadMeta(context.Background(), rec)
	if meta.Shape != "int" {
		t.Fatalf("got shape=%q, want int", meta.Shape)
	}
	if meta.Fingerprint != "" {
		t.Fatalf("got fingerprint=%q, want empty", meta.Fingerprint)
	}
}

func TestDefaultPayloadMetaEmptyForIgnored(t *testing.T) {
	t.Parallel()

	rec := routery.NewResultRecorder[int]()
	rec.Ignore("skip")

	meta := DefaultPayloadMeta(context.Background(), rec)
	if meta.Shape != shapeEmpty {
		t.Fatalf("got shape=%q, want empty", meta.Shape)
	}
}

func TestDefaultPayloadMetaEmptyForNext(t *testing.T) {
	t.Parallel()

	rec := routery.NewResultRecorder[int]()
	rec.Next("delegate")

	meta := DefaultPayloadMeta(context.Background(), rec)
	if meta.Shape != shapeEmpty {
		t.Fatalf("got shape=%q, want empty", meta.Shape)
	}
}

func TestDefaultPayloadMetaNilTypedPointerPayload(t *testing.T) {
	t.Parallel()

	rec := routery.NewResultRecorder[*int]()
	rec.Stop(nil, "")

	meta := DefaultPayloadMeta(context.Background(), rec)
	if meta.Shape != shapeNil {
		t.Fatalf("got shape=%q, want nil", meta.Shape)
	}
}

func TestDefaultPayloadMetaNilInterfacePayload(t *testing.T) {
	t.Parallel()

	rec := routery.NewResultRecorder[any]()
	rec.Stop(nil, "")

	meta := DefaultPayloadMeta(context.Background(), rec)
	if meta.Shape != shapeNil {
		t.Fatalf("got shape=%q, want nil", meta.Shape)
	}
}

func TestDefaultPayloadMetaAnyTypedPayload(t *testing.T) {
	t.Parallel()

	rec := routery.NewResultRecorder[any]()
	rec.Stop(42, "")

	meta := DefaultPayloadMeta(context.Background(), rec)
	if meta.Shape != "int" {
		t.Fatalf("got shape=%q, want int", meta.Shape)
	}
}
