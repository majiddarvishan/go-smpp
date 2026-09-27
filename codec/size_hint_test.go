package codec

import (
	"testing"

	"github.com/majiddarvishan/go-smpp/protocol"
)

// TestEncodedPDUSizeHintCoversRealEncodings is the acceptance test for
// Task 2.6: for every command a size hint is offered for, encode a
// realistic body and assert the hint (header + body) is never smaller than
// the frame EncodePDU actually produced. A hint that undershoots would mean
// a pool buffer sized from it needs a mid-encode regrowth on every use — not
// a correctness bug (EncodePDU always grows what it's given regardless), but
// exactly the "undersized on first use" cost EncodedPDUSizeHint exists to
// avoid, so it is worth its own hard failure rather than only showing up as
// a quieter benchmark regression.
func TestEncodedPDUSizeHintCoversRealEncodings(t *testing.T) {
	registry, err := NewSMPP50Registry(RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}

	optional := []protocol.OptionalParameter{
		{Tag: 0x0201, Value: []byte("tlv-value-one")},
		{Tag: 0x0202, Value: []byte("tlv-value-two-longer")},
	}

	cases := []struct {
		name    string
		command protocol.CommandID
		body    any
	}{
		{"submit_sm", protocol.CommandSubmitSM, protocol.SubmitSM{
			ServiceType: []byte("SVC"), SourceAddr: []byte("12025550100"), DestinationAddr: []byte("12025550101"),
			ScheduleDeliveryTime: []byte("240101000000000+"), ValidityPeriod: []byte("240102000000000+"),
			ShortMessage: []byte("hello world"), Optional: optional,
		}},
		{"deliver_sm", protocol.CommandDeliverSM, protocol.DeliverSM{
			ServiceType: []byte("SVC"), SourceAddr: []byte("12025550101"), DestinationAddr: []byte("12025550100"),
			ShortMessage: []byte("delivery report"), Optional: optional,
		}},
		{"submit_sm_resp", protocol.CommandSubmitSMResp, protocol.SubmitSMResp{MessageID: []byte("msg-0001"), Optional: optional}},
		{"submit_sm_resp_empty", protocol.CommandSubmitSMResp, protocol.EmptyBody{}},
		{"submit_sm_resp_optional_only", protocol.CommandSubmitSMResp, protocol.OptionalResponse{Optional: optional}},
		{"deliver_sm_resp", protocol.CommandDeliverSMResp, protocol.DeliverSMResp{MessageID: []byte("msg-0002"), Optional: optional}},
		{"deliver_sm_resp_empty", protocol.CommandDeliverSMResp, protocol.EmptyBody{}},

		{"bind_transmitter", protocol.CommandBindTransmitter, protocol.BindRequest{
			SystemID: []byte("esme-1"), Password: []byte("secret12"), SystemType: []byte("VMS"),
			InterfaceVersion: protocol.InterfaceVersion34, AddressRange: []byte("1234"),
		}},
		{"bind_receiver", protocol.CommandBindReceiver, protocol.BindRequest{
			SystemID: []byte("esme-2"), Password: []byte("secret34"), SystemType: []byte("VMS"),
			InterfaceVersion: protocol.InterfaceVersion34,
		}},
		{"bind_transceiver", protocol.CommandBindTransceiver, protocol.BindRequest{
			SystemID: []byte("esme-3"), Password: []byte("secret56"), SystemType: []byte("VMS"),
			InterfaceVersion: protocol.InterfaceVersion34,
		}},
		{"bind_transmitter_resp", protocol.CommandBindTransmitterResp, protocol.BindResponse{SystemID: []byte("smsc"), Optional: optional}},
		{"bind_receiver_resp", protocol.CommandBindReceiverResp, protocol.BindResponse{SystemID: []byte("smsc"), Optional: optional}},
		{"bind_transceiver_resp", protocol.CommandBindTransceiverResp, protocol.BindResponse{SystemID: []byte("smsc"), Optional: optional}},
		{"bind_transceiver_resp_empty", protocol.CommandBindTransceiverResp, protocol.EmptyBody{}},

		{"data_sm", protocol.CommandDataSM, protocol.DataSM{
			ServiceType: []byte("SVC"), SourceAddr: []byte("12025550100"), DestinationAddr: []byte("12025550101"),
			Optional: optional,
		}},
		{"data_sm_resp", protocol.CommandDataSMResp, protocol.DataSMResp{MessageID: []byte("msg-0003"), Optional: optional}},
		{"data_sm_resp_empty", protocol.CommandDataSMResp, protocol.EmptyBody{}},

		{"broadcast_sm", protocol.CommandBroadcastSM, protocol.BroadcastSM{
			ServiceType: []byte("SVC"), SourceAddr: []byte("12025550100"), MessageID: []byte("bcast-0001"),
			ScheduleDeliveryTime: []byte("240101000000000+"), ValidityPeriod: []byte("240102000000000+"),
			Optional: optional,
		}},
		{"broadcast_sm_resp", protocol.CommandBroadcastSMResp, protocol.BroadcastSMResp{MessageID: []byte("bcast-0001"), Optional: optional}},
		{"broadcast_sm_resp_empty", protocol.CommandBroadcastSMResp, protocol.EmptyBody{}},

		{"query_broadcast_sm", protocol.CommandQueryBroadcastSM, protocol.QueryBroadcastSM{
			MessageID: []byte("bcast-0001"), SourceAddr: []byte("12025550100"), Optional: optional,
		}},
		{"query_broadcast_sm_resp", protocol.CommandQueryBroadcastSMResp, protocol.QueryBroadcastSMResp{MessageID: []byte("bcast-0001"), Optional: optional}},
		{"query_broadcast_sm_resp_empty", protocol.CommandQueryBroadcastSMResp, protocol.EmptyBody{}},

		{"cancel_broadcast_sm", protocol.CommandCancelBroadcastSM, protocol.CancelBroadcastSM{
			ServiceType: []byte("SVC"), MessageID: []byte("bcast-0001"), SourceAddr: []byte("12025550100"), Optional: optional,
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hint, ok := EncodedPDUSizeHint(tc.command, tc.body)
			if !ok {
				t.Fatalf("EncodedPDUSizeHint reported no estimate for %s; this case should be covered", tc.name)
			}
			header := Header{CommandID: tc.command, CommandStatus: protocol.StatusOK, SequenceNumber: 1}
			frame, err := EncodePDU(nil, header, tc.body, registry)
			if err != nil {
				t.Fatalf("EncodePDU: %v", err)
			}
			if hint < len(frame) {
				t.Fatalf("hint = %d bytes, actual encoded frame = %d bytes — hint undershoots", hint, len(frame))
			}
		})
	}
}
