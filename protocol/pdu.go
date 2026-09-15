package protocol

// OptionalParameter preserves one SMPP TLV exactly as received. Value may be a
// borrowed view of the decoded PDU frame; callers that retain it beyond the
// decode lifetime must copy it.
type OptionalParameter struct {
	Tag   uint16
	Value []byte
}

const (
	TLVTagSARMsgRefNum       uint16 = 0x020C
	TLVTagSARTotalSegments   uint16 = 0x020E
	TLVTagSARSegmentSeqnum   uint16 = 0x020F
	TLVTagSCInterfaceVersion uint16 = 0x0210
	TLVTagMessagePayload     uint16 = 0x0424
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
