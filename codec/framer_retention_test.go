package codec

import (
	"bytes"
	"testing"

	"github.com/majiddarvishan/go-smpp/protocol"
)

// retentionBound is deliberately a literal rather than framerShrinkThreshold:
// asserting against the production constant would pass for any threshold. It
// is the session's default read-buffer size (session.DefaultReadBufferSize,
// 64 KiB), which codec cannot import.
const retentionBound = 64 << 10

// feedInChunks feeds stream to f in chunk-sized pieces, collecting copies of
// every emitted frame (frames are borrowed, so they must be copied).
func feedInChunks(t *testing.T, f *Framer, stream []byte, chunk int) [][]byte {
	t.Helper()
	var got [][]byte
	for len(stream) > 0 {
		n := chunk
		if n > len(stream) {
			n = len(stream)
		}
		err := f.Feed(stream[:n], func(frame []byte) error {
			got = append(got, append([]byte(nil), frame...))
			return nil
		})
		if err != nil {
			t.Fatalf("Feed: %v", err)
		}
		stream = stream[n:]
	}
	return got
}

// B7: one large PDU that arrives fragmented must not pin its full capacity on
// the framer for the rest of the session. Both entry paths into the pending
// buffer are exercised: a first chunk that already carries the length word
// (chunk >= 4), and a first chunk too short to contain it (chunk < 4).
func TestFramerReleasesLargeBufferAfterFragmentedPDU(t *testing.T) {
	const size = 1 << 20
	for _, chunk := range []int{2, 3, 4096, 64 << 10} {
		f, err := NewFramer(size)
		if err != nil {
			t.Fatal(err)
		}
		pdu := makePDU(protocol.CommandSubmitSM, 7, bytes.Repeat([]byte{0xAB}, size-HeaderSize))
		if len(pdu) != size {
			t.Fatalf("setup: pdu is %d bytes", len(pdu))
		}
		frames := feedInChunks(t, f, pdu, chunk)
		if len(frames) != 1 || !bytes.Equal(frames[0], pdu) {
			t.Fatalf("chunk=%d: emitted %d frames, content match=%v", chunk, len(frames), len(frames) == 1 && bytes.Equal(frames[0], pdu))
		}
		if f.Buffered() != 0 {
			t.Fatalf("chunk=%d: Buffered=%d after emit", chunk, f.Buffered())
		}
		if got := cap(f.pending); got > retentionBound {
			t.Fatalf("chunk=%d: framer retains cap=%d after a %d-byte fragmented PDU; want <= %d", chunk, got, size, retentionBound)
		}
	}
}

// Positive control: a fragmented PDU at or below the threshold keeps its
// buffer, so ordinary fragmentation does not turn into per-PDU allocation.
func TestFramerKeepsSmallBufferAfterFragmentedPDU(t *testing.T) {
	f, _ := NewFramer(0)
	pdu := makePDU(protocol.CommandSubmitSM, 1, bytes.Repeat([]byte{1}, 4000))
	if frames := feedInChunks(t, f, pdu, 1000); len(frames) != 1 {
		t.Fatalf("emitted %d frames", len(frames))
	}
	if cap(f.pending) < len(pdu) {
		t.Fatalf("small buffer was dropped: cap=%d, pdu=%d", cap(f.pending), len(pdu))
	}
}

// The framer must keep decoding correctly after it has dropped the buffer:
// a large fragmented PDU, then a small fragmented one, then a large one again.
func TestFramerDecodesCorrectlyAfterShrink(t *testing.T) {
	f, _ := NewFramer(0)
	big1 := makePDU(protocol.CommandSubmitSM, 1, bytes.Repeat([]byte{0x11}, 300_000))
	small := makePDU(protocol.CommandEnquireLink, 2, nil)
	big2 := makePDU(protocol.CommandDeliverSM, 3, bytes.Repeat([]byte{0x22}, 200_000))
	stream := append(append(append([]byte{}, big1...), small...), big2...)
	frames := feedInChunks(t, f, stream, 9_999)
	want := [][]byte{big1, small, big2}
	if len(frames) != len(want) {
		t.Fatalf("emitted %d frames, want %d", len(frames), len(want))
	}
	for i := range want {
		if !bytes.Equal(frames[i], want[i]) {
			t.Fatalf("frame %d differs", i)
		}
	}
	if cap(f.pending) > retentionBound {
		t.Fatalf("cap=%d after stream", cap(f.pending))
	}
}

// After the buffer is dropped, the next PDU may begin with a chunk too short to
// carry its length word. The framer must start that PDU from a clean state
// (expected == 0) rather than trusting the previous PDU's length.
func TestFramerStartsCleanAfterShrinkWithShortFirstChunk(t *testing.T) {
	f, _ := NewFramer(0)
	big := makePDU(protocol.CommandSubmitSM, 1, bytes.Repeat([]byte{0x33}, 150_000))
	next := makePDU(protocol.CommandEnquireLink, 2, nil)
	if frames := feedInChunks(t, f, big, 20_000); len(frames) != 1 {
		t.Fatalf("setup: emitted %d frames", len(frames))
	}
	frames := feedInChunks(t, f, next, 2) // 2-byte chunks: never carries the length word at once
	if len(frames) != 1 || !bytes.Equal(frames[0], next) {
		t.Fatalf("emitted %d frames after shrink; content match=%v", len(frames), len(frames) == 1 && bytes.Equal(frames[0], next))
	}
}
