package codec

import (
	"bytes"
	"errors"
	"testing"

	"github.com/majiddarvishan/go-smpp/protocol"
)

// Found by FuzzDecodePDU50: a response with a non-zero command_status was written with
// the body's mandatory fields, which the decoder (header plus optional TLVs only for an
// error status) then rejected. The encoder now writes what the decoder reads. See
// error_response.go.

func errorRegistries(t *testing.T) map[string]*Registry {
	t.Helper()
	out := map[string]*Registry{}
	for name, mk := range map[string]func(RegistryMode) (*Registry, error){"3.4": NewSMPP34Registry, "5.0": NewSMPP50Registry} {
		for modeName, mode := range map[string]RegistryMode{"compatible": RegistryCompatible, "strict": RegistryStrict} {
			r, err := mk(mode)
			if err != nil {
				t.Fatal(err)
			}
			out[name+"/"+modeName] = r
		}
	}
	return out
}

func TestErrorResponseIsWrittenAsHeaderAndOptionalsOnly(t *testing.T) {
	tlv := []protocol.OptionalParameter{{Tag: protocol.TLVTagCongestionState, Value: []byte{42}}}
	for name, registry := range errorRegistries(t) {
		hdr := Header{CommandID: protocol.CommandSubmitMultiResp, CommandStatus: protocol.StatusSystemError, SequenceNumber: 7}

		// a typed zero body: a bare header, and it decodes
		frame, err := EncodePDU(nil, hdr, protocol.SubmitMultiResp{}, registry)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(frame) != HeaderSize {
			t.Fatalf("%s: %d bytes written for an error response with no TLVs, want the %d-byte header alone: % x", name, len(frame), HeaderSize, frame)
		}
		if _, err := DecodePDU(frame, registry); err != nil {
			t.Fatalf("%s: the library's own error response does not decode: %v", name, err)
		}

		// a body full of mandatory fields: they are not written, the TLVs are
		full := protocol.SubmitMultiResp{MessageID: []byte("abc"), Unsuccessful: []protocol.UnsuccessfulSME{{DestinationAddr: []byte("1"), ErrorStatusCode: protocol.StatusSystemError}}, Optional: tlv}
		frame, err = EncodePDU(nil, hdr, full, registry)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want := []byte{0x04, 0x28, 0x00, 0x01, 42} // congestion_state TLV
		if got := frame[HeaderSize:]; !bytes.Equal(got, want) {
			t.Fatalf("%s: body % x, want only the TLV % x", name, got, want)
		}
		pdu, err := DecodePDU(frame, registry)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		// SMPP 5.0 strict hands an error response that carries TLVs back as
		// protocol.OptionalResponse; the others return the typed body. Either way: the TLV, and none
		// of the mandatory fields that were in the body we passed in.
		optional := optionalParametersOf(pdu.Body)
		if len(optional) != 1 || optional[0].Tag != protocol.TLVTagCongestionState {
			t.Fatalf("%s: decoded %#v, want the one TLV", name, pdu.Body)
		}
		if typed, ok := pdu.Body.(protocol.SubmitMultiResp); ok && (len(typed.MessageID) != 0 || len(typed.Unsuccessful) != 0) {
			t.Fatalf("%s: mandatory fields were written for an error response: %#v", name, typed)
		}
	}
}

func TestSuccessResponseIsUnchanged(t *testing.T) {
	registry, err := NewSMPP34Registry(RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := EncodePDU(nil, Header{CommandID: protocol.CommandSubmitMultiResp, SequenceNumber: 1}, protocol.SubmitMultiResp{MessageID: []byte("abc")}, registry)
	if err != nil {
		t.Fatal(err)
	}
	// message_id "abc" NUL, no_unsuccess 0
	if got, want := frame[HeaderSize:], []byte{'a', 'b', 'c', 0, 0}; !bytes.Equal(got, want) {
		t.Fatalf("success body % x, want % x", got, want)
	}
}

func TestRequestWithANonZeroStatusKeepsItsBody(t *testing.T) {
	registry, err := NewSMPP34Registry(RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	body := protocol.SubmitSM{SourceAddr: []byte("1"), DestinationAddr: []byte("2"), ShortMessage: []byte("hi")}
	ok, err := EncodePDU(nil, Header{CommandID: protocol.CommandSubmitSM, SequenceNumber: 1}, body, registry)
	if err != nil {
		t.Fatal(err)
	}
	odd, err := EncodePDU(nil, Header{CommandID: protocol.CommandSubmitSM, CommandStatus: protocol.StatusSystemError, SequenceNumber: 1}, body, registry)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ok[HeaderSize:], odd[HeaderSize:]) || len(odd) <= HeaderSize {
		t.Fatalf("a request's body must not depend on its status field: % x vs % x", ok[HeaderSize:], odd[HeaderSize:])
	}
}

func TestErrorResponseStillValidatesItsBody(t *testing.T) {
	registry, err := NewSMPP34Registry(RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	hdr := Header{CommandID: protocol.CommandSubmitSMResp, CommandStatus: protocol.StatusSystemError, SequenceNumber: 1}
	if _, err := EncodePDU(nil, hdr, protocol.SubmitSM{}, registry); !errors.Is(err, ErrInvalidPDUValue) {
		t.Fatalf("a body of the wrong type was accepted for an error response: %v", err)
	}
	tooLong := protocol.SubmitSMResp{MessageID: bytes.Repeat([]byte("x"), 200)}
	if _, err := EncodePDU(nil, hdr, tooLong, registry); err == nil {
		t.Fatal("an invalid field was accepted for an error response, though it would not have been written")
	}
}

// Every registered response, in both versions and both modes, must survive an error status:
// what the encoder writes, the decoder reads, and writing that again gives the same bytes.
func TestEveryRegisteredResponseSurvivesAnErrorStatus(t *testing.T) {
	checked := 0
	for name, registry := range errorRegistries(t) {
		for id := protocol.CommandID(0x80000000); id < 0x80000200; id++ {
			if _, ok := registry.Command(id); !ok {
				continue
			}
			// find a typed body by decoding synthetic bodies with a success status
			var body any
			for n := 0; n <= 16 && body == nil; n++ {
				frame := make([]byte, HeaderSize+n)
				if err := EncodeHeader(frame, Header{CommandLength: uint32(len(frame)), CommandID: id, SequenceNumber: 1}); err != nil {
					t.Fatal(err)
				}
				if pdu, err := DecodePDU(frame, registry); err == nil {
					body = pdu.Body
				}
			}
			if body == nil {
				t.Fatalf("%s: could not obtain a typed body for command 0x%08x", name, uint32(id))
			}
			for _, status := range []protocol.CommandStatus{protocol.StatusSystemError, protocol.StatusInvalidPassword, protocol.StatusThrottled} {
				hdr := Header{CommandID: id, CommandStatus: status, SequenceNumber: 3}
				first, err := EncodePDU(nil, hdr, body, registry)
				if err != nil {
					t.Fatalf("%s 0x%08x status %v: encode: %v", name, uint32(id), status, err)
				}
				pdu, err := DecodePDU(first, registry)
				if err != nil {
					t.Fatalf("%s 0x%08x status %v: the library's own frame does not decode: %v", name, uint32(id), status, err)
				}
				second, err := EncodePDU(nil, pdu.Header, pdu.Body, registry)
				if err != nil || !bytes.Equal(first, second) {
					t.Fatalf("%s 0x%08x status %v: not stable: % x then % x (%v)", name, uint32(id), status, first, second, err)
				}
				checked++
			}
		}
	}
	if checked < 50 {
		t.Fatalf("only %d command/status combinations checked; discovery is broken", checked)
	}
}
