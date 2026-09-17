package codec

import (
	"bytes"
	"errors"
	"testing"

	"github.com/majiddarvishan/go-smpp/protocol"
)

func TestSMPP34RegistryCompleteCommandSet(t *testing.T) {
	registry := mustSMPP34Registry(t)
	commands := []protocol.CommandID{
		protocol.CommandGenericNACK,
		protocol.CommandBindReceiver, protocol.CommandBindReceiverResp,
		protocol.CommandBindTransmitter, protocol.CommandBindTransmitterResp,
		protocol.CommandQuerySM, protocol.CommandQuerySMResp,
		protocol.CommandSubmitSM, protocol.CommandSubmitSMResp,
		protocol.CommandDeliverSM, protocol.CommandDeliverSMResp,
		protocol.CommandUnbind, protocol.CommandUnbindResp,
		protocol.CommandReplaceSM, protocol.CommandReplaceSMResp,
		protocol.CommandCancelSM, protocol.CommandCancelSMResp,
		protocol.CommandBindTransceiver, protocol.CommandBindTransceiverResp,
		protocol.CommandOutbind,
		protocol.CommandEnquireLink, protocol.CommandEnquireLinkResp,
		protocol.CommandSubmitMulti, protocol.CommandSubmitMultiResp,
		protocol.CommandAlertNotification,
		protocol.CommandDataSM, protocol.CommandDataSMResp,
	}
	if len(commands) != 27 {
		t.Fatalf("test command matrix has %d entries, want 27", len(commands))
	}
	for _, id := range commands {
		def, ok := registry.Command(id)
		if !ok {
			t.Fatalf("SMPP 3.4 command 0x%08x not registered", uint32(id))
		}
		if def.Name == "" || def.Decode == nil || def.Encode == nil {
			t.Fatalf("incomplete command definition for 0x%08x: %+v", uint32(id), def)
		}
	}
}

func TestSMPP34RegistryCompleteTLVSet(t *testing.T) {
	registry := mustSMPP34Registry(t)
	tags := []uint16{
		protocol.TLVTagDestAddrSubunit, protocol.TLVTagDestNetworkType, protocol.TLVTagDestBearerType, protocol.TLVTagDestTelematicsID,
		protocol.TLVTagSourceAddrSubunit, protocol.TLVTagSourceNetworkType, protocol.TLVTagSourceBearerType, protocol.TLVTagSourceTelematicsID,
		protocol.TLVTagQOSTimeToLive, protocol.TLVTagPayloadType, protocol.TLVTagAdditionalStatusInfoText, protocol.TLVTagReceiptedMessageID,
		protocol.TLVTagMSMsgWaitFacilities, protocol.TLVTagPrivacyIndicator, protocol.TLVTagSourceSubaddress, protocol.TLVTagDestSubaddress,
		protocol.TLVTagUserMessageReference, protocol.TLVTagUserResponseCode, protocol.TLVTagSourcePort, protocol.TLVTagDestinationPort,
		protocol.TLVTagSARMsgRefNum, protocol.TLVTagLanguageIndicator, protocol.TLVTagSARTotalSegments, protocol.TLVTagSARSegmentSeqnum,
		protocol.TLVTagSCInterfaceVersion, protocol.TLVTagCallbackNumPresInd, protocol.TLVTagCallbackNumAtag, protocol.TLVTagNumberOfMessages,
		protocol.TLVTagCallbackNum, protocol.TLVTagDPFResult, protocol.TLVTagSetDPF, protocol.TLVTagMSAvailabilityStatus,
		protocol.TLVTagNetworkErrorCode, protocol.TLVTagMessagePayload, protocol.TLVTagDeliveryFailureReason, protocol.TLVTagMoreMessagesToSend,
		protocol.TLVTagMessageState, protocol.TLVTagUSSDServiceOp, protocol.TLVTagDisplayTime, protocol.TLVTagSMSSignal,
		protocol.TLVTagMSValidity, protocol.TLVTagAlertOnMessageDelivery, protocol.TLVTagITSReplyType, protocol.TLVTagITSSessionInfo,
	}
	if len(tags) != 44 {
		t.Fatalf("test TLV matrix has %d entries, want 44", len(tags))
	}
	seen := make(map[uint16]struct{}, len(tags))
	for _, tag := range tags {
		if _, dup := seen[tag]; dup {
			t.Fatalf("duplicate TLV in test matrix: 0x%04x", tag)
		}
		seen[tag] = struct{}{}
		if def, ok := registry.TLV(tag); !ok || def.Name == "" {
			t.Fatalf("SMPP 3.4 TLV 0x%04x not registered", tag)
		}
	}
}

func TestSMPP34AdditionalPDURoundTrips(t *testing.T) {
	registry := mustSMPP34Registry(t)
	tests := []struct {
		name  string
		id    protocol.CommandID
		body  any
		check func(t *testing.T, got any)
	}{
		{"data_sm", protocol.CommandDataSM, protocol.DataSM{ServiceType: []byte("svc"), SourceAddrTON: 1, SourceAddrNPI: 1, SourceAddr: []byte("123"), DestAddrTON: 1, DestAddrNPI: 1, DestinationAddr: []byte("456"), ESMClass: 3, RegisteredDelivery: 1, DataCoding: protocol.DataCodingUCS2, Optional: []protocol.OptionalParameter{{Tag: protocol.TLVTagMessagePayload, Value: []byte{1, 2, 3}}}}, func(t *testing.T, got any) {
			v := got.(protocol.DataSM)
			if string(v.SourceAddr) != "123" || string(v.DestinationAddr) != "456" || len(v.Optional) != 1 {
				t.Fatalf("data_sm=%+v", v)
			}
		}},
		{"data_sm_resp", protocol.CommandDataSMResp, protocol.DataSMResp{MessageID: []byte("d1"), Optional: []protocol.OptionalParameter{{Tag: protocol.TLVTagDPFResult, Value: []byte{1}}}}, func(t *testing.T, got any) {
			v := got.(protocol.DataSMResp)
			if string(v.MessageID) != "d1" || len(v.Optional) != 1 {
				t.Fatalf("data_sm_resp=%+v", v)
			}
		}},
		{"query_sm", protocol.CommandQuerySM, protocol.QuerySM{MessageID: []byte("m1"), SourceAddrTON: 1, SourceAddrNPI: 1, SourceAddr: []byte("123")}, func(t *testing.T, got any) {
			v := got.(protocol.QuerySM)
			if string(v.MessageID) != "m1" || string(v.SourceAddr) != "123" {
				t.Fatalf("query_sm=%+v", v)
			}
		}},
		{"query_sm_resp", protocol.CommandQuerySMResp, protocol.QuerySMResp{MessageID: []byte("m1"), FinalDate: []byte("260917210000000+"), MessageState: protocol.MessageStateDelivered, ErrorCode: 0}, func(t *testing.T, got any) {
			v := got.(protocol.QuerySMResp)
			if v.MessageState != protocol.MessageStateDelivered || string(v.FinalDate) != "260917210000000+" {
				t.Fatalf("query_sm_resp=%+v", v)
			}
		}},
		{"cancel_sm", protocol.CommandCancelSM, protocol.CancelSM{ServiceType: []byte("svc"), MessageID: []byte("m1"), SourceAddrTON: 1, SourceAddrNPI: 1, SourceAddr: []byte("123"), DestAddrTON: 1, DestAddrNPI: 1, DestinationAddr: []byte("456")}, func(t *testing.T, got any) {
			v := got.(protocol.CancelSM)
			if string(v.MessageID) != "m1" || string(v.DestinationAddr) != "456" {
				t.Fatalf("cancel_sm=%+v", v)
			}
		}},
		{"replace_sm", protocol.CommandReplaceSM, protocol.ReplaceSM{MessageID: []byte("m1"), SourceAddrTON: 1, SourceAddrNPI: 1, SourceAddr: []byte("123"), RegisteredDelivery: 1, ShortMessage: []byte("replacement")}, func(t *testing.T, got any) {
			v := got.(protocol.ReplaceSM)
			if string(v.ShortMessage) != "replacement" {
				t.Fatalf("replace_sm=%+v", v)
			}
		}},
		{"alert_notification", protocol.CommandAlertNotification, protocol.AlertNotification{SourceAddrTON: 1, SourceAddrNPI: 1, SourceAddr: []byte("49123"), ESMEAddrTON: 1, ESMEAddrNPI: 1, ESMEAddr: []byte("49456"), Optional: []protocol.OptionalParameter{{Tag: protocol.TLVTagMSAvailabilityStatus, Value: []byte{0}}}}, func(t *testing.T, got any) {
			v := got.(protocol.AlertNotification)
			if string(v.SourceAddr) != "49123" || len(v.Optional) != 1 {
				t.Fatalf("alert=%+v", v)
			}
		}},
		{"outbind", protocol.CommandOutbind, protocol.Outbind{SystemID: []byte("smsc"), Password: []byte("secret")}, func(t *testing.T, got any) {
			v := got.(protocol.Outbind)
			if string(v.SystemID) != "smsc" || string(v.Password) != "secret" {
				t.Fatalf("outbind=%+v", v)
			}
		}},
		{"submit_multi", protocol.CommandSubmitMulti, protocol.SubmitMulti{SourceAddrTON: 1, SourceAddrNPI: 1, SourceAddr: []byte("123"), Destinations: []protocol.SubmitMultiDestination{{Flag: protocol.DestinationSME, TON: 1, NPI: 1, Address: []byte("111")}, {Flag: protocol.DestinationDistributionList, Address: []byte("group")}}, RegisteredDelivery: 1, ShortMessage: []byte("hi"), Optional: []protocol.OptionalParameter{{Tag: protocol.TLVTagSARMsgRefNum, Value: []byte{0, 1}}}}, func(t *testing.T, got any) {
			v := got.(protocol.SubmitMulti)
			if len(v.Destinations) != 2 || v.Destinations[1].Flag != protocol.DestinationDistributionList || string(v.Destinations[1].Address) != "group" {
				t.Fatalf("submit_multi=%+v", v)
			}
		}},
		{"submit_multi_resp", protocol.CommandSubmitMultiResp, protocol.SubmitMultiResp{MessageID: []byte("multi-1"), Unsuccessful: []protocol.UnsuccessfulSME{{DestAddrTON: 1, DestAddrNPI: 1, DestinationAddr: []byte("999"), ErrorStatusCode: protocol.StatusInvalidDestinationAddress}}}, func(t *testing.T, got any) {
			v := got.(protocol.SubmitMultiResp)
			if string(v.MessageID) != "multi-1" || len(v.Unsuccessful) != 1 || v.Unsuccessful[0].ErrorStatusCode != protocol.StatusInvalidDestinationAddress {
				t.Fatalf("submit_multi_resp=%+v", v)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			frame, err := EncodePDU(nil, Header{CommandID: tc.id, SequenceNumber: 0x80000001}, tc.body, registry)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := DecodePDU(frame, registry)
			if err != nil {
				t.Fatal(err)
			}
			if decoded.Header.SequenceNumber != 0x80000001 {
				t.Fatalf("sequence changed: 0x%08x", uint32(decoded.Header.SequenceNumber))
			}
			tc.check(t, decoded.Body)
		})
	}
}

func TestSMPP34SubmitMultiRejectsAmbiguousDestinationFlagAsFatal(t *testing.T) {
	registry := mustSMPP34Registry(t)
	body := protocol.SubmitMulti{Destinations: []protocol.SubmitMultiDestination{{Flag: protocol.DestinationSME, Address: []byte("1")}}}
	frame, err := EncodePDU(nil, Header{CommandID: protocol.CommandSubmitMulti, SequenceNumber: 1}, body, registry)
	if err != nil {
		t.Fatal(err)
	}
	// Body layout starts: service_type NULL, source TON, source NPI, source_addr
	// NULL, number_of_dests, dest_flag. Replace dest_flag with an unknown value.
	bodyOffset := HeaderSize
	flagOffset := bodyOffset + 1 + 1 + 1 + 1 + 1
	if frame[flagOffset] != byte(protocol.DestinationSME) {
		t.Fatalf("unexpected vector layout: %x", frame)
	}
	frame[flagOffset] = 0x7f
	_, err = DecodePDU(frame, registry)
	var fatal *protocol.FatalError
	if !errors.As(err, &fatal) || fatal.Kind != protocol.FatalFrameBoundaryLost {
		t.Fatalf("want fatal boundary error, got %v", err)
	}
}

func TestSMPP34MessagePayloadAndShortMessageRemainExclusiveForSubmitMulti(t *testing.T) {
	registry := mustSMPP34Registry(t)
	_, err := EncodePDU(nil, Header{CommandID: protocol.CommandSubmitMulti, SequenceNumber: 1}, protocol.SubmitMulti{
		Destinations: []protocol.SubmitMultiDestination{{Flag: protocol.DestinationSME, Address: []byte("1")}},
		ShortMessage: []byte("x"),
		Optional:     []protocol.OptionalParameter{{Tag: protocol.TLVTagMessagePayload, Value: []byte("y")}},
	}, registry)
	if !errors.Is(err, ErrConflictingMessageData) {
		t.Fatalf("want conflict, got %v", err)
	}
}

func TestSMPP34SubmitMultiResponseUsesNetworkOrderStatus(t *testing.T) {
	registry := mustSMPP34Registry(t)
	frame, err := EncodePDU(nil, Header{CommandID: protocol.CommandSubmitMultiResp, SequenceNumber: 7}, protocol.SubmitMultiResp{
		MessageID:    []byte("x"),
		Unsuccessful: []protocol.UnsuccessfulSME{{DestinationAddr: []byte("1"), ErrorStatusCode: protocol.StatusInvalidDestinationAddress}},
	}, registry)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(frame, []byte{0x00, 0x00, 0x00, 0x0b}) {
		t.Fatalf("status not encoded in network order: %x", frame)
	}
}
