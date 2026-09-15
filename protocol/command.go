package protocol

// CommandID identifies an SMPP PDU operation.
type CommandID uint32

// SMPP 3.4 command identifiers.
const (
	CommandGenericNACK         CommandID = 0x80000000
	CommandBindReceiver        CommandID = 0x00000001
	CommandBindReceiverResp    CommandID = 0x80000001
	CommandBindTransmitter     CommandID = 0x00000002
	CommandBindTransmitterResp CommandID = 0x80000002
	CommandQuerySM             CommandID = 0x00000003
	CommandQuerySMResp         CommandID = 0x80000003
	CommandSubmitSM            CommandID = 0x00000004
	CommandSubmitSMResp        CommandID = 0x80000004
	CommandDeliverSM           CommandID = 0x00000005
	CommandDeliverSMResp       CommandID = 0x80000005
	CommandUnbind              CommandID = 0x00000006
	CommandUnbindResp          CommandID = 0x80000006
	CommandReplaceSM           CommandID = 0x00000007
	CommandReplaceSMResp       CommandID = 0x80000007
	CommandCancelSM            CommandID = 0x00000008
	CommandCancelSMResp        CommandID = 0x80000008
	CommandBindTransceiver     CommandID = 0x00000009
	CommandBindTransceiverResp CommandID = 0x80000009
	CommandOutbind             CommandID = 0x0000000B
	CommandEnquireLink         CommandID = 0x00000015
	CommandEnquireLinkResp     CommandID = 0x80000015
	CommandSubmitMulti         CommandID = 0x00000021
	CommandSubmitMultiResp     CommandID = 0x80000021
	CommandAlertNotification   CommandID = 0x00000102
	CommandDataSM              CommandID = 0x00000103
	CommandDataSMResp          CommandID = 0x80000103
)

const responseMask CommandID = 0x80000000

// IsResponse reports whether bit 31, the SMPP response bit, is set.
func (c CommandID) IsResponse() bool {
	return c&responseMask != 0
}

// RequestID returns the request-form command identifier by clearing bit 31.
// It is a wire-level helper and does not imply that the resulting identifier is
// a defined SMPP request command.
func (c CommandID) RequestID() CommandID {
	return c &^ responseMask
}

// ResponseID returns the response-form command identifier by setting bit 31.
// It is a wire-level helper and does not imply that the resulting identifier is
// a defined SMPP response command.
func (c CommandID) ResponseID() CommandID {
	return c | responseMask
}
