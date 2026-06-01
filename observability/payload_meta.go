package observability

import (
	"context"
	"fmt"
	"reflect"

	"github.com/skosovsky/routery"
)

// PayloadMeta is a serializable summary of RouteResult.Payload for telemetry.
type PayloadMeta struct {
	Shape       string
	Fingerprint string
}

// PayloadMetaFunc computes telemetry metadata for a route result payload.
//
// When nil, [DefaultPayloadMeta] is used (shape only, no payload inspection).
type PayloadMetaFunc[Res any] func(ctx context.Context, result routery.RouteResult[Res]) PayloadMeta

// DefaultPayloadMeta returns shape-only metadata without reading payload contents.
func DefaultPayloadMeta[Res any](_ context.Context, result routery.RouteResult[Res]) PayloadMeta {
	if result.Status == routery.StatusIgnored || result.Status == routery.StatusNext {
		return PayloadMeta{Shape: "empty", Fingerprint: ""}
	}

	shape := "nil"
	if !isNilValue(result.Payload) {
		shape = fmt.Sprintf("%T", result.Payload)
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
	result routery.RouteResult[Res],
	payloadMeta PayloadMetaFunc[Res],
) PayloadMeta {
	if payloadMeta == nil {
		return DefaultPayloadMeta(ctx, result)
	}

	return payloadMeta(ctx, result)
}
