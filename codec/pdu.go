package codec

import (
	"errors"
	"fmt"

	"github.com/majiddarvishan/go-smpp/protocol"
)

var (
	ErrInvalidPDUValue          = errors.New("smpp codec: invalid PDU value")
	ErrPDUFrameLengthMismatch  = errors.New("smpp codec: PDU frame length does not match command_length")
	ErrConflictingMessageData  = errors.New("smpp codec: short_message and message_payload must not both carry data")
)

// RawBody is a borrowed view of an unresolved, structurally valid PDU body.
type RawBody []byte

// DecodedPDU contains the decoded SMPP header and a command-specific body. In
// compatible registry mode, an unknown command is returned as RawBody.
type DecodedPDU struct {
	Header Header
	Body   any
}

// DecodePDU decodes exactly one complete PDU frame. TCP coalescing/fragmentation
// belongs to Framer; passing bytes beyond command_length here is an API misuse
// and is rejected rather than guessed/resynchronized.
func DecodePDU(frame []byte, registry *Registry) (DecodedPDU, error) {
	header, err := DecodeHeader(frame)
	if err != nil {
		return DecodedPDU{}, err
	}
	declared := int(header.CommandLength)
	if len(frame) < declared {
		return DecodedPDU{}, &protocol.FatalError{
			Kind:           protocol.FatalTruncatedFrame,
			DeclaredLength: header.CommandLength,
			Command:        header.CommandID,
			Sequence:       header.SequenceNumber,
			Reason:         "frame ended before command_length",
		}
	}
	if len(frame) != declared {
		return DecodedPDU{}, fmt.Errorf("%w: declared=%d actual=%d", ErrPDUFrameLengthMismatch, declared, len(frame))
	}

	bodyBytes := frame[HeaderSize:declared]
	def, known, err := registry.ResolveCommand(header.CommandID)
	if err != nil {
		return DecodedPDU{}, err
	}
	if !known || def.Decode == nil {
		return DecodedPDU{Header: header, Body: RawBody(bodyBytes)}, nil
	}
	body, err := def.Decode(header, bodyBytes)
	if err != nil {
		return DecodedPDU{}, err
	}
	return DecodedPDU{Header: header, Body: body}, nil
}

// EncodePDU appends one complete PDU using the command encoder registered for
// header.CommandID. CommandLength is recomputed from the encoded body.
func EncodePDU(dst []byte, header Header, body any, registry *Registry) ([]byte, error) {
	def, known, err := registry.ResolveCommand(header.CommandID)
	if err != nil {
		return dst, err
	}
	if !known || def.Encode == nil {
		raw, ok := body.(RawBody)
		if !ok {
			return dst, fmt.Errorf("%w: no encoder for command 0x%08x", ErrInvalidPDUValue, uint32(header.CommandID))
		}
		start := len(dst)
		dst = append(dst, make([]byte, HeaderSize)...)
		dst = append(dst, raw...)
		header.CommandLength = uint32(len(dst) - start)
		if err := EncodeHeader(dst[start:start+HeaderSize], header); err != nil {
			return dst[:start], err
		}
		return dst, nil
	}

	start := len(dst)
	dst = append(dst, make([]byte, HeaderSize)...)
	dst, err = def.Encode(dst, body)
	if err != nil {
		return dst[:start], err
	}
	header.CommandLength = uint32(len(dst) - start)
	if err := EncodeHeader(dst[start:start+HeaderSize], header); err != nil {
		return dst[:start], err
	}
	return dst, nil
}

// ResponseHeader constructs a response header while preserving the request's
// sequence number exactly, including receive-side interoperability values above
// 0x7fffffff.
func ResponseHeader(request Header, responseID protocol.CommandID, status protocol.CommandStatus) Header {
	return Header{
		CommandID:      responseID,
		CommandStatus:  status,
		SequenceNumber: request.SequenceNumber,
	}
}
