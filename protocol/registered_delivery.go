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
	// SMSCReceiptModeSuccessOnly names the 0b11 encoding for peers that
	// explicitly define it as a success-only receipt request. SMPP 3.4 reserves
	// this encoding, so this interpretation is peer-specific and not portable.
	SMSCReceiptModeSuccessOnly
	// SMSCReceiptModeReserved is the SMPP 3.4 standards classification for
	// the same 0b11 wire value. It is retained as an alias for standards-based
	// inspection; the wire value is never changed by the decoder or encoder.
	SMSCReceiptModeReserved = SMSCReceiptModeSuccessOnly
)

// SMEDeliveryAcknowledgementMode is the two-bit SME acknowledgement request
// field in registered_delivery.
type SMEDeliveryAcknowledgementMode uint8

const (
	SMEDeliveryAcknowledgementModeNone SMEDeliveryAcknowledgementMode = iota
	SMEDeliveryAcknowledgementModeRequested
	SMEDeliveryAcknowledgementModeManualUserRequested
	// SMEDeliveryAcknowledgementModeBoth requests both delivery and
	// manual/user acknowledgements.
	SMEDeliveryAcknowledgementModeBoth
)

// IntermediateNotificationMode is the single-bit intermediate-notification
// request field in registered_delivery (bit 4).
type IntermediateNotificationMode uint8

const (
	IntermediateNotificationModeNone IntermediateNotificationMode = iota
	IntermediateNotificationModeRequested
)

// RegisteredDelivery is the lossless, field-level view of the SMPP
// registered_delivery octet. SMSCReceipt and SMEAcknowledgement are two-bit
// fields; IntermediateNotification represents bit 4; ReservedBits preserves
// bits 5-7 exactly.
//
// Existing PDU structs continue to expose RegisteredDelivery as uint8. Use
// DecodeRegisteredDelivery to inspect that octet and Encode to reconstruct it.
// No reserved or vendor-defined bit is rejected or normalized.
type RegisteredDelivery struct {
	SMSCReceipt               SMSCReceiptMode
	SMEAcknowledgement        SMEDeliveryAcknowledgementMode
	IntermediateNotification  IntermediateNotificationMode
	ReservedBits              uint8
}

// DecodeRegisteredDelivery splits a registered_delivery octet into its
// standard fields without rejecting or normalizing reserved values.
func DecodeRegisteredDelivery(value uint8) RegisteredDelivery {
	return RegisteredDelivery{
		SMSCReceipt:               SMSCReceiptMode(value & 0x03),
		SMEAcknowledgement:        SMEDeliveryAcknowledgementMode((value >> 2) & 0x03),
		IntermediateNotification:  IntermediateNotificationMode((value >> 4) & 0x01),
		ReservedBits:              value & 0xE0,
	}
}

// Encode reconstructs the registered_delivery octet, preserving every field
// value and all reserved bits.
func (r RegisteredDelivery) Encode() uint8 {
	return uint8(r.SMSCReceipt)&0x03 |
		(uint8(r.SMEAcknowledgement)&0x03)<<2 |
		(uint8(r.IntermediateNotification)&0x01)<<4 |
		(r.ReservedBits & 0xE0)
}
