package protocol

import (
	"errors"
	"testing"
)

func TestSMPP50CapabilityNegotiation(t *testing.T) {
	tests := []struct {
		name       string
		local      Profile
		peer       InterfaceVersion
		advertised bool
		want       PeerCapabilities
	}{
		{"v5-v5", SMPP50Profile(), InterfaceVersion50, true, PeerCapabilities{PeerVersion: InterfaceVersion50, NegotiatedVersion: InterfaceVersion50, VersionAdvertised: true, SupportsTLV: true, SupportsSMPP50: true}},
		{"v5-v34", SMPP50Profile(), InterfaceVersion34, true, PeerCapabilities{PeerVersion: InterfaceVersion34, NegotiatedVersion: InterfaceVersion34, VersionAdvertised: true, SupportsTLV: true}},
		{"v34-v5", SMPP34Profile(), InterfaceVersion50, true, PeerCapabilities{PeerVersion: InterfaceVersion50, NegotiatedVersion: InterfaceVersion34, VersionAdvertised: true, SupportsTLV: true}},
		{"missing-bind-response-version", SMPP50Profile(), 0, false, PeerCapabilities{}},
		{"legacy-peer", SMPP50Profile(), 0x33, true, PeerCapabilities{PeerVersion: 0x33, VersionAdvertised: true}},
		{"reserved-peer-version", SMPP50Profile(), 0x40, true, PeerCapabilities{PeerVersion: 0x40, VersionAdvertised: true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := NegotiatePeerCapabilities(tc.local, tc.peer, tc.advertised); got != tc.want {
				t.Fatalf("got %+v want %+v", got, tc.want)
			}
		})
	}
}

func TestSMPP50CongestionState(t *testing.T) {
	state, present, err := CongestionStateFromOptional([]OptionalParameter{{Tag: TLVTagCongestionState, Value: []byte{87}}})
	if err != nil || !present || state != 87 || !state.Valid() {
		t.Fatalf("state=%d present=%v err=%v", state, present, err)
	}
	if _, _, err := CongestionStateFromOptional([]OptionalParameter{{Tag: TLVTagCongestionState, Value: []byte{101}}}); !errors.Is(err, ErrInvalidCongestionState) {
		t.Fatalf("reserved congestion value err=%v", err)
	}
	if _, _, err := CongestionStateFromOptional([]OptionalParameter{{Tag: TLVTagCongestionState, Value: []byte{1, 2}}}); !errors.Is(err, ErrInvalidCongestionState) {
		t.Fatalf("invalid congestion length err=%v", err)
	}
}

func TestSMPP50Constants(t *testing.T) {
	commands := map[CommandID]uint32{
		CommandBroadcastSM: 0x00000111, CommandBroadcastSMResp: 0x80000111,
		CommandQueryBroadcastSM: 0x00000112, CommandQueryBroadcastSMResp: 0x80000112,
		CommandCancelBroadcastSM: 0x00000113, CommandCancelBroadcastSMResp: 0x80000113,
	}
	for got, want := range commands {
		if uint32(got) != want {
			t.Fatalf("command ID=0x%08x want 0x%08x", uint32(got), want)
		}
	}

	tags := map[uint16]uint16{
		TLVTagCongestionState:            0x0428,
		TLVTagBroadcastChannelIndicator:  0x0600,
		TLVTagBroadcastContentType:       0x0601,
		TLVTagBroadcastContentTypeInfo:   0x0602,
		TLVTagBroadcastMessageClass:      0x0603,
		TLVTagBroadcastRepNum:            0x0604,
		TLVTagBroadcastFrequencyInterval: 0x0605,
		TLVTagBroadcastAreaIdentifier:    0x0606,
		TLVTagBroadcastErrorStatus:       0x0607,
		TLVTagBroadcastAreaSuccess:       0x0608,
		TLVTagBroadcastEndTime:           0x0609,
		TLVTagBroadcastServiceGroup:      0x060A,
		TLVTagBillingIdentification:      0x060B,
		TLVTagSourceNetworkID:            0x060D,
		TLVTagDestNetworkID:              0x060E,
		TLVTagSourceNodeID:               0x060F,
		TLVTagDestNodeID:                 0x0610,
		TLVTagDestAddrNPResolution:       0x0611,
		TLVTagDestAddrNPInformation:      0x0612,
		TLVTagDestAddrNPCountry:          0x0613,
	}
	for got, want := range tags {
		if got != want {
			t.Fatalf("TLV tag=0x%04x want 0x%04x", got, want)
		}
	}
	if TLVTagFailedBroadcastAreaIdentifier != TLVTagBroadcastAreaIdentifier {
		t.Fatal("failed_broadcast_area_identifier must share broadcast_area_identifier wire tag")
	}

	statuses := map[CommandStatus]uint32{
		StatusServiceTypeUnauthorized: 0x00000100, StatusProhibited: 0x00000101,
		StatusServiceTypeUnavailable: 0x00000102, StatusServiceTypeDenied: 0x00000103,
		StatusInvalidDataCodingScheme: 0x00000104, StatusInvalidSourceAddrSubunit: 0x00000105,
		StatusInvalidDestAddrSubunit: 0x00000106, StatusInvalidBroadcastFrequency: 0x00000107,
		StatusInvalidBroadcastAliasName: 0x00000108, StatusInvalidBroadcastAreaFormat: 0x00000109,
		StatusInvalidNumberOfBroadcastAreas: 0x0000010A, StatusInvalidBroadcastContentType: 0x0000010B,
		StatusInvalidBroadcastMessageClass: 0x0000010C, StatusBroadcastFailed: 0x0000010D,
		StatusBroadcastQueryFailed: 0x0000010E, StatusBroadcastCancelFailed: 0x0000010F,
		StatusInvalidBroadcastRepetition: 0x00000110, StatusInvalidBroadcastServiceGroup: 0x00000111,
		StatusInvalidBroadcastChannel: 0x00000112,
	}
	for got, want := range statuses {
		if uint32(got) != want {
			t.Fatalf("status=0x%08x want 0x%08x", uint32(got), want)
		}
	}
	if InterfaceVersion34 != 0x34 || InterfaceVersion50 != 0x50 {
		t.Fatalf("interface versions: v3.4=0x%02x v5=0x%02x", byte(InterfaceVersion34), byte(InterfaceVersion50))
	}
}
