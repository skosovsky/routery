package routery

import (
	"math"
	"reflect"
	"strconv"
)

// topologyKey avoids Stringer/GoStringer and frames every typed comparable component.
func topologyKey[Key comparable](key Key) string { return fingerprintValue(reflect.ValueOf(key)) }
func fingerprintValue(value reflect.Value) string {
	if !value.IsValid() {
		return FingerprintSHA256([]byte("nil-interface"))
	}
	parts := [][]byte{[]byte(fingerprintType(value.Type()))}
	switch value.Kind() {
	case reflect.Interface:
		if value.IsNil() {
			parts = append(parts, []byte("nil"))
		} else {
			parts = append(parts, []byte(fingerprintValue(value.Elem())))
		}
	case reflect.Array, reflect.Struct:
		count := value.Len
		field := value.Index
		if value.Kind() == reflect.Struct {
			count = value.NumField
			field = value.Field
		}
		for index := range count() {
			parts = append(parts, []byte(fingerprintValue(field(index))))
		}
	case reflect.String:
		parts = append(parts, []byte(value.String()))
	case reflect.Bool:
		parts = append(parts, []byte(strconv.FormatBool(value.Bool())))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		parts = append(parts, []byte(strconv.FormatInt(value.Int(), 10)))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		parts = append(parts, []byte(strconv.FormatUint(value.Uint(), 10)))
	case reflect.Float32, reflect.Float64:
		parts = append(parts, []byte(strconv.FormatUint(math.Float64bits(value.Float()), 16)))
	case reflect.Complex64, reflect.Complex128:
		number := value.Complex()
		parts = append(
			parts,
			[]byte(strconv.FormatUint(math.Float64bits(real(number)), 16)),
			[]byte(strconv.FormatUint(math.Float64bits(imag(number)), 16)),
		)
	case reflect.Pointer, reflect.Chan, reflect.UnsafePointer:
		parts = append(parts, []byte(strconv.FormatUint(uint64(value.Pointer()), 16)))
	default:
		parts = append(parts, []byte("non-comparable"))
	}
	return FingerprintSHA256(parts...)
}

func fingerprintType(kind reflect.Type) string {
	parts := [][]byte{[]byte(kind.PkgPath()), []byte(kind.Name()), []byte(kind.String())}
	if kind.Name() != "" {
		return FingerprintSHA256(parts...)
	}
	switch kind.Kind() {
	case reflect.Array, reflect.Pointer, reflect.Chan, reflect.Slice:
		parts = append(parts, []byte(fingerprintType(kind.Elem())))
	case reflect.Struct:
		for field := range kind.Fields() {
			parts = append(
				parts,
				[]byte(field.Name),
				[]byte(field.PkgPath),
				[]byte(field.Tag),
				[]byte(strconv.FormatBool(field.Anonymous)),
				[]byte(fingerprintType(field.Type)),
			)
		}
	case reflect.Interface:
		for method := range kind.Methods() {
			parts = append(parts, []byte(method.Name), []byte(method.PkgPath), []byte(fingerprintType(method.Type)))
		}
	case reflect.Func:
		parts = append(parts, []byte(strconv.FormatBool(kind.IsVariadic())))
		for argument := range kind.Ins() {
			parts = append(parts, []byte("in"), []byte(fingerprintType(argument)))
		}
		for argument := range kind.Outs() {
			parts = append(parts, []byte("out"), []byte(fingerprintType(argument)))
		}
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128, reflect.String,
		reflect.UnsafePointer, reflect.Map, reflect.Invalid:
		// Built-ins have no children relevant to a comparable key.
	}
	return FingerprintSHA256(parts...)
}
