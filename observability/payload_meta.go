package observability

import (
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

// PayloadMetaFunc computes telemetry metadata for a route result.
type PayloadMetaFunc[Kind comparable, Reason comparable, Payload any] func(
	result routery.RouteResult[Kind, Reason, Payload],
) PayloadMeta

// DefaultPayloadMeta returns shape-only metadata without reading payload contents.
func DefaultPayloadMeta[Kind comparable, Reason comparable, Payload any](
	result routery.RouteResult[Kind, Reason, Payload],
) PayloadMeta {
	if result.Action == routery.ActionNext || !result.HasPayload {
		return PayloadMeta{Shape: shapeEmpty, Fingerprint: ""}
	}

	shape := shapeNil
	if !isNilValue(result.Payload) {
		shape = fmt.Sprintf("%T", result.Payload)
	}

	return PayloadMeta{Shape: shape, Fingerprint: ""}
}

func isNilValue[Payload any](payload Payload) bool {
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

func resolvePayloadMeta[Kind comparable, Reason comparable, Payload any](
	result routery.RouteResult[Kind, Reason, Payload],
	payloadMeta PayloadMetaFunc[Kind, Reason, Payload],
) PayloadMeta {
	if payloadMeta == nil {
		return DefaultPayloadMeta(result)
	}

	return payloadMeta(result)
}
