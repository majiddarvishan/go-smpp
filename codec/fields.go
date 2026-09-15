package codec

import (
	"bytes"
	"encoding/binary"

	"github.com/majiddarvishan/go-smpp/protocol"
)

func ReadUint8(src []byte) (uint8, int, error) {
	if len(src) < 1 {
		return 0, 0, &protocol.FatalError{Kind: protocol.FatalInvalidFieldLength, Reason: "missing 1-octet integer"}
	}
	return src[0], 1, nil
}

func ReadUint16(src []byte) (uint16, int, error) {
	if len(src) < 2 {
		return 0, 0, &protocol.FatalError{Kind: protocol.FatalInvalidFieldLength, Reason: "missing 2-octet integer"}
	}
	return binary.BigEndian.Uint16(src[:2]), 2, nil
}

func ReadUint32(src []byte) (uint32, int, error) {
	if len(src) < 4 {
		return 0, 0, &protocol.FatalError{Kind: protocol.FatalInvalidFieldLength, Reason: "missing 4-octet integer"}
	}
	return binary.BigEndian.Uint32(src[:4]), 4, nil
}

func PutUint8(dst []byte, value uint8) error {
	if len(dst) < 1 {
		return ErrShortBuffer
	}
	dst[0] = value
	return nil
}

func PutUint16(dst []byte, value uint16) error {
	if len(dst) < 2 {
		return ErrShortBuffer
	}
	binary.BigEndian.PutUint16(dst[:2], value)
	return nil
}

func PutUint32(dst []byte, value uint32) error {
	if len(dst) < 4 {
		return ErrShortBuffer
	}
	binary.BigEndian.PutUint32(dst[:4], value)
	return nil
}

// ReadCString returns a borrowed view of a NUL-terminated C-Octet String.
// maxEncodedLen, when positive, includes the terminating NUL octet.
func ReadCString(src []byte, maxEncodedLen int) ([]byte, int, error) {
	limit := len(src)
	if maxEncodedLen > 0 && maxEncodedLen < limit {
		limit = maxEncodedLen
	}
	if limit == 0 {
		return nil, 0, &protocol.FatalError{Kind: protocol.FatalInvalidCString, Reason: "C-Octet String has no room for terminator"}
	}

	idx := bytes.IndexByte(src[:limit], 0)
	if idx < 0 {
		return nil, 0, &protocol.FatalError{Kind: protocol.FatalInvalidCString, Reason: "C-Octet String is not NUL-terminated within its allowed boundary"}
	}
	return src[:idx], idx + 1, nil
}

func AppendCString(dst []byte, value []byte, maxEncodedLen int) ([]byte, error) {
	if bytes.IndexByte(value, 0) >= 0 {
		return dst, ErrEmbeddedNUL
	}
	if maxEncodedLen > 0 && len(value)+1 > maxEncodedLen {
		return dst, ErrFieldTooLong
	}
	dst = append(dst, value...)
	dst = append(dst, 0)
	return dst, nil
}

// ReadOctets returns a borrowed view of exactly n octets.
func ReadOctets(src []byte, n int) ([]byte, int, error) {
	if n < 0 || n > len(src) {
		return nil, 0, &protocol.FatalError{Kind: protocol.FatalInvalidFieldLength, Reason: "Octet String length exceeds remaining PDU body"}
	}
	return src[:n], n, nil
}

func AppendOctets(dst, value []byte) []byte {
	return append(dst, value...)
}
