package protocol

// OptionalParameter preserves one SMPP TLV exactly as received. Value may be a
// borrowed view of the decoded PDU frame; callers that retain it beyond the
// decode lifetime must copy it.
type OptionalParameter struct {
	Tag   uint16
	Value []byte
}

const (
	TLVTagDestAddrSubunit          uint16 = 0x0005
	TLVTagDestNetworkType          uint16 = 0x0006
	TLVTagDestBearerType           uint16 = 0x0007
	TLVTagDestTelematicsID         uint16 = 0x0008
	TLVTagSourceAddrSubunit        uint16 = 0x000D
	TLVTagSourceNetworkType        uint16 = 0x000E
	TLVTagSourceBearerType         uint16 = 0x000F
	TLVTagSourceTelematicsID       uint16 = 0x0010
	TLVTagQOSTimeToLive            uint16 = 0x0017
	TLVTagPayloadType              uint16 = 0x0019
	TLVTagAdditionalStatusInfoText uint16 = 0x001D
	TLVTagReceiptedMessageID       uint16 = 0x001E
	TLVTagMSMsgWaitFacilities      uint16 = 0x0030
	TLVTagPrivacyIndicator         uint16 = 0x0201
	TLVTagSourceSubaddress         uint16 = 0x0202
	TLVTagDestSubaddress           uint16 = 0x0203
	TLVTagUserMessageReference     uint16 = 0x0204
	TLVTagUserResponseCode         uint16 = 0x0205
	TLVTagSourcePort               uint16 = 0x020A
	TLVTagDestinationPort          uint16 = 0x020B
	TLVTagSARMsgRefNum             uint16 = 0x020C
	TLVTagLanguageIndicator        uint16 = 0x020D
	TLVTagSARTotalSegments         uint16 = 0x020E
	TLVTagSARSegmentSeqnum         uint16 = 0x020F
	TLVTagSCInterfaceVersion       uint16 = 0x0210
	TLVTagCallbackNumPresInd       uint16 = 0x0302
	TLVTagCallbackNumAtag          uint16 = 0x0303
	TLVTagNumberOfMessages         uint16 = 0x0304
	TLVTagCallbackNum              uint16 = 0x0381
	TLVTagDPFResult                uint16 = 0x0420
	TLVTagSetDPF                   uint16 = 0x0421
	TLVTagMSAvailabilityStatus     uint16 = 0x0422
	TLVTagNetworkErrorCode         uint16 = 0x0423
	TLVTagMessagePayload           uint16 = 0x0424
	TLVTagDeliveryFailureReason    uint16 = 0x0425
	TLVTagMoreMessagesToSend       uint16 = 0x0426
	TLVTagMessageState             uint16 = 0x0427
	TLVTagUSSDServiceOp            uint16 = 0x0501
	TLVTagDisplayTime              uint16 = 0x1201
	TLVTagSMSSignal                uint16 = 0x1203
	TLVTagMSValidity               uint16 = 0x1204
	TLVTagAlertOnMessageDelivery   uint16 = 0x130C
	TLVTagITSReplyType             uint16 = 0x1380
	TLVTagITSSessionInfo           uint16 = 0x1383
)

// BindRequest is the common body used by bind_transmitter, bind_receiver and
// bind_transceiver in SMPP 3.4.
type BindRequest struct {
	SystemID         []byte
	Password         []byte
	SystemType       []byte
	InterfaceVersion InterfaceVersion
	AddressTON       TON
	AddressNPI       NPI
	AddressRange     []byte
}

// BindResponse is the common body used by the three bind response PDUs.
type BindResponse struct {
	SystemID []byte
	Optional []OptionalParameter
}

// EmptyBody represents an SMPP PDU whose body is empty.
type EmptyBody struct{}

// SubmitSM is the SMPP 3.4 submit_sm mandatory body plus ordered optional TLVs.
type SubmitSM struct {
	ServiceType          []byte
	SourceAddrTON        TON
	SourceAddrNPI        NPI
	SourceAddr           []byte
	DestAddrTON          TON
	DestAddrNPI          NPI
	DestinationAddr      []byte
	ESMClass             uint8
	ProtocolID           uint8
	PriorityFlag         uint8
	ScheduleDeliveryTime []byte
	ValidityPeriod       []byte
	RegisteredDelivery   uint8
	ReplaceIfPresentFlag uint8
	DataCoding           DataCoding
	SMDefaultMsgID       uint8
	ShortMessage         []byte
	Optional             []OptionalParameter
}

// SubmitSMResp is the SMPP 3.4 submit_sm_resp body. The body is absent when
// command_status is non-zero.
type SubmitSMResp struct {
	MessageID []byte
}

// DeliverSM is the SMPP 3.4 deliver_sm mandatory body plus ordered optional
// TLVs. The wire layout intentionally mirrors submit_sm while preserving a
// distinct public type because several fields have different semantics.
type DeliverSM struct {
	ServiceType          []byte
	SourceAddrTON        TON
	SourceAddrNPI        NPI
	SourceAddr           []byte
	DestAddrTON          TON
	DestAddrNPI          NPI
	DestinationAddr      []byte
	ESMClass             uint8
	ProtocolID           uint8
	PriorityFlag         uint8
	ScheduleDeliveryTime []byte
	ValidityPeriod       []byte
	RegisteredDelivery   uint8
	ReplaceIfPresentFlag uint8
	DataCoding           DataCoding
	SMDefaultMsgID       uint8
	ShortMessage         []byte
	Optional             []OptionalParameter
}

// DeliverSMResp is the SMPP 3.4 deliver_sm_resp body. message_id is specified
// as unused/NULL in SMPP 3.4; it is retained here for interoperability with
// peers that send a C-Octet String body anyway.
type DeliverSMResp struct {
	MessageID []byte
}
