package session

import (
	"context"
	"net"
	"testing"

	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/transport"
)

const (
	vendorESMECommand protocol.CommandID = 0x4A000001 // ESME-issued, like submit_sm: BoundTX/BoundTRX only
	vendorSMSCCommand protocol.CommandID = 0x4A000002 // SMSC-issued, to exercise the wrong-direction case
)

// decodeVendorEmpty/encodeVendorEmpty are deliberately trivial: this test is
// about beginInbound's direction/state gate, not about encoding, so every
// vendor command and response here carries an empty body.
func decodeVendorEmpty(_ codec.Header, _ []byte) (any, error) { return protocol.EmptyBody{}, nil }
func encodeVendorEmpty(dst []byte, _ any) ([]byte, error)     { return dst, nil }

func vendorTestRegistry(t *testing.T) *codec.Registry {
	t.Helper()
	builder, err := codec.NewSMPP34RegistryBuilder(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	defs := []codec.CommandDefinition{
		{ID: vendorESMECommand, Name: "vendor_esme_cmd", Decode: decodeVendorEmpty, Encode: encodeVendorEmpty,
			Actor: codec.ActorESME, AllowedStates: []protocol.SessionState{protocol.StateBoundTX, protocol.StateBoundTRX}},
		{ID: vendorESMECommand.ResponseID(), Name: "vendor_esme_cmd_resp", Decode: decodeVendorEmpty, Encode: encodeVendorEmpty},
		{ID: vendorSMSCCommand, Name: "vendor_smsc_cmd", Decode: decodeVendorEmpty, Encode: encodeVendorEmpty,
			Actor: codec.ActorSMSC, AllowedStates: []protocol.SessionState{protocol.StateBoundTX, protocol.StateBoundTRX}},
		{ID: vendorSMSCCommand.ResponseID(), Name: "vendor_smsc_cmd_resp", Decode: decodeVendorEmpty, Encode: encodeVendorEmpty},
	}
	for _, def := range defs {
		if err := builder.RegisterCommand(def); err != nil {
			t.Fatal(err)
		}
	}
	registry, err := builder.Freeze()
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

// TestVendorCommandRefusedInWrongState is this task's first acceptance case:
// vendorESMECommand declares the same policy as submit_sm (ESME, BoundTX or
// BoundTRX only). Bound as receive-only (BoundRX — reachable via
// bind_receiver, a state submit_sm itself is refused in), the vendor command
// must be refused the same way, by the state check alone, not by role.
func TestVendorCommandRefusedInWrongState(t *testing.T) {
	registry := vendorTestRegistry(t)
	smscConn, esmeConn := net.Pipe()
	handlerCalled := false
	handler := HandlerFunc(func(_ context.Context, _ *Session, pdu InboundPDU) (Response, error) {
		if pdu.Header.CommandID == vendorESMECommand {
			handlerCalled = true
		}
		if pdu.Header.CommandID == protocol.CommandBindReceiver || pdu.Header.CommandID == protocol.CommandBindTransceiver {
			return Response{Status: protocol.StatusOK, Body: protocol.BindResponse{SystemID: []byte("smsc")}}, nil
		}
		return Response{Status: protocol.StatusOK, Body: protocol.EmptyBody{}}, nil
	})
	smsc, err := New(smscConn, Config{Role: RoleSMSC, Registry: registry, Handler: handler})
	if err != nil {
		t.Fatal(err)
	}
	defer smsc.Close()

	bindFrame, err := codec.EncodePDU(nil, codec.Header{CommandID: protocol.CommandBindReceiver, CommandStatus: protocol.StatusOK, SequenceNumber: 1},
		protocol.BindRequest{SystemID: []byte("esme"), Password: []byte("pw"), SystemType: []byte("vms"), InterfaceVersion: protocol.InterfaceVersion34}, registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := transport.WriteFull(esmeConn, bindFrame); err != nil {
		t.Fatal(err)
	}
	bindResp, err := readOnePDU(esmeConn, registry)
	if err != nil || !bindResp.Header.CommandStatus.OK() {
		t.Fatalf("bind_receiver failed: resp=%+v err=%v", bindResp.Header, err)
	}

	vendorFrame, err := codec.EncodePDU(nil, codec.Header{CommandID: vendorESMECommand, CommandStatus: protocol.StatusOK, SequenceNumber: 2}, protocol.EmptyBody{}, registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := transport.WriteFull(esmeConn, vendorFrame); err != nil {
		t.Fatal(err)
	}
	resp, err := readOnePDU(esmeConn, registry)
	if err != nil {
		t.Fatalf("read vendor command response: %v", err)
	}
	if resp.Header.CommandStatus != protocol.StatusInvalidBindState {
		t.Fatalf("status = %#x, want StatusInvalidBindState (%#x) — a BoundRX session must refuse this the way it would refuse submit_sm",
			uint32(resp.Header.CommandStatus), uint32(protocol.StatusInvalidBindState))
	}
	if handlerCalled {
		t.Fatal("handler was invoked for a command beginInbound should have refused before dispatch")
	}
}

// TestVendorCommandWrongDirectionRefused is the second acceptance case:
// vendorSMSCCommand declares Actor: ActorSMSC. An ESME peer sending it to an
// SMSC session must be refused regardless of state, since the session's
// peer role (ESME) never matches the command's declared actor (SMSC).
func TestVendorCommandWrongDirectionRefused(t *testing.T) {
	registry := vendorTestRegistry(t)
	smscConn, esmeConn := net.Pipe()
	handlerCalled := false
	handler := HandlerFunc(func(_ context.Context, _ *Session, pdu InboundPDU) (Response, error) {
		if pdu.Header.CommandID == vendorSMSCCommand {
			handlerCalled = true
		}
		if pdu.Header.CommandID == protocol.CommandBindReceiver || pdu.Header.CommandID == protocol.CommandBindTransceiver {
			return Response{Status: protocol.StatusOK, Body: protocol.BindResponse{SystemID: []byte("smsc")}}, nil
		}
		return Response{Status: protocol.StatusOK, Body: protocol.EmptyBody{}}, nil
	})
	smsc, err := New(smscConn, Config{Role: RoleSMSC, Registry: registry, Handler: handler})
	if err != nil {
		t.Fatal(err)
	}
	defer smsc.Close()

	bindFrame, err := codec.EncodePDU(nil, codec.Header{CommandID: protocol.CommandBindTransceiver, CommandStatus: protocol.StatusOK, SequenceNumber: 1},
		protocol.BindRequest{SystemID: []byte("esme"), Password: []byte("pw"), SystemType: []byte("vms"), InterfaceVersion: protocol.InterfaceVersion34}, registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := transport.WriteFull(esmeConn, bindFrame); err != nil {
		t.Fatal(err)
	}
	bindResp, err := readOnePDU(esmeConn, registry)
	if err != nil || !bindResp.Header.CommandStatus.OK() {
		t.Fatalf("bind_transceiver failed: resp=%+v err=%v", bindResp.Header, err)
	}

	// Bound as BoundTRX now — vendorSMSCCommand's AllowedStates would permit
	// this state; only the actor (direction) mismatch should refuse it.
	vendorFrame, err := codec.EncodePDU(nil, codec.Header{CommandID: vendorSMSCCommand, CommandStatus: protocol.StatusOK, SequenceNumber: 2}, protocol.EmptyBody{}, registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := transport.WriteFull(esmeConn, vendorFrame); err != nil {
		t.Fatal(err)
	}
	resp, err := readOnePDU(esmeConn, registry)
	if err != nil {
		t.Fatalf("read vendor command response: %v", err)
	}
	if resp.Header.CommandStatus != protocol.StatusInvalidBindState {
		t.Fatalf("status = %#x, want StatusInvalidBindState (%#x) — wrong-direction vendor request must be refused even in an allowed state",
			uint32(resp.Header.CommandStatus), uint32(protocol.StatusInvalidBindState))
	}
	if handlerCalled {
		t.Fatal("handler was invoked for a wrong-direction command beginInbound should have refused before dispatch")
	}
}

// TestVendorCommandAcceptedInCorrectStateAndDirection is the positive control
// for both tests above: the same vendorESMECommand, sent by the role and in
// the state its declared policy actually permits, must reach the handler.
func TestVendorCommandAcceptedInCorrectStateAndDirection(t *testing.T) {
	registry := vendorTestRegistry(t)
	smscConn, esmeConn := net.Pipe()
	handlerCalled := false
	handler := HandlerFunc(func(_ context.Context, _ *Session, pdu InboundPDU) (Response, error) {
		if pdu.Header.CommandID == vendorESMECommand {
			handlerCalled = true
		}
		if pdu.Header.CommandID == protocol.CommandBindReceiver || pdu.Header.CommandID == protocol.CommandBindTransceiver {
			return Response{Status: protocol.StatusOK, Body: protocol.BindResponse{SystemID: []byte("smsc")}}, nil
		}
		return Response{Status: protocol.StatusOK, Body: protocol.EmptyBody{}}, nil
	})
	smsc, err := New(smscConn, Config{Role: RoleSMSC, Registry: registry, Handler: handler})
	if err != nil {
		t.Fatal(err)
	}
	defer smsc.Close()

	bindFrame, err := codec.EncodePDU(nil, codec.Header{CommandID: protocol.CommandBindTransceiver, CommandStatus: protocol.StatusOK, SequenceNumber: 1},
		protocol.BindRequest{SystemID: []byte("esme"), Password: []byte("pw"), SystemType: []byte("vms"), InterfaceVersion: protocol.InterfaceVersion34}, registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := transport.WriteFull(esmeConn, bindFrame); err != nil {
		t.Fatal(err)
	}
	bindResp, err := readOnePDU(esmeConn, registry)
	if err != nil || !bindResp.Header.CommandStatus.OK() {
		t.Fatalf("bind_transceiver failed: resp=%+v err=%v", bindResp.Header, err)
	}

	vendorFrame, err := codec.EncodePDU(nil, codec.Header{CommandID: vendorESMECommand, CommandStatus: protocol.StatusOK, SequenceNumber: 2}, protocol.EmptyBody{}, registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := transport.WriteFull(esmeConn, vendorFrame); err != nil {
		t.Fatal(err)
	}
	resp, err := readOnePDU(esmeConn, registry)
	if err != nil {
		t.Fatalf("read vendor command response: %v", err)
	}
	if !resp.Header.CommandStatus.OK() {
		t.Fatalf("status = %#x, want StatusOK — correct role and state should reach the handler", uint32(resp.Header.CommandStatus))
	}
	if !handlerCalled {
		t.Fatal("handler was not invoked despite correct role and state")
	}
}
