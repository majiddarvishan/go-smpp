package codec

import (
	"encoding/binary"
	"errors"

	"github.com/majiddarvishan/go-smpp/protocol"
)

// DefaultMaxPDUSize is deliberately larger than the standard message-payload
// sizes while still providing a defensive memory bound. It is configurable per
// framer/session.
const DefaultMaxPDUSize uint32 = 1 << 20 // 1 MiB

// Framer extracts complete SMPP PDUs from a TCP byte stream. Complete PDUs
// already present in an input slice are emitted without copying. Fragmented
// PDUs are buffered. Emitted slices are borrowed and must not be retained after
// the callback returns unless copied by the caller.
type Framer struct {
	maxPDUSize uint32
	pending    []byte
	expected   uint32
	failed     error
}

func NewFramer(maxPDUSize uint32) (*Framer, error) {
	if maxPDUSize == 0 {
		maxPDUSize = DefaultMaxPDUSize
	}
	if maxPDUSize < HeaderSize {
		return nil, ErrInvalidMaxPDUSize
	}
	return &Framer{maxPDUSize: maxPDUSize}, nil
}

func (f *Framer) MaxPDUSize() uint32 { return f.maxPDUSize }
func (f *Framer) Buffered() int      { return len(f.pending) }
func (f *Framer) Failed() bool       { return f.failed != nil }

// Feed consumes bytes in TCP stream order and emits zero or more complete PDUs.
// Once a fatal framing/decode error occurs the framer is permanently failed for
// that connection and rejects all later bytes; callers must close the owning
// connection rather than trying to resynchronize.
func (f *Framer) Feed(data []byte, emit func([]byte) error) error {
	if f.failed != nil {
		return f.failed
	}

	for len(data) > 0 {
		if len(f.pending) > 0 {
			if f.expected == 0 {
				need := 4 - len(f.pending)
				if need > len(data) {
					need = len(data)
				}
				f.pending = append(f.pending, data[:need]...)
				data = data[need:]
				if len(f.pending) < 4 {
					return nil
				}

				length := binary.BigEndian.Uint32(f.pending[:4])
				if err := f.validateLength(length, f.pending); err != nil {
					return err
				}
				f.expected = length
				if cap(f.pending) < int(length) {
					buf := make([]byte, len(f.pending), int(length))
					copy(buf, f.pending)
					f.pending = buf
				}
			}

			need := int(f.expected) - len(f.pending)
			if need > 0 {
				take := need
				if take > len(data) {
					take = len(data)
				}
				f.pending = append(f.pending, data[:take]...)
				data = data[take:]
			}
			if len(f.pending) < int(f.expected) {
				return nil
			}

			frame := f.pending[:int(f.expected)]
			if err := f.emit(frame, emit); err != nil {
				return err
			}
			f.pending = f.pending[:0]
			f.expected = 0
			continue
		}

		if len(data) < 4 {
			f.pending = append(f.pending[:0], data...)
			return nil
		}

		length := binary.BigEndian.Uint32(data[:4])
		if err := f.validateLength(length, data); err != nil {
			return err
		}
		if len(data) < int(length) {
			f.expected = length
			if cap(f.pending) < int(length) {
				f.pending = make([]byte, 0, int(length))
			} else {
				f.pending = f.pending[:0]
			}
			f.pending = append(f.pending, data...)
			return nil
		}

		frame := data[:int(length)]
		if err := f.emit(frame, emit); err != nil {
			return err
		}
		data = data[int(length):]
	}
	return nil
}

// Finalize must be called when the TCP stream ends. A partially buffered PDU
// is a fatal truncated-frame condition for the connection.
func (f *Framer) Finalize() error {
	if f.failed != nil {
		return f.failed
	}
	if len(f.pending) == 0 {
		return nil
	}
	var declared uint32
	if len(f.pending) >= 4 {
		declared = binary.BigEndian.Uint32(f.pending[:4])
	}
	return f.fail(protocol.FatalTruncatedFrame, declared, f.pending, "TCP stream ended with a partial SMPP PDU")
}

func (f *Framer) validateLength(length uint32, prefix []byte) error {
	if length < HeaderSize {
		return f.fail(protocol.FatalInvalidCommandLength, length, prefix, "command_length is smaller than the 16-octet SMPP header")
	}
	if length > f.maxPDUSize {
		return f.fail(protocol.FatalPDUTooLarge, length, prefix, "command_length exceeds configured maximum PDU size")
	}
	return nil
}

func (f *Framer) emit(frame []byte, emit func([]byte) error) error {
	if emit == nil {
		return nil
	}
	err := emit(frame)
	if err == nil {
		return nil
	}

	var fatal *protocol.FatalError
	if errors.As(err, &fatal) {
		// A structural body/TLV decoder can discover corruption after framing.
		// Poison the framer as well so following bytes are never interpreted.
		f.failed = err
		f.pending = nil
		f.expected = 0
	}
	return err
}

func (f *Framer) fail(kind protocol.FatalErrorKind, declared uint32, prefix []byte, reason string) error {
	err := &protocol.FatalError{Kind: kind, DeclaredLength: declared, Reason: reason}
	if len(prefix) >= 8 {
		err.Command = protocol.CommandID(binary.BigEndian.Uint32(prefix[4:8]))
	}
	if len(prefix) >= HeaderSize {
		err.Sequence = protocol.SequenceNumber(binary.BigEndian.Uint32(prefix[12:16]))
	}
	f.failed = err
	f.pending = nil
	f.expected = 0
	return err
}
