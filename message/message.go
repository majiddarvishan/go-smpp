package message

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"

	smppenc "github.com/majiddarvishan/go-smpp/encoding"
	"github.com/majiddarvishan/go-smpp/protocol"
)

const (
	// ESMClassUDHI is set in esm_class when short_message/message_payload begins
	// with a User Data Header.
	ESMClassUDHI uint8 = 0x40

	GSM7SingleSeptets     = 160
	GSM7MultipartSeptets  = 153 // 6-byte 8-bit concat UDH consumes 7 septets.
	UCS2SingleUnits       = 70
	UCS2MultipartUnits    = 67
	BinarySingleOctets    = 140
	BinaryMultipartOctets = 134
)

var (
	ErrInvalidSegments = errors.New("smpp message: invalid multipart segments")
	ErrInvalidUDH      = errors.New("smpp message: invalid user data header")
	ErrInvalidSAR      = errors.New("smpp message: invalid SAR parameters")
)

// EncodedText maps the encoding package's representation to SMPP data_coding.
// UTF16BE intentionally uses data_coding 0x08 as an explicit interoperability
// mode for peers that accept surrogate pairs under the UCS-2 coding value.
type EncodedText struct {
	Kind       smppenc.Kind
	DataCoding protocol.DataCoding
	Data       []byte // unpacked septets for GSM7; big-endian code units otherwise.
	Units      int
}

func EncodeText(text string, allowSurrogates bool) (EncodedText, error) {
	selected, err := smppenc.SelectText(text, allowSurrogates)
	if err != nil {
		return EncodedText{}, err
	}
	coding := protocol.DataCodingUCS2
	if selected.Kind == smppenc.KindGSM7 {
		coding = protocol.DataCodingSMSCDefault
	}
	return EncodedText{Kind: selected.Kind, DataCoding: coding, Data: selected.Data, Units: selected.Units}, nil
}

// Segment contains both logical encoded content and a TP-UD-style wire form.
// For GSM7, Data contains unpacked septets while UserData contains packed septets
// (and optional UDH). Keeping both avoids losing exact septet counts to octet
// padding. For UCS2/UTF16BE/binary, Data is the content octets and UserData is
// UDH followed by those octets.
type Segment struct {
	Reference  uint16
	Total      uint8
	Sequence   uint8
	Kind       smppenc.Kind
	DataCoding protocol.DataCoding
	Units      int
	Data       []byte
	UDH        []byte
	UserData   []byte
	Optional   []protocol.OptionalParameter
}

func SegmentTextUDH(text string, allowSurrogates bool, reference byte) ([]Segment, error) {
	encoded, err := EncodeText(text, allowSurrogates)
	if err != nil {
		return nil, err
	}
	switch encoded.Kind {
	case smppenc.KindGSM7:
		if encoded.Units <= GSM7SingleSeptets {
			packed, err := smppenc.PackSeptets(encoded.Data)
			if err != nil {
				return nil, err
			}
			return []Segment{{Kind: encoded.Kind, DataCoding: encoded.DataCoding, Units: encoded.Units, Data: clone(encoded.Data), UserData: packed, Total: 1, Sequence: 1}}, nil
		}
		chunks := splitBytes(encoded.Data, GSM7MultipartSeptets)
		return makeUDHSegments(chunks, encoded.Kind, encoded.DataCoding, reference)
	case smppenc.KindUCS2:
		if encoded.Units <= UCS2SingleUnits {
			return []Segment{{Kind: encoded.Kind, DataCoding: encoded.DataCoding, Units: encoded.Units, Data: clone(encoded.Data), UserData: clone(encoded.Data), Total: 1, Sequence: 1}}, nil
		}
		chunks := splitFixedUnits(encoded.Data, 2, UCS2MultipartUnits)
		return makeUDHSegments(chunks, encoded.Kind, encoded.DataCoding, reference)
	case smppenc.KindUTF16BE:
		if encoded.Units <= UCS2SingleUnits {
			return []Segment{{Kind: encoded.Kind, DataCoding: encoded.DataCoding, Units: encoded.Units, Data: clone(encoded.Data), UserData: clone(encoded.Data), Total: 1, Sequence: 1}}, nil
		}
		chunks, err := splitUTF16(encoded.Data, UCS2MultipartUnits)
		if err != nil {
			return nil, err
		}
		return makeUDHSegments(chunks, encoded.Kind, encoded.DataCoding, reference)
	default:
		return nil, fmt.Errorf("%w: unsupported text kind %s", ErrInvalidSegments, encoded.Kind)
	}
}

func SegmentBinaryUDH(data []byte, reference byte) ([]Segment, error) {
	if len(data) <= BinarySingleOctets {
		return []Segment{{Kind: smppenc.KindBinary, DataCoding: protocol.DataCodingOctetBinary2, Units: len(data), Data: clone(data), UserData: clone(data), Total: 1, Sequence: 1}}, nil
	}
	return makeUDHSegments(splitBytes(data, BinaryMultipartOctets), smppenc.KindBinary, protocol.DataCodingOctetBinary2, reference)
}

func makeUDHSegments(chunks [][]byte, kind smppenc.Kind, coding protocol.DataCoding, reference byte) ([]Segment, error) {
	if len(chunks) == 0 || len(chunks) > 255 {
		return nil, fmt.Errorf("%w: part count %d", ErrInvalidSegments, len(chunks))
	}
	total := uint8(len(chunks))
	segments := make([]Segment, len(chunks))
	for i, chunk := range chunks {
		seq := uint8(i + 1)
		udh := BuildConcatUDH8(reference, total, seq)
		userData := clone(udh)
		units := len(chunk)
		if kind == smppenc.KindGSM7 {
			fill := GSM7UDHFillBits(len(udh))
			packed, err := smppenc.PackSeptetsWithFill(chunk, fill)
			if err != nil {
				return nil, err
			}
			userData = append(userData, packed...)
		} else {
			userData = append(userData, chunk...)
			if kind == smppenc.KindUCS2 || kind == smppenc.KindUTF16BE {
				units = len(chunk) / 2
			}
		}
		segments[i] = Segment{Reference: uint16(reference), Total: total, Sequence: seq, Kind: kind, DataCoding: coding, Units: units, Data: clone(chunk), UDH: udh, UserData: userData}
	}
	return segments, nil
}

// GSM7UDHFillBits returns the zero fill bits needed after a UDH so the first
// text septet begins on the next septet boundary.
func GSM7UDHFillBits(headerOctets int) uint8 {
	if headerOctets <= 0 {
		return 0
	}
	mod := (headerOctets * 8) % 7
	if mod == 0 {
		return 0
	}
	return uint8(7 - mod)
}

func BuildConcatUDH8(reference, total, sequence byte) []byte {
	return []byte{0x05, 0x00, 0x03, reference, total, sequence}
}

func BuildConcatUDH16(reference uint16, total, sequence byte) []byte {
	return []byte{0x06, 0x08, 0x04, byte(reference >> 8), byte(reference), total, sequence}
}

// ParseConcatUDH parses the standard 8-bit or 16-bit concatenation IE and
// returns the total UDH length in octets so callers can locate message content.
func ParseConcatUDH(userData []byte) (reference uint16, total, sequence uint8, headerLen int, err error) {
	if len(userData) < 1 {
		return 0, 0, 0, 0, ErrInvalidUDH
	}
	headerLen = int(userData[0]) + 1
	if headerLen > len(userData) || headerLen < 6 {
		return 0, 0, 0, 0, fmt.Errorf("%w: declared header length %d", ErrInvalidUDH, headerLen)
	}
	for off := 1; off+2 <= headerLen; {
		iei := userData[off]
		length := int(userData[off+1])
		off += 2
		if off+length > headerLen {
			return 0, 0, 0, 0, ErrInvalidUDH
		}
		value := userData[off : off+length]
		off += length
		switch iei {
		case 0x00:
			if length != 3 {
				return 0, 0, 0, 0, ErrInvalidUDH
			}
			return uint16(value[0]), value[1], value[2], headerLen, nil
		case 0x08:
			if length != 4 {
				return 0, 0, 0, 0, ErrInvalidUDH
			}
			return binary.BigEndian.Uint16(value[:2]), value[2], value[3], headerLen, nil
		}
	}
	return 0, 0, 0, 0, fmt.Errorf("%w: concatenation IE missing", ErrInvalidUDH)
}

// Reassemble validates reference/ordering metadata and concatenates the logical
// Data fields, not padded GSM wire octets.
func Reassemble(segments []Segment) ([]byte, error) {
	if len(segments) == 0 {
		return nil, ErrInvalidSegments
	}
	parts := append([]Segment(nil), segments...)
	sort.Slice(parts, func(i, j int) bool { return parts[i].Sequence < parts[j].Sequence })
	ref, total, kind, coding := parts[0].Reference, parts[0].Total, parts[0].Kind, parts[0].DataCoding
	if total == 0 {
		total = uint8(len(parts))
	}
	if int(total) != len(parts) {
		return nil, fmt.Errorf("%w: have %d parts want %d", ErrInvalidSegments, len(parts), total)
	}
	var out []byte
	for i, part := range parts {
		if part.Reference != ref || part.Kind != kind || part.DataCoding != coding || part.Total != total || part.Sequence != uint8(i+1) {
			return nil, fmt.Errorf("%w: inconsistent part %d", ErrInvalidSegments, i+1)
		}
		out = append(out, part.Data...)
	}
	return out, nil
}

func DecodeTextSegments(segments []Segment) (string, error) {
	if len(segments) == 0 {
		return "", ErrInvalidSegments
	}
	data, err := Reassemble(segments)
	if err != nil {
		return "", err
	}
	switch segments[0].Kind {
	case smppenc.KindGSM7:
		return smppenc.DecodeGSM7(data)
	case smppenc.KindUCS2:
		return smppenc.DecodeUCS2(data)
	case smppenc.KindUTF16BE:
		return smppenc.DecodeUTF16BE(data)
	default:
		return "", fmt.Errorf("%w: non-text kind %s", ErrInvalidSegments, segments[0].Kind)
	}
}

// SARTLVs builds the three SMPP SAR optional parameters. sar_msg_ref_num is a
// two-octet unsigned value; total/sequence are single octets.
func SARTLVs(reference uint16, total, sequence uint8) []protocol.OptionalParameter {
	ref := []byte{byte(reference >> 8), byte(reference)}
	return []protocol.OptionalParameter{
		{Tag: protocol.TLVTagSARMsgRefNum, Value: ref},
		{Tag: protocol.TLVTagSARTotalSegments, Value: []byte{total}},
		{Tag: protocol.TLVTagSARSegmentSeqnum, Value: []byte{sequence}},
	}
}

func ParseSAR(optional []protocol.OptionalParameter) (reference uint16, total, sequence uint8, err error) {
	var haveRef, haveTotal, haveSeq bool
	for _, p := range optional {
		switch p.Tag {
		case protocol.TLVTagSARMsgRefNum:
			if len(p.Value) != 2 {
				return 0, 0, 0, ErrInvalidSAR
			}
			reference = binary.BigEndian.Uint16(p.Value)
			haveRef = true
		case protocol.TLVTagSARTotalSegments:
			if len(p.Value) != 1 {
				return 0, 0, 0, ErrInvalidSAR
			}
			total = p.Value[0]
			haveTotal = true
		case protocol.TLVTagSARSegmentSeqnum:
			if len(p.Value) != 1 {
				return 0, 0, 0, ErrInvalidSAR
			}
			sequence = p.Value[0]
			haveSeq = true
		}
	}
	if !haveRef || !haveTotal || !haveSeq || total == 0 || sequence == 0 || sequence > total {
		return 0, 0, 0, ErrInvalidSAR
	}
	return reference, total, sequence, nil
}

func SegmentTextSAR(text string, allowSurrogates bool, reference uint16) ([]Segment, error) {
	encoded, err := EncodeText(text, allowSurrogates)
	if err != nil {
		return nil, err
	}
	var chunks [][]byte
	switch encoded.Kind {
	case smppenc.KindGSM7:
		chunks = splitBytes(encoded.Data, GSM7SingleSeptets)
	case smppenc.KindUCS2:
		chunks = splitFixedUnits(encoded.Data, 2, UCS2SingleUnits)
	case smppenc.KindUTF16BE:
		chunks, err = splitUTF16(encoded.Data, UCS2SingleUnits)
		if err != nil {
			return nil, err
		}
	}
	if len(chunks) <= 1 {
		return SegmentTextUDH(text, allowSurrogates, byte(reference))
	}
	if len(chunks) > 255 {
		return nil, ErrInvalidSegments
	}
	total := uint8(len(chunks))
	result := make([]Segment, len(chunks))
	for i, chunk := range chunks {
		seq := uint8(i + 1)
		units := len(chunk)
		userData := clone(chunk)
		if encoded.Kind == smppenc.KindGSM7 {
			userData, err = smppenc.PackSeptets(chunk)
			if err != nil {
				return nil, err
			}
		} else {
			units = len(chunk) / 2
		}
		result[i] = Segment{Reference: reference, Total: total, Sequence: seq, Kind: encoded.Kind, DataCoding: encoded.DataCoding, Units: units, Data: clone(chunk), UserData: userData, Optional: SARTLVs(reference, total, seq)}
	}
	return result, nil
}

func SegmentBinarySAR(data []byte, reference uint16) ([]Segment, error) {
	chunks := splitBytes(data, BinarySingleOctets)
	if len(chunks) <= 1 {
		return SegmentBinaryUDH(data, byte(reference))
	}
	if len(chunks) > 255 {
		return nil, ErrInvalidSegments
	}
	total := uint8(len(chunks))
	out := make([]Segment, len(chunks))
	for i, chunk := range chunks {
		seq := uint8(i + 1)
		out[i] = Segment{Reference: reference, Total: total, Sequence: seq, Kind: smppenc.KindBinary, DataCoding: protocol.DataCodingOctetBinary2, Units: len(chunk), Data: clone(chunk), UserData: clone(chunk), Optional: SARTLVs(reference, total, seq)}
	}
	return out, nil
}

func splitBytes(data []byte, max int) [][]byte {
	if len(data) == 0 {
		return [][]byte{{}}
	}
	parts := make([][]byte, 0, (len(data)+max-1)/max)
	for len(data) > 0 {
		n := max
		if len(data) < n {
			n = len(data)
		}
		parts = append(parts, clone(data[:n]))
		data = data[n:]
	}
	return parts
}

func splitFixedUnits(data []byte, unitSize, maxUnits int) [][]byte {
	return splitBytes(data, unitSize*maxUnits)
}

func splitUTF16(data []byte, maxUnits int) ([][]byte, error) {
	if len(data)%2 != 0 {
		return nil, ErrInvalidSegments
	}
	var parts [][]byte
	start := 0
	units := 0
	for off := 0; off < len(data); {
		u := binary.BigEndian.Uint16(data[off:])
		width := 1
		if u >= 0xd800 && u <= 0xdbff {
			if off+4 > len(data) {
				return nil, ErrInvalidSegments
			}
			low := binary.BigEndian.Uint16(data[off+2:])
			if low < 0xdc00 || low > 0xdfff {
				return nil, ErrInvalidSegments
			}
			width = 2
		} else if u >= 0xdc00 && u <= 0xdfff {
			return nil, ErrInvalidSegments
		}
		if units > 0 && units+width > maxUnits {
			parts = append(parts, clone(data[start:off]))
			start = off
			units = 0
		}
		if width > maxUnits {
			return nil, ErrInvalidSegments
		}
		units += width
		off += width * 2
	}
	parts = append(parts, clone(data[start:]))
	return parts, nil
}

func clone(src []byte) []byte { return append([]byte(nil), src...) }
