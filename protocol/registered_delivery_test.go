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
	if got.SMSCReceipt != SMSCReceiptModeReserved {
		t.Fatalf("SMSC receipt mode = %d, want reserved", got.SMSCReceipt)
	}
	if got.SMEAcknowledgement != SMEDeliveryAcknowledgementModeReserved {
		t.Fatalf("SME acknowledgement = %d, want reserved", got.SMEAcknowledgement)
	}
	if got.IntermediateNotification != IntermediateNotificationModeReserved3 {
		t.Fatalf("intermediate notification = %d, want reserved3", got.IntermediateNotification)
	}
	if got.ReservedBits != 0 {
		t.Fatalf("reserved high bits = 0x%02x, want 0", got.ReservedBits)
	}

	got = DecodeRegisteredDelivery(0xd1)
	if got.SMSCReceipt != SMSCReceiptModeFinalOutcome ||
		got.SMEAcknowledgement != SMEDeliveryAcknowledgementModeNone ||
		got.IntermediateNotification != IntermediateNotificationModeReserved3 ||
		got.ReservedBits != 0xc0 {
		t.Fatalf("unexpected field decode: %+v", got)
	}
}
