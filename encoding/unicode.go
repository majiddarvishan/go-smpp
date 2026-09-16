package encoding

import (
	"encoding/binary"
	"fmt"
	"unicode/utf16"
	"unicode/utf8"
)

// Kind identifies the message text representation chosen before it is mapped to
// an SMPP data_coding value by the higher-level message package.
type Kind uint8

const (
	KindUnknown Kind = iota
	KindGSM7
	KindUCS2
	KindUTF16BE
	KindBinary
)

func (k Kind) String() string {
	switch k {
	case KindGSM7:
		return "gsm7"
	case KindUCS2:
		return "ucs2"
	case KindUTF16BE:
		return "utf16be"
	case KindBinary:
		return "binary"
	default:
		return "unknown"
	}
}

// EncodedText is a selected text representation. Units is septets for GSM7 and
// 16-bit code units for UCS2/UTF16BE, which is the quantity used for SMS length
// decisions rather than Go rune count.
type EncodedText struct {
	Kind  Kind
	Data  []byte
	Units int
}

// EncodeUCS2 encodes strict UCS-2/BMP text. Supplementary-plane characters,
// including most emoji, are rejected instead of silently producing surrogates.
func EncodeUCS2(text string) ([]byte, error) {
	if !utf8.ValidString(text) {
		return nil, ErrInvalidUnicode
	}
	units := make([]uint16, 0, len(text)/2+1)
	for _, r := range text {
		if r > 0xffff || (r >= 0xd800 && r <= 0xdfff) {
			return nil, fmt.Errorf("%w in UCS-2: %U", ErrUnrepresentableRune, r)
		}
		units = append(units, uint16(r))
	}
	out := make([]byte, len(units)*2)
	for i, unit := range units {
		binary.BigEndian.PutUint16(out[i*2:], unit)
	}
	return out, nil
}

func DecodeUCS2(data []byte) (string, error) {
	if len(data)%2 != 0 {
		return "", fmt.Errorf("%w: odd UCS-2 length %d", ErrInvalidUnicode, len(data))
	}
	runes := make([]rune, 0, len(data)/2)
	for i := 0; i < len(data); i += 2 {
		unit := binary.BigEndian.Uint16(data[i:])
		if unit >= 0xd800 && unit <= 0xdfff {
			return "", fmt.Errorf("%w: surrogate 0x%04x in strict UCS-2", ErrInvalidUnicode, unit)
		}
		runes = append(runes, rune(unit))
	}
	return string(runes), nil
}

// EncodeUTF16BE encodes Unicode as UTF-16BE and therefore supports
// supplementary-plane characters through surrogate pairs.
func EncodeUTF16BE(text string) ([]byte, error) {
	if !utf8.ValidString(text) {
		return nil, ErrInvalidUnicode
	}
	units := utf16.Encode([]rune(text))
	out := make([]byte, len(units)*2)
	for i, unit := range units {
		binary.BigEndian.PutUint16(out[i*2:], unit)
	}
	return out, nil
}

func DecodeUTF16BE(data []byte) (string, error) {
	if len(data)%2 != 0 {
		return "", fmt.Errorf("%w: odd UTF-16BE length %d", ErrInvalidUnicode, len(data))
	}
	units := make([]uint16, len(data)/2)
	for i := range units {
		units[i] = binary.BigEndian.Uint16(data[i*2:])
	}
	for i := 0; i < len(units); i++ {
		u := units[i]
		switch {
		case u >= 0xd800 && u <= 0xdbff:
			if i+1 >= len(units) || units[i+1] < 0xdc00 || units[i+1] > 0xdfff {
				return "", fmt.Errorf("%w: unpaired high surrogate 0x%04x", ErrInvalidUnicode, u)
			}
			i++
		case u >= 0xdc00 && u <= 0xdfff:
			return "", fmt.Errorf("%w: unpaired low surrogate 0x%04x", ErrInvalidUnicode, u)
		}
	}
	return string(utf16.Decode(units)), nil
}

func UTF16CodeUnitCount(text string) (int, error) {
	if !utf8.ValidString(text) {
		return 0, ErrInvalidUnicode
	}
	return len(utf16.Encode([]rune(text))), nil
}

// SelectText prefers GSM 03.38, then strict UCS-2, then (when permitted)
// UTF-16BE with surrogate pairs. Applications can therefore explicitly reject
// emoji on peers that claim UCS-2 but do not tolerate surrogate pairs.
func SelectText(text string, allowSurrogates bool) (EncodedText, error) {
	if septets, err := EncodeGSM7(text); err == nil {
		return EncodedText{Kind: KindGSM7, Data: septets, Units: len(septets)}, nil
	}
	if ucs2, err := EncodeUCS2(text); err == nil {
		return EncodedText{Kind: KindUCS2, Data: ucs2, Units: len(ucs2) / 2}, nil
	}
	if !allowSurrogates {
		return EncodedText{}, fmt.Errorf("%w: text requires UTF-16 surrogate pairs", ErrUnrepresentableRune)
	}
	utf16be, err := EncodeUTF16BE(text)
	if err != nil {
		return EncodedText{}, err
	}
	return EncodedText{Kind: KindUTF16BE, Data: utf16be, Units: len(utf16be) / 2}, nil
}

// CloneBinary returns an owned copy for binary SMS payload handling.
func CloneBinary(data []byte) EncodedText {
	return EncodedText{Kind: KindBinary, Data: append([]byte(nil), data...), Units: len(data)}
}
