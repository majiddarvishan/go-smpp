package protocol

// SequenceNumber correlates an SMPP response PDU with its request PDU.
type SequenceNumber uint32

const (
	SequenceMin         SequenceNumber = 0x00000001
	SequenceOutboundMax SequenceNumber = 0x7FFFFFFF
	SequenceInboundMax  SequenceNumber = 0xFFFFFFFF
)

// Valid reports whether the value is accepted on the wire for an inbound PDU.
// For interoperability, inbound values up to 0xFFFFFFFF are accepted even
// though SMPP 3.4 defines 0x7FFFFFFF as the upper end of the sequence range.
func (s SequenceNumber) Valid() bool { return s.ValidInbound() }

// ValidInbound reports whether a received sequence number is accepted.
func (s SequenceNumber) ValidInbound() bool {
	return s >= SequenceMin && s <= SequenceInboundMax
}

// ValidOutbound reports whether the value is suitable for locally generated
// requests. Locally generated sequence numbers remain within the SMPP-defined
// 1..0x7FFFFFFF range.
func (s SequenceNumber) ValidOutbound() bool {
	return s >= SequenceMin && s <= SequenceOutboundMax
}
