package codec

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/majiddarvishan/go-smpp/protocol"
)

// FuzzDecodePDU50 fuzzes the SMPP 5.0 command set, which FuzzDecodePDU (SMPP 3.4,
// compatible mode) never reaches: broadcast_sm and its siblings, and 5.0 TLVs such as
// congestion_state. The first input byte picks the registry mode (strict or
// compatible); the rest is the frame.
//
// Properties checked:
//   - nothing panics, whatever the bytes;
//   - a frame that decodes was exactly the length its header declared;
//   - the library's own output is stable. A decoded successful response is encoded, the
//     result must decode, and encoding that must give the same bytes. This catches a
//     decoder accepting something the encoder would write differently, or reading one
//     frame two ways.
//
// An encode failure on a decoded body is not counted as a failure: a decoder may accept
// a form the encoder refuses to write (an unknown command kept as RawBody, say).
func FuzzDecodePDU50(f *testing.F) {
	strict, err := NewSMPP50Registry(RegistryStrict)
	if err != nil {
		f.Fatal(err)
	}
	compatible, err := NewSMPP50Registry(RegistryCompatible)
	if err != nil {
		f.Fatal(err)
	}
	registries := []*Registry{compatible, strict}

	for mode := byte(0); mode < 2; mode++ {
		for _, sd := range decode50Seeds() {
			frame, err := EncodePDU(nil, Header{CommandID: sd.command, SequenceNumber: 1}, sd.body, registries[mode&1])
			if err != nil {
				f.Fatalf("seed %v: %v", sd.command, err)
			}
			f.Add(append([]byte{mode}, frame...))
		}
	}
	f.Add([]byte{0})
	f.Add([]byte{1, 0, 0, 0, 16, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 1}) // an unknown command
	f.Add([]byte{0, 0, 0, 0, 15})

	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) == 0 {
			return
		}
		registry := registries[input[0]&1]
		frame := input[1:]
		pdu, err := DecodePDU(frame, registry)
		if err != nil {
			return
		}
		if int(pdu.Header.CommandLength) != len(frame) {
			t.Fatalf("decoded a %d-byte frame whose header declares %d", len(frame), pdu.Header.CommandLength)
		}
		if pdu.Header.CommandStatus != 0 {
			return // see TestErrorResponseBodyIsNotRoundTripSafe: a known, undecided gap
		}
		encoded, err := EncodePDU(nil, pdu.Header, pdu.Body, registry)
		if err != nil {
			return
		}
		again, err := DecodePDU(encoded, registry)
		if err != nil {
			t.Fatalf("a body the decoder produced was encoded into a frame the decoder then rejected: %v", err)
		}
		// Compare what goes on the wire, not Go values: a decoder may hand back a nil slice
		// where the re-decoded frame has an empty one, which means the same thing.
		reencoded, err := EncodePDU(nil, again.Header, again.Body, registry)
		if err != nil {
			t.Fatalf("a body decoded from the library's own output could not be encoded again: %v", err)
		}
		if !bytes.Equal(encoded, reencoded) {
			t.Fatalf("encoding is not stable:\n first  % x\n second % x", encoded, reencoded)
		}
	})
}

type decode50Seed struct {
	command protocol.CommandID
	body    any
}

// decode50Seeds are well-formed SMPP 5.0 (and shared 3.4) bodies, each carrying two TLVs
// where the command takes them. They seed the fuzz target and drive
// TestDecode50SeedsAreLossless.
func decode50Seeds() []decode50Seed {
	tlvs := []protocol.OptionalParameter{
		{Tag: protocol.TLVTagCongestionState, Value: []byte{55}},
		{Tag: protocol.TLVTagBroadcastAreaIdentifier, Value: []byte{1, 2, 3}},
	}
	return []decode50Seed{
		{protocol.CommandBroadcastSM, protocol.BroadcastSM{ServiceType: []byte("CMT"), SourceAddr: []byte("1234"), MessageID: []byte("m1"), ScheduleDeliveryTime: []byte{}, ValidityPeriod: []byte{}, Optional: tlvs}},
		{protocol.CommandBroadcastSMResp, protocol.BroadcastSMResp{MessageID: []byte("m1"), Optional: tlvs}},
		{protocol.CommandQueryBroadcastSM, protocol.QueryBroadcastSM{MessageID: []byte("m1"), SourceAddr: []byte("1234")}},
		{protocol.CommandQueryBroadcastSMResp, protocol.QueryBroadcastSMResp{MessageID: []byte("m1"), Optional: tlvs}},
		{protocol.CommandCancelBroadcastSM, protocol.CancelBroadcastSM{ServiceType: []byte("CMT"), MessageID: []byte("m1"), SourceAddr: []byte("1234")}},
		{protocol.CommandSubmitSM, protocol.SubmitSM{SourceAddr: []byte("1"), DestinationAddr: []byte("2"), ShortMessage: []byte("hi"), Optional: tlvs}},
		{protocol.CommandBindTransceiver, protocol.BindRequest{SystemID: []byte("esme"), Password: []byte("pw"), InterfaceVersion: protocol.InterfaceVersion50}},
		{protocol.CommandEnquireLink, protocol.EmptyBody{}},
	}
}

// The fuzz property starts from what the decoder produced, so a decoder that silently
// drops a field is invisible to it: the first encode already lacks the field, and from then
// on everything is stable. Starting from a known-good body closes that: encode it, decode
// it, encode the result, and the two encodings must be identical (a dropped TLV, a reordered
// TLV or a shifted field all change the bytes). Found when a mutant that made the
// broadcast_sm decoder discard its TLVs survived the fuzz target.
func TestDecode50SeedsAreLossless(t *testing.T) {
	for _, mode := range []RegistryMode{RegistryCompatible, RegistryStrict} {
		registry, err := NewSMPP50Registry(mode)
		if err != nil {
			t.Fatal(err)
		}
		for _, sd := range decode50Seeds() {
			first, err := EncodePDU(nil, Header{CommandID: sd.command, SequenceNumber: 9}, sd.body, registry)
			if err != nil {
				t.Fatalf("%v: %v", sd.command, err)
			}
			pdu, err := DecodePDU(first, registry)
			if err != nil {
				t.Fatalf("%v: the library's own frame does not decode: %v", sd.command, err)
			}
			if reflect.TypeOf(pdu.Body) != reflect.TypeOf(sd.body) {
				t.Fatalf("%v: body came back as %T, want %T", sd.command, pdu.Body, sd.body)
			}
			second, err := EncodePDU(nil, pdu.Header, pdu.Body, registry)
			if err != nil {
				t.Fatalf("%v: re-encoding the decoded body: %v", sd.command, err)
			}
			if !bytes.Equal(first, second) {
				t.Fatalf("%v: decode lost or changed something:\n first  % x\n second % x", sd.command, first, second)
			}
		}
	}
}

// KNOWN GAP, pinned so it cannot change unnoticed; the fuzz target skips non-zero command_status
// because of it. Fuzzing found that for a response with a non-zero status:
//   - the decoder turns a header-only error response into a typed, zero-valued body
//     (here protocol.SubmitMultiResp{}), not protocol.EmptyBody;
//   - the encoder writes that body's mandatory fields (message_id and no_unsuccess, two
//     octets) whatever the status; and
//   - the decoder reads any body after a non-zero status as TLVs only, so it then rejects
//     what the library just wrote ("truncated TLV header").
//
// The library can therefore write a PDU its own decoder refuses. session.Response paths use
// EmptyBody for errors and are not affected; a caller who returns a typed body with an error
// status is. Which side should change (the encoder to write nothing for an error status, or the
// decoder to return EmptyBody) is a decision, not a fix made here. When it is made, this test
// should change with it.
func TestErrorResponseBodyIsNotRoundTripSafe(t *testing.T) {
	frame := []byte{0, 0, 0, 16, 0x80, 0, 0, 0x21, 0, 0, 0, 8, 0, 0, 0, 1} // submit_multi_resp, status 8, no body
	for _, mode := range []RegistryMode{RegistryCompatible, RegistryStrict} {
		registry, err := NewSMPP50Registry(mode)
		if err != nil {
			t.Fatal(err)
		}
		pdu, err := DecodePDU(frame, registry)
		if err != nil {
			t.Fatalf("mode %v: a header-only error response should decode: %v", mode, err)
		}
		if _, ok := pdu.Body.(protocol.SubmitMultiResp); !ok {
			t.Fatalf("mode %v: body is %T; the pinned gap expects a typed zero SubmitMultiResp (if it is now EmptyBody, the gap was fixed: update this test)", mode, pdu.Body)
		}
		encoded, err := EncodePDU(nil, pdu.Header, pdu.Body, registry)
		if err != nil {
			t.Fatalf("mode %v: encode: %v", mode, err)
		}
		if len(encoded) == HeaderSize {
			t.Fatalf("mode %v: the encoder now writes no body for an error status; the gap was fixed, update this test", mode)
		}
		if _, err := DecodePDU(encoded, registry); err == nil {
			t.Fatalf("mode %v: the decoder now accepts what the encoder writes for an error status; the gap was fixed, update this test", mode)
		}
	}
}
