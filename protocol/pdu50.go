package protocol

// CongestionState is the SMPP 5.0 congestion_state TLV value. Values 0..100
// are defined by the specification; larger values are reserved.
type CongestionState uint8

// Valid reports whether the congestion value is defined by SMPP 5.0.
func (c CongestionState) Valid() bool { return c <= 100 }

// BroadcastSM is the SMPP 5.0 broadcast_sm body. Parameters represented on
// the wire as TLVs, including the conditionally mandatory broadcast parameters,
// are preserved in Optional in their original order.
type BroadcastSM struct {
	ServiceType          []byte
	SourceAddrTON        TON
	SourceAddrNPI        NPI
	SourceAddr           []byte
	MessageID            []byte
	PriorityFlag         uint8
	ScheduleDeliveryTime []byte
	ValidityPeriod       []byte
	ReplaceIfPresentFlag uint8
	DataCoding           DataCoding
	SMDefaultMsgID       uint8
	Optional             []OptionalParameter
}

// BroadcastSMResp is the SMPP 5.0 broadcast_sm_resp body.
type BroadcastSMResp struct {
	MessageID []byte
	Optional  []OptionalParameter
}

// QueryBroadcastSM is the SMPP 5.0 query_broadcast_sm body.
type QueryBroadcastSM struct {
	MessageID     []byte
	SourceAddrTON TON
	SourceAddrNPI NPI
	SourceAddr    []byte
	Optional      []OptionalParameter
}

// QueryBroadcastSMResp is the SMPP 5.0 query_broadcast_sm_resp body. The
// message state and area details are TLVs and are therefore retained in
// Optional in wire order.
type QueryBroadcastSMResp struct {
	MessageID []byte
	Optional  []OptionalParameter
}

// CancelBroadcastSM is the SMPP 5.0 cancel_broadcast_sm body.
type CancelBroadcastSM struct {
	ServiceType   []byte
	MessageID     []byte
	SourceAddrTON TON
	SourceAddrNPI NPI
	SourceAddr    []byte
	Optional      []OptionalParameter
}
