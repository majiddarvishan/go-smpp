package codec

import (
	"reflect"

	"github.com/majiddarvishan/go-smpp/protocol"
)

// A response with a non-zero command_status is written as a header plus, if the body
// has any, its optional TLVs, and nothing else. SMPP 3.4 sends an error response as a
// bare header, and SMPP 5.0 additionally allows TLVs on it; decodeErrorResponseOptional
// reads exactly that and nothing more, so the encoder writes exactly that. Writing the
// body's mandatory fields as well (a message_id, a count) produced a frame this
// package's own decoder then rejected as a truncated TLV header; fuzzing found it.
//
// The body is still validated by the command's own encoder first, so a body of the
// wrong type, or one with an invalid field, is an error as before. What changes is
// only what reaches the wire.
func appendErrorResponseBody(dst []byte, def CommandDefinition, body any) ([]byte, error) {
	if _, err := def.Encode(nil, body); err != nil {
		return dst, err
	}
	return appendOptional(dst, optionalParametersOf(body))
}

// optionalParametersOf returns the Optional field of a typed body, or nil if it has none.
func optionalParametersOf(body any) []protocol.OptionalParameter {
	v := reflect.ValueOf(body)
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	f := v.FieldByName("Optional")
	if !f.IsValid() || f.Type() != reflect.TypeOf([]protocol.OptionalParameter(nil)) {
		return nil
	}
	return f.Interface().([]protocol.OptionalParameter)
}
