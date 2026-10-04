package message

import (
	"errors"
	"testing"

	smppenc "github.com/majiddarvishan/go-smpp/encoding"
	"github.com/majiddarvishan/go-smpp/protocol"
)

// B8, public contract. An empty message is one single-part Segment with an
// empty body: not an error, no UDH, no SAR TLVs. This is what the code has
// always done, and it is now documented on each entry point; pinning it here
// keeps the documentation and the behaviour from drifting apart. The empty
// segment comes from each function's "fits in one part" branch, never from
// the split helpers (see TestSplitHelpersReturnNoPartsForEmptyInput).
func TestEmptyMessageIsOneEmptySinglePartSegment(t *testing.T) {
	cases := []struct {
		name string
		call func() ([]Segment, error)
		kind smppenc.Kind
	}{
		{"SegmentTextUDH", func() ([]Segment, error) { return SegmentTextUDH("", false, 1) }, smppenc.KindGSM7},
		{"SegmentTextSAR", func() ([]Segment, error) { return SegmentTextSAR("", false, 1) }, smppenc.KindGSM7},
		{"SegmentBinaryUDH(nil)", func() ([]Segment, error) { return SegmentBinaryUDH(nil, 1) }, smppenc.KindBinary},
		{"SegmentBinaryUDH(empty)", func() ([]Segment, error) { return SegmentBinaryUDH([]byte{}, 1) }, smppenc.KindBinary},
		{"SegmentBinarySAR(nil)", func() ([]Segment, error) { return SegmentBinarySAR(nil, 1) }, smppenc.KindBinary},
		{"SegmentBinarySAR(empty)", func() ([]Segment, error) { return SegmentBinarySAR([]byte{}, 1) }, smppenc.KindBinary},
	}
	for _, c := range cases {
		segs, err := c.call()
		if err != nil {
			t.Fatalf("%s: unexpected error %v", c.name, err)
		}
		if len(segs) != 1 {
			t.Fatalf("%s: got %d segments, want exactly 1", c.name, len(segs))
		}
		s := segs[0]
		if s.Total != 1 || s.Sequence != 1 || s.Units != 0 || s.Kind != c.kind {
			t.Fatalf("%s: total=%d seq=%d units=%d kind=%v", c.name, s.Total, s.Sequence, s.Units, s.Kind)
		}
		if len(s.Data) != 0 || len(s.UserData) != 0 || len(s.UDH) != 0 || len(s.Optional) != 0 {
			t.Fatalf("%s: body not empty: data=%d userdata=%d udh=%d optional=%d", c.name, len(s.Data), len(s.UserData), len(s.UDH), len(s.Optional))
		}
	}
}

// B8, helpers. "Split into parts" of nothing is zero parts. Returning one
// empty part would let a caller that forgot its own emptiness guard emit an
// empty multipart segment carrying only a UDH; with zero parts,
// makeUDHSegments rejects it (TestMakeUDHSegmentsRejectsZeroChunks).
func TestSplitHelpersReturnNoPartsForEmptyInput(t *testing.T) {
	for _, in := range [][]byte{nil, {}} {
		if got := splitBytes(in, 153); len(got) != 0 {
			t.Fatalf("splitBytes(%#v) = %#v, want no parts", in, got)
		}
		if got := splitFixedUnits(in, 2, 67); len(got) != 0 {
			t.Fatalf("splitFixedUnits(%#v) = %#v, want no parts", in, got)
		}
		got, err := splitUTF16(in, 67)
		if err != nil || len(got) != 0 {
			t.Fatalf("splitUTF16(%#v) = %#v, %v; want no parts, nil error", in, got, err)
		}
	}
}

// Non-empty input must be unaffected by the empty-input rule: exact multiple,
// remainder, and a single short chunk.
func TestSplitBytesNonEmptyUnchanged(t *testing.T) {
	data := make([]byte, 10)
	for i := range data {
		data[i] = byte(i)
	}
	for _, c := range []struct {
		max  int
		want []int
	}{{5, []int{5, 5}}, {4, []int{4, 4, 2}}, {10, []int{10}}, {64, []int{10}}} {
		parts := splitBytes(data, c.max)
		if len(parts) != len(c.want) {
			t.Fatalf("max=%d: %d parts, want %d", c.max, len(parts), len(c.want))
		}
		var joined []byte
		for i, p := range parts {
			if len(p) != c.want[i] {
				t.Fatalf("max=%d part %d: len %d, want %d", c.max, i, len(p), c.want[i])
			}
			joined = append(joined, p...)
		}
		if string(joined) != string(data) {
			t.Fatalf("max=%d: parts do not rejoin to the input", c.max)
		}
	}
}

// The zero-part result above is only safe because the multipart builder fails
// loudly on it. Pin that loudness so the two cannot be weakened independently.
func TestMakeUDHSegmentsRejectsZeroChunks(t *testing.T) {
	_, err := makeUDHSegments(nil, smppenc.KindBinary, protocol.DataCodingOctetBinary2, 1)
	if !errors.Is(err, ErrInvalidSegments) {
		t.Fatalf("err = %v, want ErrInvalidSegments", err)
	}
}
