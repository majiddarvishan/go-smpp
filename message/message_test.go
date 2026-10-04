package message

import (
	"bytes"
	"strings"
	"testing"

	smppenc "github.com/majiddarvishan/go-smpp/encoding"
	"github.com/majiddarvishan/go-smpp/protocol"
)

func TestGSM7SegmentationCountsSeptetsNotRunes(t *testing.T) {
	parts, err := SegmentTextUDH(strings.Repeat("A", 160), false, 0x33)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 1 || parts[0].Units != 160 || len(parts[0].UDH) != 0 {
		t.Fatalf("parts=%+v", parts)
	}

	parts, err = SegmentTextUDH(strings.Repeat("A", 161), false, 0x33)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 2 || parts[0].Units != 153 || parts[1].Units != 8 {
		t.Fatalf("units=%d/%d", parts[0].Units, parts[1].Units)
	}
	if len(parts[0].UDH) != 6 || GSM7UDHFillBits(len(parts[0].UDH)) != 1 {
		t.Fatalf("udh=%x", parts[0].UDH)
	}
	text, err := DecodeTextSegments(parts)
	if err != nil || text != strings.Repeat("A", 161) {
		t.Fatalf("text len=%d err=%v", len(text), err)
	}

	// '^' is one Go rune but requires ESC + extension code, i.e. two septets.
	parts, err = SegmentTextUDH(strings.Repeat("^", 81), false, 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 2 {
		t.Fatalf("extension-table text should split, parts=%d", len(parts))
	}
}

func TestUnicodeSegmentationCountsCodeUnitsAndPreservesEmojiPair(t *testing.T) {
	parts, err := SegmentTextUDH(strings.Repeat("界", 71), false, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 2 || parts[0].Kind != smppenc.KindUCS2 || parts[0].Units != 67 || parts[1].Units != 4 {
		t.Fatalf("parts=%+v", parts)
	}

	parts, err = SegmentTextUDH(strings.Repeat("😀", 36), true, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 2 || parts[0].Kind != smppenc.KindUTF16BE || parts[0].Units != 66 || parts[1].Units != 6 {
		t.Fatalf("emoji units=%d/%d kind=%s", parts[0].Units, parts[1].Units, parts[0].Kind)
	}
	text, err := DecodeTextSegments(parts)
	if err != nil || text != strings.Repeat("😀", 36) {
		t.Fatalf("decode err=%v len=%d", err, len([]rune(text)))
	}
}

func TestBinaryUDHAndReassembly(t *testing.T) {
	payload := bytes.Repeat([]byte{0xaa}, 300)
	parts, err := SegmentBinaryUDH(payload, 0x42)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 3 || len(parts[0].Data) != BinaryMultipartOctets {
		t.Fatalf("parts=%d first=%d", len(parts), len(parts[0].Data))
	}
	ref, total, seq, headerLen, err := ParseConcatUDH(parts[0].UserData)
	if err != nil || ref != 0x42 || total != 3 || seq != 1 || headerLen != 6 {
		t.Fatalf("udh ref=%x total=%d seq=%d len=%d err=%v", ref, total, seq, headerLen, err)
	}
	joined, err := Reassemble(parts)
	if err != nil || !bytes.Equal(joined, payload) {
		t.Fatalf("reassemble len=%d err=%v", len(joined), err)
	}
}

// TestReassembleRejectsDuplicateSequence is the acceptance test for Task 3.3
// (Finding B9): a duplicate Sequence value must return an error, and now a
// specific one naming the duplicate, not just the generic inconsistency
// error a later position check would have produced anyway.
func TestReassembleRejectsDuplicateSequence(t *testing.T) {
	payload := bytes.Repeat([]byte{0xaa}, 300)
	parts, err := SegmentBinaryUDH(payload, 0x42)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 3 {
		t.Fatalf("parts=%d, want 3", len(parts))
	}
	dup := append([]Segment(nil), parts...)
	dup[2] = dup[1] // sequence 2 appears twice; sequence 3 is now missing entirely
	_, err = Reassemble(dup)
	if err == nil {
		t.Fatal("Reassemble succeeded on a duplicate-sequence set, want an error")
	}
	if !strings.Contains(err.Error(), "duplicate sequence") {
		t.Fatalf("error = %q, want it to name the duplicate sequence", err.Error())
	}
}

// TestReassembleRejectsZeroTotal is Task 3.3's other acceptance case: a
// segment set with no explicit total must error rather than silently
// treating len(segments) as a confident total — a caller that doesn't know
// how many segments the message has cannot claim it has the complete set.
func TestReassembleRejectsZeroTotal(t *testing.T) {
	segs := []Segment{
		{Reference: 7, Total: 0, Sequence: 1, Data: []byte("A")},
		{Reference: 7, Total: 0, Sequence: 2, Data: []byte("B")},
	}
	_, err := Reassemble(segs)
	if err == nil {
		t.Fatal("Reassemble succeeded with total=0 on every segment, want an error")
	}
	if !strings.Contains(err.Error(), "total must be specified") {
		t.Fatalf("error = %q, want it to say total must be specified", err.Error())
	}
}

// TestReassembleRejectsIncompleteSet pins the acceptance criterion's other
// named case directly: a correctly-declared total with fewer parts than
// that total must not truncate silently.
func TestReassembleRejectsIncompleteSet(t *testing.T) {
	payload := bytes.Repeat([]byte{0xaa}, 300)
	parts, err := SegmentBinaryUDH(payload, 0x42)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 3 {
		t.Fatalf("parts=%d, want 3", len(parts))
	}
	incomplete := parts[:2] // segments 1 and 2 of a declared 3-part message
	_, err = Reassemble(incomplete)
	if err == nil {
		t.Fatal("Reassemble succeeded on an incomplete set, want an error")
	}
}

func TestSARHelpersAndSegmentation(t *testing.T) {
	tlvs := SARTLVs(0x1234, 3, 2)
	ref, total, seq, err := ParseSAR(tlvs)
	if err != nil || ref != 0x1234 || total != 3 || seq != 2 {
		t.Fatalf("sar ref=%x total=%d seq=%d err=%v", ref, total, seq, err)
	}
	parts, err := SegmentTextSAR(strings.Repeat("A", 161), false, 0x1234)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 2 || len(parts[0].UDH) != 0 {
		t.Fatalf("parts=%d udh=%x", len(parts), parts[0].UDH)
	}
	if parts[0].Optional[0].Tag != protocol.TLVTagSARMsgRefNum {
		t.Fatalf("optional=%+v", parts[0].Optional)
	}
}

func TestTextDataCodingSelection(t *testing.T) {
	gsm, err := EncodeText("hello", false)
	if err != nil || gsm.DataCoding != protocol.DataCodingSMSCDefault {
		t.Fatalf("gsm=%+v err=%v", gsm, err)
	}
	emoji, err := EncodeText("😀", true)
	if err != nil || emoji.DataCoding != protocol.DataCodingUCS2 || emoji.Kind != smppenc.KindUTF16BE {
		t.Fatalf("emoji=%+v err=%v", emoji, err)
	}
}
