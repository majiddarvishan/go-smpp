package codec

import (
	"encoding/binary"

	"github.com/majiddarvishan/go-smpp/protocol"
)

const HeaderSize = 16

// Header is the fixed 16-octet SMPP PDU header.
type Header struct {
	CommandLength  uint32
	CommandID      protocol.CommandID
	CommandStatus  protocol.CommandStatus
	SequenceNumber protocol.SequenceNumber
}

// DecodeHeader decodes the fixed SMPP header. It intentionally accepts the
// full uint32 sequence-number wire range; receive-side interoperability policy
// is handled by protocol.SequenceNumber.ValidInbound.
func DecodeHeader(src []byte) (Header, error) {
	if len(src) < HeaderSize {
		return Header{}, &protocol.FatalError{Kind: protocol.FatalTruncatedFrame, Reason: "PDU header shorter than 16 octets"}
	}

	h := Header{
		CommandLength:  binary.BigEndian.Uint32(src[0:4]),
		CommandID:      protocol.CommandID(binary.BigEndian.Uint32(src[4:8])),
		CommandStatus:  protocol.CommandStatus(binary.BigEndian.Uint32(src[8:12])),
		SequenceNumber: protocol.SequenceNumber(binary.BigEndian.Uint32(src[12:16])),
	}
	if h.CommandLength < HeaderSize {
		return Header{}, &protocol.FatalError{
			Kind:           protocol.FatalInvalidCommandLength,
			DeclaredLength: h.CommandLength,
			Command:        h.CommandID,
			Sequence:       h.SequenceNumber,
			Reason:         "command_length is smaller than the SMPP header",
		}
	}
	return h, nil
}

// EncodeHeader encodes the fixed SMPP header in network byte order.
func EncodeHeader(dst []byte, h Header) error {
	if len(dst) < HeaderSize {
		return ErrShortBuffer
	}
	if h.CommandLength < HeaderSize {
		return ErrInvalidCommandLength
	}
	binary.BigEndian.PutUint32(dst[0:4], h.CommandLength)
	binary.BigEndian.PutUint32(dst[4:8], uint32(h.CommandID))
	binary.BigEndian.PutUint32(dst[8:12], uint32(h.CommandStatus))
	binary.BigEndian.PutUint32(dst[12:16], uint32(h.SequenceNumber))
	return nil
}
