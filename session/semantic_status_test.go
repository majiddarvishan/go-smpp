package session

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/transport"
)

// TestSemanticDecodeErrorNACKsWithMappedStatusAndSurvives is the session-
// level half of Task 3.1 (Finding B6): a peer that sends a structurally
// valid but semantically wrong submit_sm (sm_length 255) must get back a
// generic_nack carrying the mapped status, not the old blanket
// StatusInvalidMessageLength fallback for every kind of semantic error —
// and, just as importantly, the session must still be fully functional
// afterward: framing corruption closes a session, but a semantic/body
// error must not poison or resynchronize the stream (AGENTS.md's rule,
// unaffected by this task but the reason this second half of the test
// exists at all).
func TestSemanticDecodeErrorNACKsWithMappedStatusAndSurvives(t *testing.T) {
	smscConn, esmeConn := net.Pipe()
	registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}

	handler := HandlerFunc(func(_ context.Context, _ *Session, pdu InboundPDU) (Response, error) {
		switch pdu.Header.CommandID {
		case protocol.CommandSubmitSM:
			return Response{Status: protocol.StatusOK, Body: protocol.SubmitSMResp{MessageID: []byte("ok-1")}}, nil
		case protocol.CommandBindTransceiver:
			return Response{Status: protocol.StatusOK, Body: protocol.BindResponse{SystemID: []byte("smsc")}}, nil
		}
		return Response{Status: protocol.StatusOK}, nil
	})
	smsc, err := New(smscConn, Config{Role: RoleSMSC, Handler: handler})
	if err != nil {
		t.Fatal(err)
	}
	defer smsc.Close()

	// Play the ESME side by hand so a structurally-valid-but-semantically-
	// wrong frame (something EncodePDU/the protocol structs would never let
	// a well-behaved caller build) can be put on the wire directly.
	writeFrame := func(commandID protocol.CommandID, status protocol.CommandStatus, seq uint32, body []byte) {
		h := codec.Header{CommandLength: uint32(codec.HeaderSize + len(body)), CommandID: commandID, CommandStatus: status, SequenceNumber: protocol.SequenceNumber(seq)}
		frame := make([]byte, codec.HeaderSize+len(body))
		if err := codec.EncodeHeader(frame, h); err != nil {
			t.Fatal(err)
		}
		copy(frame[codec.HeaderSize:], body)
		if err := transport.WriteFull(esmeConn, frame); err != nil {
			t.Fatal(err)
		}
	}
	// bind_transceiver, built the normal way — only the submit_sm below needs
	// to be hand-built, since that's the one case under test.
	bindFrame, err := codec.EncodePDU(nil, codec.Header{CommandID: protocol.CommandBindTransceiver, CommandStatus: protocol.StatusOK, SequenceNumber: 1}, protocol.BindRequest{SystemID: []byte("esme"), Password: []byte("pw"), SystemType: []byte("vms"), InterfaceVersion: protocol.InterfaceVersion34}, registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := transport.WriteFull(esmeConn, bindFrame); err != nil {
		t.Fatal(err)
	}
	resp, err := readOnePDU(esmeConn, registry)
	if err != nil {
		t.Fatalf("read bind response: %v", err)
	}
	if resp.Header.CommandID != protocol.CommandBindTransceiverResp || !resp.Header.CommandStatus.OK() {
		t.Fatalf("bind failed: %+v", resp.Header)
	}

	// header-only enquire_link with an unexpected trailing octet (Task 3.1's
	// mapped case: StatusInvalidCommandLength, 0x02 — chosen deliberately
	// because it differs from the StatusInvalidMessageLength (0x01) fallback,
	// so this test can actually tell "mapped correctly" apart from "fell back
	// to the default and got lucky"; the codec-level table test covers every
	// mapping, including the ones that coincide with the fallback value.
	writeFrame(protocol.CommandEnquireLink, protocol.StatusOK, 2, []byte{0xFF})

	nack, err := readOnePDU(esmeConn, registry)
	if err != nil {
		t.Fatalf("read generic_nack: %v", err)
	}
	if nack.Header.CommandID != protocol.CommandGenericNACK {
		t.Fatalf("response command = %#x, want generic_nack", uint32(nack.Header.CommandID))
	}
	if nack.Header.CommandStatus != protocol.StatusInvalidCommandLength {
		t.Fatalf("generic_nack status = %#x, want %#x (StatusInvalidCommandLength)", uint32(nack.Header.CommandStatus), uint32(protocol.StatusInvalidCommandLength))
	}
	if nack.Header.SequenceNumber != protocol.SequenceNumber(2) {
		t.Fatalf("generic_nack sequence = %d, want 2 (matching the bad enquire_link)", nack.Header.SequenceNumber)
	}

	// The framer must not be poisoned: a well-formed submit_sm right after
	// must still be processed normally.
	writeFrame(protocol.CommandSubmitSM, protocol.StatusOK, 3, []byte{
		0, // service_type
		0, 0,
		'1', 0, // source_addr
		0, 0,
		'2', 0, // destination_addr
		0, 0, 0,
		0, // schedule_delivery_time
		0, // validity_period
		0, 0, 0, 0,
		0, // sm_length = 0
	})
	ok, err := readOnePDU(esmeConn, registry)
	if err != nil {
		t.Fatalf("read post-error submit_sm_resp: %v (session did not survive the semantic error)", err)
	}
	if ok.Header.CommandID != protocol.CommandSubmitSMResp || !ok.Header.CommandStatus.OK() {
		t.Fatalf("post-error response = %+v, want a successful submit_sm_resp", ok.Header)
	}
	if ok.Header.SequenceNumber != protocol.SequenceNumber(3) {
		t.Fatalf("post-error response sequence = %d, want 3", ok.Header.SequenceNumber)
	}

	select {
	case <-smsc.Done():
		t.Fatalf("session closed after a semantic error, want it to stay open: %v", smsc.Err())
	case <-time.After(50 * time.Millisecond):
	}
}
