package protocol

// SMSCReceiptMode is the two-bit SMSC delivery-receipt request field in the
// SMPP registered_delivery octet.
type SMSCReceiptMode uint8

const (
	// SMSCReceiptModeNone requests no SMSC delivery receipt.
	SMSCReceiptModeNone SMSCReceiptMode = iota
	// SMSCReceiptModeFinalOutcome requests a receipt for the final success or
	// failure outcome.
	SMSCReceiptModeFinalOutcome
	// SMSCReceiptModeFailureOnly requests a receipt only for final failure.
	SMSCReceiptModeFailureOnly
	// SMSCReceiptModeReserved is the reserved two-bit value 0b11. It is exposed
	// so callers can inspect and preserve peer values without reinterpretation.
	SMSCReceiptModeReserved
)

// SMEDeliveryAcknowledgementMode is the two-bit SME acknowledgement request
// field in registered_delivery.
type SMEDeliveryAcknowledgementMode uint8

const (
	SMEDeliveryAcknowledgementModeNone SMEDeliveryAcknowledgementMode = iota
	SMEDeliveryAcknowledgementModeRequested
	SMEDeliveryAcknowledgementModeManualUserRequested
	SMEDeliveryAcknowledgementModeReserved
)

// IntermediateNotificationMode is the two-bit intermediate-notification
// request field in registered_delivery.
type IntermediateNotificationMode uint8

const (
	IntermediateNotificationModeNone IntermediateNotificationMode = iota
	IntermediateNotificationModeRequested
	IntermediateNotificationModeReserved2
	IntermediateNotificationModeReserved3
)

// RegisteredDelivery is the lossless, field-level view of the SMPP
// registered_delivery octet. The first three fields retain their complete
// two-bit values, including reserved encodings. ReservedBits retains bits 6-7.
//
// Existing PDU structs continue to expose RegisteredDelivery as uint8. Use
// DecodeRegisteredDelivery to inspect that octet and Encode to reconstruct it.
// No reserved or vendor-defined value is rejected or normalized.
type RegisteredDelivery struct {
	SMSCReceipt               SMSCReceiptMode
	SMEAcknowledgement        SMEDeliveryAcknowledgementMode
	IntermediateNotification IntermediateNotificationMode
	ReservedBits              uint8
}

// DecodeRegisteredDelivery splits a registered_delivery octet into its
// standard bit fields without rejecting or normalizing reserved values.
func DecodeRegisteredDelivery(value uint8) RegisteredDelivery {
	return RegisteredDelivery{
		SMSCReceipt:               SMSCReceiptMode(value & 0x03),
		SMEAcknowledgement:        SMEDeliveryAcknowledgementMode((value >> 2) & 0x03),
		IntermediateNotification: IntermediateNotificationMode((value >> 4) & 0x03),
		ReservedBits:              value & 0xC0,
	}
}

// Encode reconstructs the registered_delivery octet, preserving every
// two-bit field value and the reserved high bits.
func (r RegisteredDelivery) Encode() uint8 {
	return uint8(r.SMSCReceipt)&0x03 |
		(uint8(r.SMEAcknowledgement)&0x03)<<2 |
		(uint8(r.IntermediateNotification)&0x03)<<4 |
		(r.ReservedBits & 0xC0)
}
