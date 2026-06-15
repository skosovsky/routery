package observability

import (
	"context"
	"fmt"
	"reflect"

	"github.com/skosovsky/routery"
)

const (
	shapeEmpty = "empty"
	shapeNil   = "nil"
)

// PayloadMeta is a serializable summary of route payload for telemetry.
type PayloadMeta struct {
	Shape       string
	Fingerprint string
}

// PayloadMetaFunc computes telemetry metadata for a recorded route payload.
type PayloadMetaFunc[Res any] func(ctx context.Context, rec routery.ResultRecorder[Res]) PayloadMeta

// DefaultPayloadMeta returns shape-only metadata without reading payload contents.
func DefaultPayloadMeta[Res any](_ context.Context, rec routery.ResultRecorder[Res]) PayloadMeta {
	if rec.Action() == routery.ActionNext {
		return PayloadMeta{Shape: shapeEmpty, Fingerprint: ""}
	}

	payload, ok := rec.Payload()
	if !ok {
		return PayloadMeta{Shape: shapeEmpty, Fingerprint: ""}
	}

	shape := shapeNil
	if !isNilValue(payload) {
		shape = fmt.Sprintf("%T", payload)
	}

	return PayloadMeta{Shape: shape, Fingerprint: ""}
}

func isNilValue[Res any](payload Res) bool {
	value := reflect.ValueOf(payload)
	if !value.IsValid() {
		return true
	}

	switch value.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface, reflect.Chan, reflect.Func:
		return value.IsNil()
	default:
		return false
	}
}

func resolvePayloadMeta[Res any](
	ctx context.Context,
	rec routery.ResultRecorder[Res],
	payloadMeta PayloadMetaFunc[Res],
) PayloadMeta {
	if payloadMeta == nil {
		return DefaultPayloadMeta(ctx, rec)
	}

	return payloadMeta(ctx, rec)
}
