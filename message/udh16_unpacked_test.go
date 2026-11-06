package message

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	smppenc "github.com/majiddarvishan/go-smpp/encoding"
	"github.com/majiddarvishan/go-smpp/protocol"
)

func TestUDH16UnpackedGSM7ExtensionBoundary(t *testing.T) {
	text := strings.Repeat("A", 151) + "^" + strings.Repeat("B", 40)
	segments, err := SegmentTextUDH16Unpacked(text, false, 0xbeef)
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 2 {
		t.Fatalf("segments=%d, want 2", len(segments))
	}
	var decoded strings.Builder
	for i, seg := range segments {
		wantUDH := []byte{0x06, 0x08, 0x04, 0xbe, 0xef, 2, byte(i + 1)}
		if !bytes.Equal(seg.UDH, wantUDH) {
			t.Fatalf("segment %d UDH=%x want=%x", i, seg.UDH, wantUDH)
		}
		if !bytes.Equal(seg.UserData, append(append([]byte(nil), seg.UDH...), seg.Data...)) {
			t.Fatalf("segment %d GSM7 was packed or changed", i)
		}
		if len(seg.Data) > 152 || (len(seg.Data) > 0 && seg.Data[len(seg.Data)-1] == 0x1b) {
			t.Fatalf("segment %d has invalid GSM7 extension boundary: %x", i, seg.Data)
		}
		part, err := smppenc.DecodeGSM7(seg.Data)
		if err != nil {
			t.Fatalf("segment %d GSM7 decode: %v", i, err)
		}
		decoded.WriteString(part)
		if seg.Reference != 0xbeef || seg.DataCoding != protocol.DataCodingSMSCDefault {
			t.Fatalf("segment %d reference/coding mismatch", i)
		}
	}
	if decoded.String() != text {
		t.Fatal("unpacked GSM7 multipart text did not round-trip")
	}
	if len(segments[0].Data) != 151 || segments[1].Data[0] != 0x1b {
		t.Fatal("extension escape was separated from its extension code")
	}
}

func TestUDH16UnpackedUCS2Boundary(t *testing.T) {
	text := strings.Repeat("پ", 71)
	segments, err := SegmentTextUDH16Unpacked(text, false, 0x1234)
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 2 {
		t.Fatalf("segments=%d, want 2", len(segments))
	}
	if segments[0].Units != 66 || segments[1].Units != 5 {
		t.Fatalf("UCS2 units=%d,%d want=66,5", segments[0].Units, segments[1].Units)
	}
	for i, seg := range segments {
		if seg.DataCoding != protocol.DataCodingUCS2 || len(seg.UserData) != 7+len(seg.Data) || len(seg.UserData) > 139 {
			t.Fatalf("segment %d UCS2/UDH size mismatch", i)
		}
	}
}

func TestUDH16BinaryAndUnfragmentedGSM7(t *testing.T) {
	data := bytes.Repeat([]byte{0xa5}, 141)
	segments, err := SegmentBinaryUDH16(data, 0xf012)
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 2 || len(segments[0].Data) != 133 || len(segments[1].Data) != 8 {
		t.Fatalf("binary segments=%v", segments)
	}
	var joined []byte
	for _, seg := range segments {
		if len(seg.UDH) != 7 || !bytes.Equal(seg.UserData[7:], seg.Data) {
			t.Fatal("binary UDH16 does not match content")
		}
		joined = append(joined, seg.Data...)
	}
	if !bytes.Equal(joined, data) {
		t.Fatal("binary payload changed")
	}
	single, err := SegmentTextUDH16Unpacked(strings.Repeat("A", 160), false, 0x11)
	if err != nil {
		t.Fatal(err)
	}
	if len(single) != 1 || len(single[0].UDH) != 0 || len(single[0].UserData) != 160 {
		t.Fatalf("single GSM7 message unexpectedly fragmented: %v", single)
	}
	if !bytes.Equal(single[0].UserData, single[0].Data) {
		t.Fatal("single GSM7 message was packed")
	}
}

func TestUDH16RejectsMoreThan255Parts(t *testing.T) {
	_, err := SegmentTextUDH16Unpacked(strings.Repeat("A", 152*255+1), false, 1)
	if !errors.Is(err, ErrInvalidSegments) {
		t.Fatalf("part count error=%v want ErrInvalidSegments", err)
	}
}
