package protocol

import "testing"

func TestRegisteredDeliveryRoundTripsEveryOctet(t *testing.T) {
	for value := 0; value <= 0xff; value++ {
		got := DecodeRegisteredDelivery(uint8(value))
		if encoded := got.Encode(); encoded != uint8(value) {
			t.Fatalf("registered_delivery 0x%02x decoded as %+v and encoded as 0x%02x", value, got, encoded)
		}
	}
}

func TestRegisteredDeliveryFieldExtraction(t *testing.T) {
	got := DecodeRegisteredDelivery(0x3f)
	if got.SMSCReceipt != SMSCReceiptModeSuccessOnly {
		t.Fatalf("SMSC receipt mode = %d, want peer-specific success-only", got.SMSCReceipt)
	}
	if got.SMSCReceipt != SMSCReceiptModeReserved {
		t.Fatalf("reserved alias = %d, want same 0b11 wire value", got.SMSCReceipt)
	}
	if got.SMEAcknowledgement != SMEDeliveryAcknowledgementModeBoth {
		t.Fatalf("SME acknowledgement = %d, want both", got.SMEAcknowledgement)
	}
	if got.IntermediateNotification != IntermediateNotificationModeRequested {
		t.Fatalf("intermediate notification = %d, want requested", got.IntermediateNotification)
	}
	if got.ReservedBits != 0 {
		t.Fatalf("reserved bits = 0x%02x, want 0", got.ReservedBits)
	}

	got = DecodeRegisteredDelivery(0xd1)
	if got.SMSCReceipt != SMSCReceiptModeFinalOutcome ||
		got.SMEAcknowledgement != SMEDeliveryAcknowledgementModeNone ||
		got.IntermediateNotification != IntermediateNotificationModeRequested ||
		got.ReservedBits != 0xc0 {
		t.Fatalf("unexpected field decode: %+v", got)
	}

	got = DecodeRegisteredDelivery(0x20)
	if got.IntermediateNotification != IntermediateNotificationModeNone || got.ReservedBits != 0x20 {
		t.Fatalf("bit 5 must remain reserved, got %+v", got)
	}

	got = DecodeRegisteredDelivery(0x30)
	if got.IntermediateNotification != IntermediateNotificationModeRequested || got.ReservedBits != 0x20 {
		t.Fatalf("bit 4 must request notification while bit 5 remains reserved, got %+v", got)
	}
}
