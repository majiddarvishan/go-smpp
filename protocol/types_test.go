package protocol

import "testing"

func TestCommandIDs(t *testing.T) {
	tests := map[string]struct {
		got  CommandID
		want CommandID
	}{
		"bind_receiver":      {CommandBindReceiver, 0x00000001},
		"bind_transmitter":   {CommandBindTransmitter, 0x00000002},
		"submit_sm":          {CommandSubmitSM, 0x00000004},
		"deliver_sm":         {CommandDeliverSM, 0x00000005},
		"bind_transceiver":   {CommandBindTransceiver, 0x00000009},
		"enquire_link":       {CommandEnquireLink, 0x00000015},
		"submit_multi":       {CommandSubmitMulti, 0x00000021},
		"alert_notification": {CommandAlertNotification, 0x00000102},
		"data_sm":            {CommandDataSM, 0x00000103},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("got 0x%08x, want 0x%08x", tt.got, tt.want)
			}
			if tt.got.ResponseID().RequestID() != tt.got {
				t.Fatalf("response/request transform did not round-trip for 0x%08x", tt.got)
			}
		})
	}
}

func TestResponseIDs(t *testing.T) {
	if !CommandSubmitSMResp.IsResponse() {
		t.Fatal("submit_sm_resp must have response bit set")
	}
	if CommandSubmitSM.IsResponse() {
		t.Fatal("submit_sm must not have response bit set")
	}
	if CommandSubmitSM.ResponseID() != CommandSubmitSMResp {
		t.Fatalf("got 0x%08x, want 0x%08x", CommandSubmitSM.ResponseID(), CommandSubmitSMResp)
	}
}

func TestSequenceRange(t *testing.T) {
	if SequenceNumber(0).Valid() {
		t.Fatal("zero sequence must be invalid")
	}
	if !SequenceMin.Valid() || !SequenceMax.Valid() {
		t.Fatal("sequence range endpoints must be valid")
	}
	if SequenceNumber(uint32(SequenceMax) + 1).Valid() {
		t.Fatal("sequence above maximum must be invalid")
	}
}

func TestProtocolProfiles(t *testing.T) {
	if !Profile34.Valid() || Profile34.InterfaceVersion != 0x34 {
		t.Fatal("SMPP 3.4 profile is invalid")
	}
	if !Profile50.Valid() || Profile50.InterfaceVersion != 0x50 || !Profile50.IsSMPP50() {
		t.Fatal("SMPP 5.0 profile is invalid")
	}
	if !Profile34.SupportsOptionalParameters() || !Profile50.SupportsOptionalParameters() {
		t.Fatal("SMPP 3.4 and 5.0 profiles must support optional TLV parameters")
	}
	if (Profile{InterfaceVersion: 0x40}).Valid() {
		t.Fatal("reserved interface version must not be accepted as a known profile")
	}
}

func TestTONAndNPIConstants(t *testing.T) {
	if TONInternational != 0x01 || TONAlphanumeric != 0x05 {
		t.Fatal("unexpected TON values")
	}
	if NPIISDN != 0x01 || NPIInternet != 0x0E || NPIWAPClientID != 0x12 {
		t.Fatal("unexpected NPI values")
	}
}

func TestDataCodingConstants(t *testing.T) {
	if DataCodingSMSCDefault != 0x00 || DataCodingUCS2 != 0x08 || DataCodingKSC5601 != 0x0E {
		t.Fatal("unexpected data_coding values")
	}
}

func TestSessionStateStrings(t *testing.T) {
	if StateBoundTRX.String() != "Bound_TRX" || StateClosed.String() != "Closed" {
		t.Fatal("unexpected session-state string")
	}
}

func TestUint32WireHelpers(t *testing.T) {
	buf := make([]byte, 4)
	if !PutUint32(buf, 0x01020304) {
		t.Fatal("PutUint32 unexpectedly failed")
	}
	if want := []byte{0x01, 0x02, 0x03, 0x04}; string(buf) != string(want) {
		t.Fatalf("got %x, want %x", buf, want)
	}
	got, ok := ReadUint32(buf)
	if !ok || got != 0x01020304 {
		t.Fatalf("got 0x%08x ok=%v", got, ok)
	}
	if PutUint32(buf[:3], 1) {
		t.Fatal("short destination must fail")
	}
	if _, ok := ReadUint32(buf[:3]); ok {
		t.Fatal("short source must fail")
	}
}

func TestStatusOK(t *testing.T) {
	if !StatusOK.OK() || StatusThrottled.OK() {
		t.Fatal("unexpected command status success classification")
	}
}

func TestFatalErrorClassification(t *testing.T) {
	err := &FatalError{Kind: FatalInvalidCommandLength, DeclaredLength: 7}
	if !err.Fatal() {
		t.Fatal("fatal protocol error must be classified fatal")
	}
	if err.Kind.String() != "invalid_command_length" {
		t.Fatalf("unexpected fatal kind: %s", err.Kind)
	}
}
