package encoding

import (
	"bytes"
	"errors"
	"testing"
)

func TestGSM7DefaultAndExtensionRoundTrip(t *testing.T) {
	text := "Hello £ΔÄñ ^{}\\[~]|€"
	septets, err := EncodeGSM7(text)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeGSM7(septets)
	if err != nil {
		t.Fatal(err)
	}
	if decoded != text {
		t.Fatalf("decoded=%q", decoded)
	}

	for _, r := range "^{}\\[~]|€" {
		encoded, err := EncodeGSM7(string(r))
		if err != nil {
			t.Fatalf("%q: %v", r, err)
		}
		if len(encoded) != 2 || encoded[0] != gsmEscape {
			t.Fatalf("extension %q encoded as %x", r, encoded)
		}
	}
}

func TestGSM7PackingRoundTrip(t *testing.T) {
	septets, err := EncodeGSM7("hello")
	if err != nil {
		t.Fatal(err)
	}
	packed, err := PackSeptets(septets)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0xe8, 0x32, 0x9b, 0xfd, 0x06}
	if !bytes.Equal(packed, want) {
		t.Fatalf("packed=%x want=%x", packed, want)
	}
	unpacked, err := UnpackSeptets(packed, len(septets), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(unpacked, septets) {
		t.Fatalf("unpacked=%x septets=%x", unpacked, septets)
	}
}

func TestGSM7PackingWithUDHFill(t *testing.T) {
	septets, _ := EncodeGSM7("abcdef")
	packed, err := PackSeptetsWithFill(septets, 1)
	if err != nil {
		t.Fatal(err)
	}
	unpacked, err := UnpackSeptets(packed, len(septets), 1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(unpacked, septets) {
		t.Fatalf("unpacked=%x", unpacked)
	}
}

func TestUCS2StrictAndEmojiUTF16(t *testing.T) {
	bmp := "سلام 世界"
	encoded, err := EncodeUCS2(bmp)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeUCS2(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded != bmp {
		t.Fatalf("decoded=%q", decoded)
	}

	if _, err := EncodeUCS2("😀"); !errors.Is(err, ErrUnrepresentableRune) {
		t.Fatalf("strict UCS2 emoji error=%v", err)
	}
	utf16be, err := EncodeUTF16BE("😀")
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0xd8, 0x3d, 0xde, 0x00}
	if !bytes.Equal(utf16be, want) {
		t.Fatalf("emoji=%x want=%x", utf16be, want)
	}
	decoded, err = DecodeUTF16BE(utf16be)
	if err != nil || decoded != "😀" {
		t.Fatalf("decoded=%q err=%v", decoded, err)
	}
}

func TestSelectTextPreferenceAndSurrogatePolicy(t *testing.T) {
	selected, err := SelectText("hello ^", false)
	if err != nil || selected.Kind != KindGSM7 {
		t.Fatalf("selected=%+v err=%v", selected, err)
	}
	selected, err = SelectText("世界", false)
	if err != nil || selected.Kind != KindUCS2 {
		t.Fatalf("selected=%+v err=%v", selected, err)
	}
	if _, err = SelectText("😀", false); !errors.Is(err, ErrUnrepresentableRune) {
		t.Fatalf("err=%v", err)
	}
	selected, err = SelectText("😀", true)
	if err != nil || selected.Kind != KindUTF16BE || selected.Units != 2 {
		t.Fatalf("selected=%+v err=%v", selected, err)
	}
}
