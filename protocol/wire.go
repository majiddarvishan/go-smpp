package protocol

import "encoding/binary"

const Uint32Size = 4

// PutUint32 writes an SMPP 4-octet integer in network byte order.
func PutUint32(dst []byte, value uint32) bool {
	if len(dst) < Uint32Size {
		return false
	}
	binary.BigEndian.PutUint32(dst[:Uint32Size], value)
	return true
}

// ReadUint32 reads an SMPP 4-octet integer in network byte order.
func ReadUint32(src []byte) (uint32, bool) {
	if len(src) < Uint32Size {
		return 0, false
	}
	return binary.BigEndian.Uint32(src[:Uint32Size]), true
}
