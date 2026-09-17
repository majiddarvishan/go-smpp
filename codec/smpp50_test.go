package codec

import (
	"testing"

	"github.com/majiddarvishan/go-smpp/protocol"
)

func mustSMPP50Registry(t *testing.T) *Registry {
	t.Helper()
	registry, err := NewSMPP50Registry(RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func TestSMPP50RegistryExtendsSMPP34(t *testing.T) {
	registry := mustSMPP50Registry(t)
	if registry.CommandCount() != 33 {
		t.Fatalf("command count=%d want 33", registry.CommandCount())
	}
	if registry.TLVCount() != 64 {
		t.Fatalf("TLV count=%d want 64", registry.TLVCount())
	}
	for _, id := range []protocol.CommandID{
		protocol.CommandBroadcastSM, protocol.CommandBroadcastSMResp,
		protocol.CommandQueryBroadcastSM, protocol.CommandQueryBroadcastSMResp,
		protocol.CommandCancelBroadcastSM, protocol.CommandCancelBroadcastSMResp,
	} {
		if def, ok := registry.Command(id); !ok || def.Decode == nil || def.Encode == nil {
			t.Fatalf("missing v5 command 0x%08x: %+v", uint32(id), def)
		}
	}
	for _, tag := range []uint16{
		protocol.TLVTagCongestionState, protocol.TLVTagBroadcastChannelIndicator,
		protocol.TLVTagBroadcastContentType, protocol.TLVTagBroadcastAreaIdentifier,
		protocol.TLVTagBillingIdentification, protocol.TLVTagSourceNetworkID,
		protocol.TLVTagDestNetworkID, protocol.TLVTagSourceNodeID, protocol.TLVTagDestNodeID,
		protocol.TLVTagDestAddrNPResolution, protocol.TLVTagDestAddrNPInformation, protocol.TLVTagDestAddrNPCountry,
	} {
		if def, ok := registry.TLV(tag); !ok || def.Name == "" {
			t.Fatalf("missing v5 TLV 0x%04x", tag)
		}
	}

	v34, err := NewSMPP34Registry(RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := v34.Command(protocol.CommandBroadcastSM); ok {
		t.Fatal("SMPP 3.4 registry unexpectedly contains broadcast_sm")
	}
	if _, ok := v34.TLV(protocol.TLVTagCongestionState); ok {
		t.Fatal("SMPP 3.4 registry unexpectedly contains congestion_state")
	}
}

func TestSMPP50BroadcastRoundTrips(t *testing.T) {
	registry := mustSMPP50Registry(t)
	tests := []struct {
		name  string
		id    protocol.CommandID
		body  any
		check func(t *testing.T, body any)
	}{
		{
			name: "broadcast_sm",
			id:   protocol.CommandBroadcastSM,
			body: protocol.BroadcastSM{
				ServiceType: []byte("svc"), SourceAddrTON: 1, SourceAddrNPI: 1, SourceAddr: []byte("123"),
				PriorityFlag: 1, DataCoding: protocol.DataCodingUCS2,
				Optional: []protocol.OptionalParameter{
					{Tag: protocol.TLVTagBroadcastAreaIdentifier, Value: []byte{0, 'A'}},
					{Tag: protocol.TLVTagBroadcastContentType, Value: []byte{0, 0, 1}},
					{Tag: protocol.TLVTagBroadcastFrequencyInterval, Value: []byte{9, 0, 31}},
					{Tag: protocol.TLVTagBroadcastRepNum, Value: []byte{0, 5}},
				},
			},
			check: func(t *testing.T, body any) {
				got := body.(protocol.BroadcastSM)
				if string(got.SourceAddr) != "123" || len(got.Optional) != 4 || got.Optional[0].Tag != protocol.TLVTagBroadcastAreaIdentifier {
					t.Fatalf("broadcast_sm=%+v", got)
				}
			},
		},
		{
			name: "broadcast_sm_resp", id: protocol.CommandBroadcastSMResp,
			body: protocol.BroadcastSMResp{MessageID: []byte("b-1"), Optional: []protocol.OptionalParameter{{Tag: protocol.TLVTagCongestionState, Value: []byte{85}}}},
			check: func(t *testing.T, body any) {
				got := body.(protocol.BroadcastSMResp)
				if string(got.MessageID) != "b-1" || len(got.Optional) != 1 || got.Optional[0].Value[0] != 85 {
					t.Fatalf("broadcast_sm_resp=%+v", got)
				}
			},
		},
		{
			name: "query_broadcast_sm", id: protocol.CommandQueryBroadcastSM,
			body: protocol.QueryBroadcastSM{MessageID: []byte("b-1"), SourceAddrTON: 1, SourceAddrNPI: 1, SourceAddr: []byte("123"), Optional: []protocol.OptionalParameter{{Tag: protocol.TLVTagUserMessageReference, Value: []byte{0, 9}}}},
			check: func(t *testing.T, body any) {
				got := body.(protocol.QueryBroadcastSM)
				if string(got.MessageID) != "b-1" || len(got.Optional) != 1 {
					t.Fatalf("query_broadcast_sm=%+v", got)
				}
			},
		},
		{
			name: "query_broadcast_sm_resp", id: protocol.CommandQueryBroadcastSMResp,
			body: protocol.QueryBroadcastSMResp{MessageID: []byte("b-1"), Optional: []protocol.OptionalParameter{{Tag: protocol.TLVTagMessageState, Value: []byte{byte(protocol.MessageStateDelivered)}}}},
			check: func(t *testing.T, body any) {
				got := body.(protocol.QueryBroadcastSMResp)
				if string(got.MessageID) != "b-1" || len(got.Optional) != 1 {
					t.Fatalf("query_broadcast_sm_resp=%+v", got)
				}
			},
		},
		{
			name: "cancel_broadcast_sm", id: protocol.CommandCancelBroadcastSM,
			body: protocol.CancelBroadcastSM{ServiceType: []byte("svc"), MessageID: []byte("b-1"), SourceAddrTON: 1, SourceAddrNPI: 1, SourceAddr: []byte("123"), Optional: []protocol.OptionalParameter{{Tag: protocol.TLVTagBroadcastContentType, Value: []byte{0, 0, 1}}}},
			check: func(t *testing.T, body any) {
				got := body.(protocol.CancelBroadcastSM)
				if string(got.MessageID) != "b-1" || len(got.Optional) != 1 {
					t.Fatalf("cancel_broadcast_sm=%+v", got)
				}
			},
		},
		{
			name: "cancel_broadcast_sm_resp", id: protocol.CommandCancelBroadcastSMResp, body: protocol.EmptyBody{},
			check: func(t *testing.T, body any) { _ = body.(protocol.EmptyBody) },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status := protocol.StatusOK
			frame, err := EncodePDU(nil, Header{CommandID: tc.id, CommandStatus: status, SequenceNumber: 9}, tc.body, registry)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := DecodePDU(frame, registry)
			if err != nil {
				t.Fatal(err)
			}
			tc.check(t, decoded.Body)
		})
	}
}

func TestSMPP50CongestionStateOnSubmitResponse(t *testing.T) {
	registry := mustSMPP50Registry(t)
	body := protocol.SubmitSMResp{MessageID: []byte("m1"), Optional: []protocol.OptionalParameter{{Tag: protocol.TLVTagCongestionState, Value: []byte{90}}}}
	frame, err := EncodePDU(nil, Header{CommandID: protocol.CommandSubmitSMResp, SequenceNumber: 7}, body, registry)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodePDU(frame, registry)
	if err != nil {
		t.Fatal(err)
	}
	got := decoded.Body.(protocol.SubmitSMResp)
	state, present, err := protocol.CongestionStateFromOptional(got.Optional)
	if err != nil || !present || state != 90 {
		t.Fatalf("state=%d present=%v err=%v body=%+v", state, present, err, got)
	}
}

func TestSMPP50CongestionStateOnHeaderOnlyResponse(t *testing.T) {
	registry := mustSMPP50Registry(t)
	body := protocol.OptionalResponse{Optional: []protocol.OptionalParameter{{Tag: protocol.TLVTagCongestionState, Value: []byte{82}}}}
	frame, err := EncodePDU(nil, Header{CommandID: protocol.CommandEnquireLinkResp, SequenceNumber: 11}, body, registry)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodePDU(frame, registry)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := decoded.Body.(protocol.OptionalResponse)
	if !ok {
		t.Fatalf("body type=%T", decoded.Body)
	}
	state, present, err := protocol.CongestionStateFromOptional(got.Optional)
	if err != nil || !present || state != 82 {
		t.Fatalf("state=%d present=%v err=%v body=%+v", state, present, err, got)
	}
}

func TestSMPP50CongestionStateOnErrorResponseWithoutStandardBody(t *testing.T) {
	registry := mustSMPP50Registry(t)
	body := protocol.OptionalResponse{Optional: []protocol.OptionalParameter{{Tag: protocol.TLVTagCongestionState, Value: []byte{96}}}}
	frame, err := EncodePDU(nil, Header{
		CommandID: protocol.CommandSubmitSMResp, CommandStatus: protocol.StatusThrottled, SequenceNumber: 12,
	}, body, registry)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodePDU(frame, registry)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := decoded.Body.(protocol.OptionalResponse)
	if !ok {
		t.Fatalf("body type=%T", decoded.Body)
	}
	state, present, err := protocol.CongestionStateFromOptional(got.Optional)
	if err != nil || !present || state != 96 {
		t.Fatalf("state=%d present=%v err=%v body=%+v", state, present, err, got)
	}
}
