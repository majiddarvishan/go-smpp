package codec

import (
	"errors"
	"testing"

	"github.com/majiddarvishan/go-smpp/protocol"
)

// buildRawFrame assembles a wire frame with a correct command_length from a
// header and hand-built body bytes, so a test can construct bodies no
// well-behaved encoder would ever produce — which is exactly what these
// tests need: each case is testing how DecodePDU classifies bytes a real
// peer sent that violate a decode-time semantic rule, not anything
// EncodePDU/the protocol structs could construct for it.
func buildRawFrame(commandID protocol.CommandID, status protocol.CommandStatus, seq uint32, body []byte) []byte {
	h := Header{CommandLength: uint32(HeaderSize + len(body)), CommandID: commandID, CommandStatus: status, SequenceNumber: protocol.SequenceNumber(seq)}
	frame := make([]byte, HeaderSize+len(body))
	if err := EncodeHeader(frame, h); err != nil {
		panic(err) // test setup bug, not a case under test
	}
	copy(frame[HeaderSize:], body)
	return frame
}

func cString(s string) []byte { return append([]byte(s), 0) }

// TestDecodePDUSemanticErrorStatusMapping is the acceptance test for
// Task 3.1: for each recognized semantic decode-error class, the resulting
// error must be a *protocol.SemanticError carrying the documented status —
// not the StatusInvalidMessageLength fallback every one of these used to
// produce regardless of what was actually wrong — and errors.Is against the
// original sentinel (ErrInvalidPDUValue / ErrConflictingMessageData) must
// still succeed, since SemanticError wraps rather than replaces it.
func TestDecodePDUSemanticErrorStatusMapping(t *testing.T) {
	registry, err := NewSMPP34Registry(RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name       string
		frame      []byte
		wantStatus protocol.CommandStatus
		wantCause  error
	}{
		{
			name: "bind_response sc_interface_version wrong length",
			frame: buildRawFrame(protocol.CommandBindTransceiverResp, protocol.StatusOK, 1, func() []byte {
				body := cString("smsc") // system_id
				// One TLV: sc_interface_version (0x0210), length 2 (must be 1), value 0x00 0x00.
				body = append(body, 0x02, 0x10, 0x00, 0x02, 0x00, 0x00)
				return body
			}()),
			wantStatus: protocol.StatusInvalidParameterLength,
			wantCause:  ErrInvalidPDUValue,
		},
		{
			name:       "enquire_link (header-only) with an unexpected body octet",
			frame:      buildRawFrame(protocol.CommandEnquireLink, protocol.StatusOK, 2, []byte{0x01}),
			wantStatus: protocol.StatusInvalidCommandLength,
			wantCause:  ErrInvalidPDUValue,
		},
		{
			name: "bind_transmitter with a trailing octet after a complete body",
			frame: buildRawFrame(protocol.CommandBindTransmitter, protocol.StatusOK, 3, func() []byte {
				body := cString("sys")                 // system_id
				body = append(body, cString("pw")...)  // password
				body = append(body, cString("vms")...) // system_type
				body = append(body, 0x34, 0x00, 0x00)  // interface_version, addr_ton, addr_npi
				body = append(body, cString("")...)    // address_range
				body = append(body, 0xFF)              // trailing garbage
				return body
			}()),
			wantStatus: protocol.StatusInvalidCommandLength,
			wantCause:  ErrInvalidPDUValue,
		},
		{
			name: "submit_sm with sm_length 255",
			frame: buildRawFrame(protocol.CommandSubmitSM, protocol.StatusOK, 4, func() []byte {
				body := cString("")                  // service_type
				body = append(body, 0, 0)            // source_addr_ton, source_addr_npi
				body = append(body, cString("1")...) // source_addr
				body = append(body, 0, 0)            // dest_addr_ton, dest_addr_npi
				body = append(body, cString("2")...) // destination_addr
				body = append(body, 0, 0)            // esm_class, protocol_id
				body = append(body, 0)               // priority_flag
				body = append(body, cString("")...)  // schedule_delivery_time
				body = append(body, cString("")...)  // validity_period
				body = append(body, 0, 0, 0, 0)      // registered_delivery, replace_if_present_flag, data_coding, sm_default_msg_id
				body = append(body, 255)             // sm_length = 255
				return body
			}()),
			wantStatus: protocol.StatusInvalidMessageLength,
			wantCause:  ErrInvalidPDUValue,
		},
		{
			name: "submit_sm with both short_message and message_payload set",
			frame: buildRawFrame(protocol.CommandSubmitSM, protocol.StatusOK, 5, func() []byte {
				body := cString("")
				body = append(body, 0, 0)
				body = append(body, cString("1")...)
				body = append(body, 0, 0)
				body = append(body, cString("2")...)
				body = append(body, 0, 0)
				body = append(body, 0)
				body = append(body, cString("")...)
				body = append(body, cString("")...)
				body = append(body, 0, 0, 0, 0)
				body = append(body, 5)                  // sm_length = 5
				body = append(body, []byte("hello")...) // short_message
				// message_payload TLV (0x0424), length 3, value "abc".
				body = append(body, 0x04, 0x24, 0x00, 0x03)
				body = append(body, []byte("abc")...)
				return body
			}()),
			wantStatus: protocol.StatusInvalidOptionalParameterValue,
			wantCause:  ErrConflictingMessageData,
		},
		{
			name:       "submit_multi with number_of_dests 0",
			frame:      buildRawFrame(protocol.CommandSubmitMulti, protocol.StatusOK, 6, buildSubmitMultiBody(0)),
			wantStatus: protocol.StatusInvalidNumberOfDestinations,
			wantCause:  ErrInvalidPDUValue,
		},
		{
			name:       "a request PDU with a nonzero command_status",
			frame:      buildRawFrame(protocol.CommandEnquireLink, protocol.CommandStatus(1), 7, nil),
			wantStatus: protocol.StatusInvalidMessageLength,
			wantCause:  ErrInvalidPDUValue,
		},
		{
			// buildRawFrame always sets command_length correctly, so this one
			// appends an extra byte after building the frame specifically to
			// make the frame longer than its own declared command_length.
			name:       "frame longer than its own declared command_length",
			frame:      append(buildRawFrame(protocol.CommandEnquireLink, protocol.StatusOK, 8, nil), 0xFF),
			wantStatus: protocol.StatusInvalidCommandLength,
			wantCause:  ErrPDUFrameLengthMismatch,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodePDU(tc.frame, registry)
			if err == nil {
				t.Fatal("DecodePDU succeeded, want a semantic decode error")
			}
			var fatal *protocol.FatalError
			if errors.As(err, &fatal) {
				t.Fatalf("got a *protocol.FatalError (%v), want a recoverable *protocol.SemanticError — this case belongs in the fatal-error tests instead", fatal)
			}
			var semantic *protocol.SemanticError
			if !errors.As(err, &semantic) {
				t.Fatalf("error %v (%T) is not a *protocol.SemanticError", err, err)
			}
			if semantic.Status != tc.wantStatus {
				t.Fatalf("status = %#v, want %#v", semantic.Status, tc.wantStatus)
			}
			if !errors.Is(err, tc.wantCause) {
				t.Fatalf("errors.Is(err, %v) = false, want true — SemanticError must still wrap the original sentinel", tc.wantCause)
			}
		})
	}
}

// buildSubmitMultiBody builds a minimal, otherwise-valid submit_multi body
// with the given number_of_dests octet, for exercising that one field in
// isolation.
func buildSubmitMultiBody(numberOfDests byte) []byte {
	body := cString("")                  // service_type
	body = append(body, 0, 0)            // source_addr_ton, source_addr_npi
	body = append(body, cString("1")...) // source_addr
	body = append(body, numberOfDests)   // number_of_dests
	return body
}
