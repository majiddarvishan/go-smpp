package session

import (
	"reflect"
	"sort"
	"testing"

	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/protocol"
)

// C4: ownDecodedPDU and responseOptionalParameters are two type switches over
// the same set of response body types. The finding proposed collapsing them
// behind interfaces implemented by the body types, but those types live in
// package protocol: a session-side interface cannot be satisfied by methods
// the protocol package does not declare, and adding them would change
// protocol's exported API and make it aware of session's arena. So the two
// switches stay, and what the finding actually wanted - that forgetting one
// of them for a new body type fails loudly instead of silently - is provided
// by this single test.
//
// It does not keep its own list of body types, which would be a third place
// to forget. It asks the codec registry: every registered response command is
// decoded from synthetic bodies, with and without a trailing TLV, and each
// distinct Go type that comes back is checked. A new response body type
// registered in codec is therefore covered automatically.
//
// For each type it checks, by reflection over every field:
//   - ownDecodedPDU deep-copies it: after the borrowed source is overwritten
//     the owned body still equals an untouched twin (a type missing from the
//     switch would alias the source and fail here); and
//   - responseOptionalParameters returns the body's Optional field, or nil if
//     it has none (a type missing from that switch would silently drop TLVs,
//     such as congestion state, and fail here).
func TestEveryResponseBodyTypeIsOwnedAndExposesItsOptionals(t *testing.T) {
	types := responseBodyTypes(t)
	if len(types) < 10 {
		t.Fatalf("only %d response body types discovered (%v); discovery is broken, not the code under test", len(types), typeNames(types))
	}
	optionalType := reflect.TypeOf([]protocol.OptionalParameter(nil))
	for _, typ := range types {
		typ := typ
		t.Run(typ.String(), func(t *testing.T) {
			borrowed := fillBody(typ)
			expected := fillBody(typ)
			if typ.Kind() == reflect.Struct && typ.NumField() > 0 && countByteSlices(expected) == 0 {
				t.Fatalf("%s has fields but the test filled no byte slice in it; it cannot prove anything about this type", typ)
			}

			owned := ownDecodedPDU(codec.DecodedPDU{Header: codec.Header{}, Body: borrowed.Interface()})
			scribble(borrowed)
			if !reflect.DeepEqual(owned.Body, expected.Interface()) {
				t.Errorf("ownDecodedPDU left %s aliasing the borrowed source buffer:\n got  %#v\n want %#v", typ, owned.Body, expected.Interface())
			}

			var wantOptional []protocol.OptionalParameter
			if typ.Kind() == reflect.Struct {
				if f := expected.FieldByName("Optional"); f.IsValid() && f.Type() == optionalType {
					wantOptional = f.Interface().([]protocol.OptionalParameter)
				}
			}
			if got := responseOptionalParameters(expected.Interface()); !reflect.DeepEqual(got, wantOptional) {
				t.Errorf("responseOptionalParameters(%s) = %#v, want %#v", typ, got, wantOptional)
			}
		})
	}
}

// responseBodyTypes returns every distinct body type the SMPP 5.0 registry can
// produce for a response command, plus codec.RawBody (what an unregistered
// command's body decodes to, which ownDecodedPDU names explicitly).
func responseBodyTypes(t *testing.T) []reflect.Type {
	t.Helper()
	reg, err := codec.NewSMPP50Registry(codec.RegistryStrict)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[reflect.Type]bool{reflect.TypeOf(codec.RawBody(nil)): true}
	congestionTLV := []byte{byte(protocol.TLVTagCongestionState >> 8), byte(protocol.TLVTagCongestionState & 0xff), 0, 1, 5}
	for id := protocol.CommandID(0x80000000); id < 0x80000200; id++ {
		if _, ok := reg.Command(id); !ok {
			continue
		}
		decoded := false
		for n := 0; n <= 16; n++ {
			for _, tlv := range [][]byte{nil, congestionTLV} {
				body := append(make([]byte, n), tlv...)
				frame := make([]byte, codec.HeaderSize+len(body))
				if err := codec.EncodeHeader(frame, codec.Header{CommandLength: uint32(len(frame)), CommandID: id, SequenceNumber: 1}); err != nil {
					t.Fatal(err)
				}
				copy(frame[codec.HeaderSize:], body)
				pdu, err := codec.DecodePDU(frame, reg)
				if err != nil {
					continue
				}
				decoded = true
				seen[reflect.TypeOf(pdu.Body)] = true
			}
		}
		if !decoded {
			t.Fatalf("registered response command 0x%08x could not be decoded from any synthetic body; extend the probe", uint32(id))
		}
	}
	var out []reflect.Type
	for typ := range seen {
		out = append(out, typ)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

func typeNames(types []reflect.Type) []string {
	var s []string
	for _, t := range types {
		s = append(s, t.String())
	}
	return s
}

// fillBody returns a settable value of typ with every byte slice, slice of
// structs and integer populated with deterministic non-zero content. Two calls
// return independent, deeply equal values.
func fillBody(typ reflect.Type) reflect.Value {
	v := reflect.New(typ).Elem()
	counter := 0
	fill(v, &counter)
	return v
}

func fill(v reflect.Value, counter *int) {
	switch v.Kind() {
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			b := make([]byte, 3)
			for i := range b {
				*counter++
				b[i] = byte(*counter)
			}
			v.Set(reflect.ValueOf(b).Convert(v.Type()))
			return
		}
		s := reflect.MakeSlice(v.Type(), 2, 2)
		for i := 0; i < s.Len(); i++ {
			fill(s.Index(i), counter)
		}
		v.Set(s)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Field(i).CanSet() {
				fill(v.Field(i), counter)
			}
		}
	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		*counter++
		v.SetUint(uint64(*counter))
	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		*counter++
		v.SetInt(int64(*counter))
	}
}

// scribble overwrites everything reachable from v that a shallow copy would
// still share with it: every byte, and every integer inside slices of structs.
func scribble(v reflect.Value) {
	switch v.Kind() {
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			b := v.Bytes()
			for i := range b {
				b[i] = 0xFF
			}
			return
		}
		for i := 0; i < v.Len(); i++ {
			scribbleDeep(v.Index(i))
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Field(i).CanInterface() {
				scribble(v.Field(i))
			}
		}
	}
}

func scribbleDeep(v reflect.Value) {
	switch v.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Field(i).CanSet() {
				scribbleDeep(v.Field(i))
			}
		}
	case reflect.Slice:
		scribble(v)
	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(^uint64(0) >> (64 - v.Type().Bits()))
	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(-1)
	}
}

func countByteSlices(v reflect.Value) int {
	switch v.Kind() {
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return 1
		}
		n := 0
		for i := 0; i < v.Len(); i++ {
			n += countByteSlices(v.Index(i))
		}
		return n
	case reflect.Struct:
		n := 0
		for i := 0; i < v.NumField(); i++ {
			n += countByteSlices(v.Field(i))
		}
		return n
	}
	return 0
}
