package protocol

// MessageState is the SMPP 3.4 query_sm_resp message_state value.
type MessageState uint8

const (
	MessageStateEnroute       MessageState = 1
	MessageStateDelivered     MessageState = 2
	MessageStateExpired       MessageState = 3
	MessageStateDeleted       MessageState = 4
	MessageStateUndeliverable MessageState = 5
	MessageStateAccepted      MessageState = 6
	MessageStateUnknown       MessageState = 7
	MessageStateRejected      MessageState = 8
)

// DestinationFlag identifies the submit_multi destination representation.
type DestinationFlag uint8

const (
	DestinationSME              DestinationFlag = 1
	DestinationDistributionList DestinationFlag = 2
)

// SubmitMultiDestination is one submit_multi destination. For DestinationSME,
// TON/NPI and Address are encoded. For DestinationDistributionList, Address is
// the distribution-list name and TON/NPI are ignored.
type SubmitMultiDestination struct {
	Flag    DestinationFlag
	TON     TON
	NPI     NPI
	Address []byte
}

// UnsuccessfulSME is one submit_multi_resp unsuccessful destination entry.
type UnsuccessfulSME struct {
	DestAddrTON     TON
	DestAddrNPI     NPI
	DestinationAddr []byte
	ErrorStatusCode CommandStatus
}

// SubmitMulti is the SMPP 3.4 submit_multi mandatory body plus ordered TLVs.
type SubmitMulti struct {
	ServiceType          []byte
	SourceAddrTON        TON
	SourceAddrNPI        NPI
	SourceAddr           []byte
	Destinations         []SubmitMultiDestination
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

// SubmitMultiResp is the SMPP 3.4 submit_multi_resp body.
type SubmitMultiResp struct {
	MessageID    []byte
	Unsuccessful []UnsuccessfulSME
}

// DataSM is the SMPP 3.4 data_sm mandatory body plus ordered TLVs.
type DataSM struct {
	ServiceType        []byte
	SourceAddrTON      TON
	SourceAddrNPI      NPI
	SourceAddr         []byte
	DestAddrTON        TON
	DestAddrNPI        NPI
	DestinationAddr    []byte
	ESMClass           uint8
	RegisteredDelivery uint8
	DataCoding         DataCoding
	Optional           []OptionalParameter
}

// DataSMResp is the SMPP 3.4 data_sm_resp body plus ordered TLVs.
type DataSMResp struct {
	MessageID []byte
	Optional  []OptionalParameter
}

// QuerySM is the SMPP 3.4 query_sm body.
type QuerySM struct {
	MessageID     []byte
	SourceAddrTON TON
	SourceAddrNPI NPI
	SourceAddr    []byte
}

// QuerySMResp is the SMPP 3.4 query_sm_resp body.
type QuerySMResp struct {
	MessageID    []byte
	FinalDate    []byte
	MessageState MessageState
	ErrorCode    uint8
}

// CancelSM is the SMPP 3.4 cancel_sm body.
type CancelSM struct {
	ServiceType     []byte
	MessageID       []byte
	SourceAddrTON   TON
	SourceAddrNPI   NPI
	SourceAddr      []byte
	DestAddrTON     TON
	DestAddrNPI     NPI
	DestinationAddr []byte
}

// ReplaceSM is the SMPP 3.4 replace_sm body.
type ReplaceSM struct {
	MessageID            []byte
	SourceAddrTON        TON
	SourceAddrNPI        NPI
	SourceAddr           []byte
	ScheduleDeliveryTime []byte
	ValidityPeriod       []byte
	RegisteredDelivery   uint8
	SMDefaultMsgID       uint8
	ShortMessage         []byte
}

// AlertNotification is the one-way SMPP 3.4 alert_notification body.
type AlertNotification struct {
	SourceAddrTON TON
	SourceAddrNPI NPI
	SourceAddr    []byte
	ESMEAddrTON   TON
	ESMEAddrNPI   NPI
	ESMEAddr      []byte
	Optional      []OptionalParameter
}

// Outbind is the one-way SMPP 3.4 outbind body.
type Outbind struct {
	SystemID []byte
	Password []byte
}
