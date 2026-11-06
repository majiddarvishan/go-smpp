package message

import (
	"bytes"
	"strings"
	"testing"

	"github.com/majiddarvishan/go-smpp/protocol"
)

func assertGSM7SegmentsKeepExtensions(t *testing.T, text string, segments []Segment, maxUnits int) {
	t.Helper()
	encoded, err := EncodeText(text, false)
	if err != nil {
		t.Fatal(err)
	}
	if encoded.DataCoding != protocol.DataCodingSMSCDefault {
		t.Fatalf("selected coding=%v, want GSM7", encoded.DataCoding)
	}
	var joined []byte
	for i, segment := range segments {
		if len(segment.Data) > maxUnits {
			t.Errorf("segment %d: %d septets exceeds %d", i, len(segment.Data), maxUnits)
		}
		if len(segment.Data) > 0 && segment.Data[len(segment.Data)-1] == 0x1b {
			t.Errorf("segment %d ends with orphan GSM7 extension escape", i)
		}
		if len(segment.Data) > 0 && segment.Data[0] != 0x1b && i > 0 && len(segments[i-1].Data) > 0 &&
			segments[i-1].Data[len(segments[i-1].Data)-1] == 0x1b {
			t.Errorf("segment %d starts after split escape", i)
		}
		joined = append(joined, segment.Data...)
	}
	if !bytes.Equal(joined, encoded.Data) {
		t.Fatal("segmentation changed encoded GSM7 septets")
	}
}

func TestUDHGSM7ExtensionDoesNotSplitAtBoundary(t *testing.T) {
	text := strings.Repeat("a", GSM7MultipartSeptets-1) + "^" + strings.Repeat("b", 20)
	segments, err := SegmentTextUDH(text, false, 0x2a)
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 2 {
		t.Fatalf("segments=%d, want 2", len(segments))
	}
	if len(segments[0].Data) != GSM7MultipartSeptets-1 {
		t.Fatalf("first part septets=%d, want %d", len(segments[0].Data), GSM7MultipartSeptets-1)
	}
	if len(segments[1].Data) < 2 || segments[1].Data[0] != 0x1b {
		t.Fatal("extension character was not moved together to the next UDH segment")
	}
	assertGSM7SegmentsKeepExtensions(t, text, segments, GSM7MultipartSeptets)
}

func TestSARGSM7ExtensionDoesNotSplitAtBoundary(t *testing.T) {
	text := strings.Repeat("a", GSM7SingleSeptets-1) + "^" + strings.Repeat("b", 20)
	segments, err := SegmentTextSAR(text, false, 0x1234)
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 2 {
		t.Fatalf("segments=%d, want 2", len(segments))
	}
	if len(segments[0].Data) != GSM7SingleSeptets-1 {
		t.Fatalf("first SAR part septets=%d", len(segments[0].Data))
	}
	if len(segments[1].Data) < 2 || segments[1].Data[0] != 0x1b {
		t.Fatal("extension character was not moved together to the next SAR segment")
	}
	assertGSM7SegmentsKeepExtensions(t, text, segments, GSM7SingleSeptets)
}

func TestGSM7BoundarySeveralConsecutiveExtensions(t *testing.T) {
	text := strings.Repeat("a", GSM7MultipartSeptets-1) + "^^^^" + strings.Repeat("b", GSM7MultipartSeptets)
	segments, err := SegmentTextUDH(text, false, 0x11)
	if err != nil {
		t.Fatal(err)
	}
	assertGSM7SegmentsKeepExtensions(t, text, segments, GSM7MultipartSeptets)
	if len(segments) < 3 {
		t.Fatalf("segments=%d, want at least three", len(segments))
	}
}
