package encoding

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

var (
	ErrUnrepresentableRune = errors.New("smpp encoding: rune is not representable")
	ErrInvalidGSM7         = errors.New("smpp encoding: invalid GSM 7-bit data")
	ErrInvalidUnicode      = errors.New("smpp encoding: invalid Unicode data")
)

const gsmEscape byte = 0x1b

// GSM7Kind identifies the GSM 03.38 default alphabet.
const GSM7Kind Kind = KindGSM7

// gsmDefault contains the GSM 03.38 default alphabet indexed by septet value.
var gsmDefault = [128]rune{
	'@', '£', '$', '¥', 'è', 'é', 'ù', 'ì', 'ò', 'Ç', '\n', 'Ø', 'ø', '\r', 'Å', 'å',
	'Δ', '_', 'Φ', 'Γ', 'Λ', 'Ω', 'Π', 'Ψ', 'Σ', 'Θ', 'Ξ', 0, 'Æ', 'æ', 'ß', 'É',
	' ', '!', '"', '#', '¤', '%', '&', '\'', '(', ')', '*', '+', ',', '-', '.', '/',
	'0', '1', '2', '3', '4', '5', '6', '7', '8', '9', ':', ';', '<', '=', '>', '?',
	'¡', 'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J', 'K', 'L', 'M', 'N', 'O',
	'P', 'Q', 'R', 'S', 'T', 'U', 'V', 'W', 'X', 'Y', 'Z', 'Ä', 'Ö', 'Ñ', 'Ü', '§',
	'¿', 'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm', 'n', 'o',
	'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z', 'ä', 'ö', 'ñ', 'ü', 'à',
}

var gsmExtension = map[byte]rune{
	0x0a: '\f',
	0x14: '^',
	0x28: '{',
	0x29: '}',
	0x2f: '\\',
	0x3c: '[',
	0x3d: '~',
	0x3e: ']',
	0x40: '|',
	0x65: '€',
}

var (
	gsmDefaultReverse   map[rune]byte
	gsmExtensionReverse map[rune]byte
)

func init() {
	gsmDefaultReverse = make(map[rune]byte, len(gsmDefault))
	for i, r := range gsmDefault {
		if i == int(gsmEscape) || r == 0 {
			continue
		}
		gsmDefaultReverse[r] = byte(i)
	}
	gsmExtensionReverse = make(map[rune]byte, len(gsmExtension))
	for code, r := range gsmExtension {
		gsmExtensionReverse[r] = code
	}
}

// EncodeGSM7 converts UTF-8 text to unpacked GSM 03.38 septets. Characters in
// the extension table are encoded as ESC followed by the extension code and
// therefore consume two septets.
func EncodeGSM7(text string) ([]byte, error) {
	if !utf8.ValidString(text) {
		return nil, ErrInvalidUnicode
	}
	out := make([]byte, 0, len(text))
	for _, r := range text {
		if code, ok := gsmDefaultReverse[r]; ok {
			out = append(out, code)
			continue
		}
		if code, ok := gsmExtensionReverse[r]; ok {
			out = append(out, gsmEscape, code)
			continue
		}
		return nil, fmt.Errorf("%w: %U", ErrUnrepresentableRune, r)
	}
	return out, nil
}

// DecodeGSM7 converts unpacked GSM 03.38 septets to UTF-8 text.
func DecodeGSM7(septets []byte) (string, error) {
	var b strings.Builder
	b.Grow(len(septets))
	for i := 0; i < len(septets); i++ {
		code := septets[i]
		if code > 0x7f {
			return "", fmt.Errorf("%w: septet 0x%02x", ErrInvalidGSM7, code)
		}
		if code == gsmEscape {
			i++
			if i >= len(septets) {
				return "", fmt.Errorf("%w: trailing escape", ErrInvalidGSM7)
			}
			r, ok := gsmExtension[septets[i]]
			if !ok {
				return "", fmt.Errorf("%w: extension 0x%02x", ErrInvalidGSM7, septets[i])
			}
			b.WriteRune(r)
			continue
		}
		r := gsmDefault[code]
		if r == 0 {
			return "", fmt.Errorf("%w: reserved septet 0x%02x", ErrInvalidGSM7, code)
		}
		b.WriteRune(r)
	}
	return b.String(), nil
}

// GSM7SeptetCount returns the encoded septet count without packing.
func GSM7SeptetCount(text string) (int, error) {
	septets, err := EncodeGSM7(text)
	if err != nil {
		return 0, err
	}
	return len(septets), nil
}

// CanEncodeGSM7 reports whether all runes are representable in GSM 03.38.
func CanEncodeGSM7(text string) bool {
	_, err := GSM7SeptetCount(text)
	return err == nil
}

// PackSeptets packs unpacked GSM septets into octets with no leading fill bits.
func PackSeptets(septets []byte) ([]byte, error) {
	return PackSeptetsWithFill(septets, 0)
}

// PackSeptetsWithFill packs septets after fillBits zero bits. This is useful for
// UDH-bearing GSM user data where the text begins on the next septet boundary.
func PackSeptetsWithFill(septets []byte, fillBits uint8) ([]byte, error) {
	if fillBits > 6 {
		return nil, fmt.Errorf("%w: fill bits %d", ErrInvalidGSM7, fillBits)
	}
	if len(septets) == 0 {
		if fillBits == 0 {
			return nil, nil
		}
		return []byte{0}, nil
	}
	bits := int(fillBits) + len(septets)*7
	out := make([]byte, (bits+7)/8)
	bitPos := int(fillBits)
	for _, septet := range septets {
		if septet > 0x7f {
			return nil, fmt.Errorf("%w: septet 0x%02x", ErrInvalidGSM7, septet)
		}
		for bit := 0; bit < 7; bit++ {
			if septet&(1<<bit) != 0 {
				pos := bitPos + bit
				out[pos/8] |= 1 << (pos % 8)
			}
		}
		bitPos += 7
	}
	return out, nil
}

// UnpackSeptets reverses PackSeptetsWithFill. count is the exact number of
// septets to recover, preventing padding bits from becoming phantom characters.
func UnpackSeptets(data []byte, count int, fillBits uint8) ([]byte, error) {
	if count < 0 || fillBits > 6 {
		return nil, fmt.Errorf("%w: count=%d fill=%d", ErrInvalidGSM7, count, fillBits)
	}
	requiredBits := int(fillBits) + count*7
	if requiredBits > len(data)*8 {
		return nil, fmt.Errorf("%w: need %d bits, have %d", ErrInvalidGSM7, requiredBits, len(data)*8)
	}
	out := make([]byte, count)
	bitPos := int(fillBits)
	for i := range out {
		var septet byte
		for bit := 0; bit < 7; bit++ {
			pos := bitPos + bit
			if data[pos/8]&(1<<(pos%8)) != 0 {
				septet |= 1 << bit
			}
		}
		out[i] = septet
		bitPos += 7
	}
	return out, nil
}
