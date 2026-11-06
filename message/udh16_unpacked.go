package message

import (
	"fmt"

	smppenc "github.com/majiddarvishan/go-smpp/encoding"
	"github.com/majiddarvishan/go-smpp/protocol"
)

const (
	gsm7Multipart16Septets = 152
	ucs2Multipart16Units   = 66
	binaryMultipart16Bytes = 133
)

// SegmentTextUDH16Unpacked encodes text into one or more SMPP short_message
// payloads using a 16-bit concatenation UDH for multipart messages.
//
// GSM 03.38 text is represented as UNPACKED septet octets, including ESC+code
// for extension characters. This is a specific SMPP peer dialect: it differs
// from SegmentTextUDH, which uses an 8-bit UDH reference and GSM7 septet
// packing. Callers must select the representation their SMSC expects.
//
// A single part contains no UDH. Multipart limits are 152 GSM7 septets or
// 66 UCS2 UTF-16 code units per part; GSM7 escape pairs and UTF-16 surrogate
// pairs are never split across part boundaries. The function never silently
// truncates text or generates more than 255 parts.
func SegmentTextUDH16Unpacked(text string, allowSurrogates bool, reference uint16) ([]Segment, error) {
	encoded, err := EncodeText(text, allowSurrogates)
	if err != nil {
		return nil, err
	}
	var limit int
	var chunks [][]byte
	switch encoded.Kind {
	case smppenc.KindGSM7:
		limit = GSM7SingleSeptets
		if encoded.Units > limit {
			chunks = splitGSM7(encoded.Data, gsm7Multipart16Septets)
		}
	case smppenc.KindUCS2:
		limit = UCS2SingleUnits
		if encoded.Units > limit {
			chunks = splitFixedUnits(encoded.Data, 2, ucs2Multipart16Units)
		}
	case smppenc.KindUTF16BE:
		limit = UCS2SingleUnits
		if encoded.Units > limit {
			chunks, err = splitUTF16(encoded.Data, ucs2Multipart16Units)
			if err != nil {
				return nil, err
			}
		}
	default:
		return nil, fmt.Errorf("%w: unsupported kind %s", ErrInvalidSegments, encoded.Kind)
	}
	if encoded.Units <= limit {
		data := clone(encoded.Data)
		return []Segment{{
			Kind: encoded.Kind, DataCoding: encoded.DataCoding, Units: encoded.Units,
			Data: clone(data), UserData: data, Total: 1, Sequence: 1,
		}}, nil
	}
	return makeUDH16UnpackedSegments(chunks, encoded.Kind, encoded.DataCoding, reference)
}

// SegmentBinaryUDH16 uses the same 16-bit concatenation reference for binary
// message content. A single binary message <=140 octets remains unfragmented;
// multipart binary chunks contain at most 133 content octets plus 7 UDH bytes.
func SegmentBinaryUDH16(data []byte, reference uint16) ([]Segment, error) {
	if len(data) <= BinarySingleOctets {
		content := clone(data)
		return []Segment{{
			Kind: smppenc.KindBinary, DataCoding: protocol.DataCodingOctetBinary2,
			Units: len(data), Data: clone(content), UserData: content, Total: 1, Sequence: 1,
		}}, nil
	}
	return makeUDH16UnpackedSegments(splitBytes(data, binaryMultipart16Bytes), smppenc.KindBinary, protocol.DataCodingOctetBinary2, reference)
}

func makeUDH16UnpackedSegments(chunks [][]byte, kind smppenc.Kind, coding protocol.DataCoding, reference uint16) ([]Segment, error) {
	if len(chunks) == 0 || len(chunks) > 255 {
		return nil, fmt.Errorf("%w: part count %d", ErrInvalidSegments, len(chunks))
	}
	total := uint8(len(chunks))
	out := make([]Segment, len(chunks))
	for i, chunk := range chunks {
		seq := uint8(i + 1)
		udh := BuildConcatUDH16(reference, total, seq)
		content := make([]byte, len(udh)+len(chunk))
		copy(content, udh)
		copy(content[len(udh):], chunk)
		units := len(chunk)
		if kind == smppenc.KindUCS2 || kind == smppenc.KindUTF16BE {
			units /= 2
		}
		out[i] = Segment{
			Reference: reference, Total: total, Sequence: seq,
			Kind: kind, DataCoding: coding, Units: units,
			Data: clone(chunk), UDH: udh, UserData: content,
		}
	}
	return out, nil
}
