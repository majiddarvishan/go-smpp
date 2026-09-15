package codec

import (
	"encoding/binary"
	"math"

	"github.com/majiddarvishan/go-smpp/protocol"
)

const TLVHeaderSize = 4

// TLV preserves tag order and permits duplicate tags. Value is a borrowed view
// when produced by ScanTLVs or ParseTLVs.
type TLV struct {
	Tag   uint16
	Value []byte
}

// ScanTLVs walks an optional-parameter area without allocating. Returning false
// from visit stops scanning successfully.
func ScanTLVs(src []byte, visit func(TLV) bool) error {
	for offset := 0; offset < len(src); {
		remaining := len(src) - offset
		if remaining < TLVHeaderSize {
			return &protocol.FatalError{Kind: protocol.FatalInvalidTLVLength, Reason: "truncated TLV header"}
		}

		tag := binary.BigEndian.Uint16(src[offset : offset+2])
		length := int(binary.BigEndian.Uint16(src[offset+2 : offset+4]))
		if length > remaining-TLVHeaderSize {
			return &protocol.FatalError{Kind: protocol.FatalInvalidTLVLength, Reason: "TLV value length exceeds remaining PDU body"}
		}

		value := src[offset+TLVHeaderSize : offset+TLVHeaderSize+length]
		if visit != nil && !visit(TLV{Tag: tag, Value: value}) {
			return nil
		}
		offset += TLVHeaderSize + length
	}
	return nil
}

// ParseTLVs returns ordered TLVs while keeping values borrowed from src.
func ParseTLVs(src []byte) ([]TLV, error) {
	capacity := len(src) / TLVHeaderSize
	if capacity > 8 {
		capacity = 8
	}
	out := make([]TLV, 0, capacity)
	err := ScanTLVs(src, func(tlv TLV) bool {
		out = append(out, tlv)
		return true
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func AppendTLV(dst []byte, tag uint16, value []byte) ([]byte, error) {
	if len(value) > math.MaxUint16 {
		return dst, ErrTLVValueTooLong
	}
	start := len(dst)
	dst = append(dst, 0, 0, 0, 0)
	binary.BigEndian.PutUint16(dst[start:start+2], tag)
	binary.BigEndian.PutUint16(dst[start+2:start+4], uint16(len(value)))
	dst = append(dst, value...)
	return dst, nil
}
