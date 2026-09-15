package codec

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/majiddarvishan/go-smpp/protocol"
)

func mustSMPP34Registry(t *testing.T) *Registry {
	t.Helper()
	registry, err := NewSMPP34Registry(RegistryCompatible)
	if err != nil {
		t.Fatalf("new SMPP 3.4 registry: %v", err)
	}
	return registry
}

func TestSMPP34RegistryEssentialCommandsAndExtensionPoint(t *testing.T) {
	builder, err := NewSMPP34RegistryBuilder(RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	vendorCommand := protocol.CommandID(0x00010234)
	vendorTLV := uint16(0x1400)
	if err := builder.RegisterCommand(CommandDefinition{ID: vendorCommand, Name: "vendor_command"}); err != nil {
		t.Fatalf("register vendor command: %v", err)
	}
	if err := builder.RegisterTLV(TLVDefinition{Tag: vendorTLV, Name: "vendor_tlv"}); err != nil {
		t.Fatalf("register vendor TLV: %v", err)
	}
	registry, err := builder.Freeze()
	if err != nil {
		t.Fatal(err)
	}

	commands := []protocol.CommandID{
		protocol.CommandBindTransmitter, protocol.CommandBindTransmitterResp,
		protocol.CommandBindReceiver, protocol.CommandBindReceiverResp,
		protocol.CommandBindTransceiver, protocol.CommandBindTransceiverResp,
		protocol.CommandUnbind, protocol.CommandUnbindResp,
		protocol.CommandEnquireLink, protocol.CommandEnquireLinkResp,
		protocol.CommandGenericNACK,
		protocol.CommandSubmitSM, protocol.CommandSubmitSMResp,
		protocol.CommandDeliverSM, protocol.CommandDeliverSMResp,
		vendorCommand,
	}
	for _, id := range commands {
		if _, ok := registry.Command(id); !ok {
			t.Fatalf("command 0x%08x not registered", uint32(id))
		}
	}
	if _, ok := registry.TLV(vendorTLV); !ok {
		t.Fatal("vendor TLV not registered")
	}
}

func TestBindTransmitterSpecificationVector(t *testing.T) {
	registry := mustSMPP34Registry(t)
	header := Header{CommandID: protocol.CommandBindTransmitter, SequenceNumber: 1}
	body := protocol.BindRequest{
		SystemID: []byte("esme"), Password: []byte("pwd"), InterfaceVersion: protocol.InterfaceVersion34,
		AddressTON: protocol.TONInternational, AddressNPI: protocol.NPIISDN,
	}
	encoded, err := EncodePDU(nil, header, body, registry)
	if err != nil {
		t.Fatal(err)
	}
	expected := []byte{
		0x00, 0x00, 0x00, 0x1e,
		0x00, 0x00, 0x00, 0x02,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x01,
		'e', 's', 'm', 'e', 0x00,
		'p', 'w', 'd', 0x00,
		0x00,
		0x34,
		0x01,
		0x01,
		0x00,
	}
	if !bytes.Equal(encoded, expected) {
		t.Fatalf("bind vector mismatch\n got: %x\nwant: %x", encoded, expected)
	}

	decoded, err := DecodePDU(encoded, registry)
	if err != nil {
		t.Fatal(err)
	}
	bind, ok := decoded.Body.(protocol.BindRequest)
	if !ok {
		t.Fatalf("unexpected body type %T", decoded.Body)
	}
	if string(bind.SystemID) != "esme" || string(bind.Password) != "pwd" || bind.InterfaceVersion != protocol.InterfaceVersion34 {
		t.Fatalf("unexpected bind body: %+v", bind)
	}
}

func TestBindResponsePreservesOptionalTLV(t *testing.T) {
	registry := mustSMPP34Registry(t)
	header := Header{CommandID: protocol.CommandBindTransmitterResp, SequenceNumber: 9}
	body := protocol.BindResponse{
		SystemID: []byte("smsc"),
		Optional: []protocol.OptionalParameter{{Tag: protocol.TLVTagSCInterfaceVersion, Value: []byte{0x34}}},
	}
	encoded, err := EncodePDU(nil, header, body, registry)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodePDU(encoded, registry)
	if err != nil {
		t.Fatal(err)
	}
	response := decoded.Body.(protocol.BindResponse)
	if string(response.SystemID) != "smsc" || len(response.Optional) != 1 {
		t.Fatalf("unexpected bind response: %+v", response)
	}
	if response.Optional[0].Tag != protocol.TLVTagSCInterfaceVersion || !bytes.Equal(response.Optional[0].Value, []byte{0x34}) {
		t.Fatalf("optional TLV not preserved: %+v", response.Optional)
	}
}

func TestSubmitSMSpecificationVector(t *testing.T) {
	registry := mustSMPP34Registry(t)
	header := Header{CommandID: protocol.CommandSubmitSM, SequenceNumber: 7}
	body := protocol.SubmitSM{
		SourceAddrTON: protocol.TONInternational, SourceAddrNPI: protocol.NPIISDN, SourceAddr: []byte("123"),
		DestAddrTON: protocol.TONInternational, DestAddrNPI: protocol.NPIISDN, DestinationAddr: []byte("456"),
		RegisteredDelivery: 1, ShortMessage: []byte("Hi"),
		Optional: []protocol.OptionalParameter{{Tag: protocol.TLVTagSARMsgRefNum, Value: []byte{0x12, 0x34}}},
	}
	encoded, err := EncodePDU(nil, header, body, registry)
	if err != nil {
		t.Fatal(err)
	}
	expected := []byte{
		0x00, 0x00, 0x00, 0x2f,
		0x00, 0x00, 0x00, 0x04,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x07,
		0x00,
		0x01, 0x01, '1', '2', '3', 0x00,
		0x01, 0x01, '4', '5', '6', 0x00,
		0x00, 0x00, 0x00,
		0x00,
		0x00,
		0x01, 0x00, 0x00, 0x00,
		0x02, 'H', 'i',
		0x02, 0x0c, 0x00, 0x02, 0x12, 0x34,
	}
	if !bytes.Equal(encoded, expected) {
		t.Fatalf("submit_sm vector mismatch\n got: %x\nwant: %x", encoded, expected)
	}
	decoded, err := DecodePDU(encoded, registry)
	if err != nil {
		t.Fatal(err)
	}
	submit, ok := decoded.Body.(protocol.SubmitSM)
	if !ok {
		t.Fatalf("unexpected body type %T", decoded.Body)
	}
	if string(submit.SourceAddr) != "123" || string(submit.DestinationAddr) != "456" || string(submit.ShortMessage) != "Hi" {
		t.Fatalf("unexpected submit body: %+v", submit)
	}
	if len(submit.Optional) != 1 || submit.Optional[0].Tag != protocol.TLVTagSARMsgRefNum || !bytes.Equal(submit.Optional[0].Value, []byte{0x12, 0x34}) {
		t.Fatalf("submit optional parameters not preserved: %+v", submit.Optional)
	}
}

func TestDeliverSMRoundTripPreservesDuplicateUnknownTLVs(t *testing.T) {
	registry := mustSMPP34Registry(t)
	body := protocol.DeliverSM{
		SourceAddrTON: protocol.TONInternational, SourceAddrNPI: protocol.NPIISDN, SourceAddr: []byte("111"),
		DestAddrTON: protocol.TONInternational, DestAddrNPI: protocol.NPIISDN, DestinationAddr: []byte("222"),
		ShortMessage: []byte("ok"),
		Optional: []protocol.OptionalParameter{
			{Tag: 0x1400, Value: []byte{1}},
			{Tag: 0x1400, Value: []byte{2}},
		},
	}
	encoded, err := EncodePDU(nil, Header{CommandID: protocol.CommandDeliverSM, SequenceNumber: 11}, body, registry)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodePDU(encoded, registry)
	if err != nil {
		t.Fatal(err)
	}
	deliver := decoded.Body.(protocol.DeliverSM)
	if len(deliver.Optional) != 2 || deliver.Optional[0].Tag != 0x1400 || deliver.Optional[1].Tag != 0x1400 || deliver.Optional[0].Value[0] != 1 || deliver.Optional[1].Value[0] != 2 {
		t.Fatalf("duplicate/order preservation failed: %+v", deliver.Optional)
	}
}

func TestHeaderOnlyEssentialPDUs(t *testing.T) {
	registry := mustSMPP34Registry(t)
	cases := []struct {
		id     protocol.CommandID
		status protocol.CommandStatus
		seq    protocol.SequenceNumber
	}{
		{protocol.CommandUnbind, protocol.StatusOK, 1},
		{protocol.CommandUnbindResp, protocol.StatusOK, 2},
		{protocol.CommandEnquireLink, protocol.StatusOK, 3},
		{protocol.CommandEnquireLinkResp, protocol.StatusOK, 4},
		{protocol.CommandGenericNACK, protocol.StatusInvalidCommandID, 0},
	}
	for _, tc := range cases {
		encoded, err := EncodePDU(nil, Header{CommandID: tc.id, CommandStatus: tc.status, SequenceNumber: tc.seq}, protocol.EmptyBody{}, registry)
		if err != nil {
			t.Fatalf("encode 0x%08x: %v", uint32(tc.id), err)
		}
		if len(encoded) != HeaderSize || binary.BigEndian.Uint32(encoded[:4]) != HeaderSize {
			t.Fatalf("command 0x%08x must be header-only: %x", uint32(tc.id), encoded)
		}
		if _, err := DecodePDU(encoded, registry); err != nil {
			t.Fatalf("decode 0x%08x: %v", uint32(tc.id), err)
		}
	}
}

func TestResponseHeaderPreservesFullInboundSequenceRange(t *testing.T) {
	registry := mustSMPP34Registry(t)
	request := Header{CommandID: protocol.CommandDeliverSM, SequenceNumber: 0xffffffff}
	response := ResponseHeader(request, protocol.CommandDeliverSMResp, protocol.StatusOK)
	if response.SequenceNumber != 0xffffffff {
		t.Fatalf("sequence changed: got 0x%08x", uint32(response.SequenceNumber))
	}
	encoded, err := EncodePDU(nil, response, protocol.DeliverSMResp{}, registry)
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.BigEndian.Uint32(encoded[12:16]); got != 0xffffffff {
		t.Fatalf("wire sequence changed: 0x%08x", got)
	}
}

func TestUnsuccessfulSubmitResponseMayBeHeaderOnly(t *testing.T) {
	registry := mustSMPP34Registry(t)
	header := Header{CommandID: protocol.CommandSubmitSMResp, CommandStatus: protocol.StatusThrottled, SequenceNumber: 17}
	encoded, err := EncodePDU(nil, header, protocol.EmptyBody{}, registry)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) != HeaderSize {
		t.Fatalf("error submit response must have no body: %x", encoded)
	}
	decoded, err := DecodePDU(encoded, registry)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Header.CommandStatus != protocol.StatusThrottled {
		t.Fatalf("status changed: %v", decoded.Header.CommandStatus)
	}
}

func TestMessagePayloadConflictsWithShortMessage(t *testing.T) {
	registry := mustSMPP34Registry(t)
	body := protocol.SubmitSM{
		ShortMessage: []byte("x"),
		Optional: []protocol.OptionalParameter{{Tag: protocol.TLVTagMessagePayload, Value: []byte("payload")}},
	}
	_, err := EncodePDU(nil, Header{CommandID: protocol.CommandSubmitSM, SequenceNumber: 1}, body, registry)
	if !errors.Is(err, ErrConflictingMessageData) {
		t.Fatalf("expected message-data conflict, got %v", err)
	}
}

func TestMalformedMandatoryCStringRemainsFatal(t *testing.T) {
	registry := mustSMPP34Registry(t)
	frame := make([]byte, HeaderSize+16)
	if err := EncodeHeader(frame[:HeaderSize], Header{
		CommandLength: uint32(len(frame)), CommandID: protocol.CommandBindTransmitter, SequenceNumber: 1,
	}); err != nil {
		t.Fatal(err)
	}
	for i := HeaderSize; i < len(frame); i++ {
		frame[i] = 'A'
	}
	_, err := DecodePDU(frame, registry)
	var fatal *protocol.FatalError
	if !errors.As(err, &fatal) {
		t.Fatalf("expected fatal structural error, got %T %v", err, err)
	}
}
