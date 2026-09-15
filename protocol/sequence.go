package protocol

// SequenceNumber correlates an SMPP response PDU with its request PDU.
type SequenceNumber uint32

const (
	SequenceMin SequenceNumber = 0x00000001
	SequenceMax SequenceNumber = 0x7FFFFFFF
)

// Valid reports whether the value is in the SMPP-defined sequence range.
func (s SequenceNumber) Valid() bool {
	return s >= SequenceMin && s <= SequenceMax
}
