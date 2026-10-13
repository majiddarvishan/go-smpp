package codec

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/majiddarvishan/go-smpp/protocol"
)

// FuzzFramerChunked is the differential fuzz target for the framer's state that
// lives BETWEEN Feed calls: the pending buffer, the expected length, a length word
// split across reads, and the buffer release of Task 3.4. FuzzFramer cannot reach
// any of that because it hands the framer one slice.
//
// The property: delivering a byte stream in arbitrary pieces must be
// indistinguishable from delivering it in one piece. Same frames in the same order,
// same outcome (clean, or the same fatal error), same amount left buffered, and a
// failed framer stays failed with the same error. On top of that, a framer holding
// nothing must not be holding a large buffer (only reachable above 64 KiB, so that
// part is exercised by TestFramerChunkedLargePDUs, which uses a larger maximum than
// this target: large inputs make every fuzz execution slow, and a 70 KiB seed made
// the fuzzer manage about 30 executions per second).
//
// `plan` chooses the piece sizes; `data` is the stream.
const chunkedFuzzMaxPDU = 4096 // small, so every execution is cheap and the fuzzer can explore

type framerRun struct {
	frames   [][]byte
	err      error
	buffered int
	finalize error
}

func runFramer(t *testing.T, maxPDU uint32, data []byte, sizes func(i int) int) framerRun {
	t.Helper()
	f, err := NewFramer(maxPDU)
	if err != nil {
		t.Fatal(err)
	}
	var run framerRun
	emit := func(frame []byte) error {
		if len(frame) < HeaderSize {
			t.Fatalf("emitted a %d-byte frame, shorter than a header", len(frame))
		}
		if declared := binary.BigEndian.Uint32(frame[:4]); int(declared) != len(frame) {
			t.Fatalf("declared length %d but frame is %d bytes", declared, len(frame))
		}
		run.frames = append(run.frames, append([]byte(nil), frame...))
		return nil
	}
	rest := data
	if len(rest) == 0 {
		run.err = f.Feed(nil, emit)
	}
	for i := 0; len(rest) > 0 && run.err == nil; i++ {
		n := sizes(i)
		if n <= 0 { // a zero-length read must be harmless; it still has to make progress after
			if err := f.Feed(nil, emit); err != nil {
				run.err = err
				break
			}
			n = 1
		}
		if n > len(rest) {
			n = len(rest)
		}
		run.err = f.Feed(rest[:n], emit)
		rest = rest[n:]
		// Holding nothing must mean holding no large buffer (Task 3.4).
		if run.err == nil && f.Buffered() == 0 && cap(f.pending) > framerShrinkThreshold {
			t.Fatalf("framer holds nothing but retains a %d-byte buffer", cap(f.pending))
		}
	}
	run.buffered = f.Buffered()
	if run.err != nil {
		// A failed framer is failed for good, with the same error.
		if again := f.Feed([]byte{0}, emit); again != run.err {
			t.Fatalf("framer returned %v after failing with %v", again, run.err)
		}
		if !f.Failed() {
			t.Fatal("framer reported an error but not Failed()")
		}
	} else {
		run.finalize = f.Finalize()
	}
	return run
}

// sameOutcome compares two terminal errors by what they mean, since two failed
// framers hold distinct error values.
func sameOutcome(a, b error) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	var fa, fb *protocol.FatalError
	if errors.As(a, &fa) && errors.As(b, &fb) {
		return fa.Kind == fb.Kind && fa.DeclaredLength == fb.DeclaredLength
	}
	return a.Error() == b.Error()
}

func FuzzFramerChunked(f *testing.F) {
	pdu := func(n int, seq byte) []byte {
		b := make([]byte, n)
		binary.BigEndian.PutUint32(b, uint32(n))
		binary.BigEndian.PutUint32(b[4:], 21) // enquire_link: only the framing matters here
		b[15] = seq
		for i := HeaderSize; i < n; i++ {
			b[i] = byte(i)
		}
		return b
	}
	two := append(pdu(16, 1), pdu(40, 2)...)
	f.Add(pdu(16, 1), []byte{1})                         // one PDU, a byte at a time
	f.Add(two, []byte{3, 5})                             // two PDUs, odd pieces
	f.Add(two, []byte{2})                                // the length word is always split
	f.Add(two, []byte{0, 1, 0, 9})                       // zero-length reads in the mix
	f.Add(append(pdu(16, 1), 0, 0, 0, 15), []byte{4})    // a valid PDU, then an illegal length
	f.Add(pdu(16, 1)[:11], []byte{5})                    // a truncated stream
	f.Add([]byte{0, 0, 0x20, 1, 0, 0, 0, 21}, []byte{3}) // declares more than the maximum
	f.Add([]byte{}, []byte{1})

	f.Fuzz(func(t *testing.T, data []byte, plan []byte) {
		oneShot := runFramer(t, chunkedFuzzMaxPDU, data, func(int) int { return len(data) })
		chunked := runFramer(t, chunkedFuzzMaxPDU, data, func(i int) int {
			if len(plan) == 0 {
				return len(data)
			}
			b := int(plan[i%len(plan)])
			if b%7 == 0 && i%2 == 0 {
				return 0
			}
			return 1 + b*b/4 // 1 to about 16 KiB, weighted towards small pieces
		})

		if len(oneShot.frames) != len(chunked.frames) {
			t.Fatalf("one piece emitted %d frames, pieces emitted %d", len(oneShot.frames), len(chunked.frames))
		}
		for i := range oneShot.frames {
			if !bytes.Equal(oneShot.frames[i], chunked.frames[i]) {
				t.Fatalf("frame %d differs between one piece and pieces", i)
			}
		}
		if !sameOutcome(oneShot.err, chunked.err) {
			t.Fatalf("outcome differs: one piece %v, pieces %v", oneShot.err, chunked.err)
		}
		if oneShot.err == nil {
			if oneShot.buffered != chunked.buffered {
				t.Fatalf("left buffered: one piece %d, pieces %d", oneShot.buffered, chunked.buffered)
			}
			if !sameOutcome(oneShot.finalize, chunked.finalize) {
				t.Fatalf("Finalize differs: one piece %v, pieces %v", oneShot.finalize, chunked.finalize)
			}
		}
	})
}

// The large-input half of the same property, deterministic and cheap: a PDU above
// the release threshold delivered in many shapes of pieces, alone and followed by
// small ones, must come out the same as when delivered whole, and the framer must
// not keep the big buffer once it holds nothing.
func TestFramerChunkedLargePDUs(t *testing.T) {
	const max = 1 << 20
	pdu := func(n int, seq byte) []byte {
		b := make([]byte, n)
		binary.BigEndian.PutUint32(b, uint32(n))
		binary.BigEndian.PutUint32(b[4:], 21)
		b[15] = seq
		for i := HeaderSize; i < n; i++ {
			b[i] = byte(i * 7)
		}
		return b
	}
	streams := map[string][]byte{
		"70 KiB":                    pdu(70*1024, 1),
		"1 MiB":                     pdu(max, 2),
		"big then small":            append(pdu(200*1024, 3), pdu(16, 4)...),
		"small, big, small":         append(append(pdu(40, 5), pdu(90*1024, 6)...), pdu(16, 7)...),
		"two big":                   append(pdu(80*1024, 8), pdu(100*1024, 9)...),
		"big with a truncated tail": append(pdu(70*1024, 10), pdu(50, 11)[:23]...),
	}
	plans := map[string]func(int) int{
		"1 byte":       func(int) int { return 1 },
		"2 bytes":      func(int) int { return 2 },
		"3 bytes":      func(int) int { return 3 },
		"4096":         func(int) int { return 4096 },
		"64 KiB reads": func(int) int { return 64 << 10 },
		"uneven":       func(i int) int { return []int{5, 1, 13000, 2, 70000, 7}[i%6] },
	}
	for name, data := range streams {
		whole := runFramer(t, max, data, func(int) int { return len(data) })
		for pname, plan := range plans {
			if len(data) > 1<<20 && pname == "1 byte" {
				continue // a million one-byte Feeds adds time and proves nothing the 2-byte plan does not
			}
			got := runFramer(t, max, data, plan)
			if len(got.frames) != len(whole.frames) {
				t.Fatalf("%s / %s: %d frames, want %d", name, pname, len(got.frames), len(whole.frames))
			}
			for i := range whole.frames {
				if !bytes.Equal(got.frames[i], whole.frames[i]) {
					t.Fatalf("%s / %s: frame %d differs", name, pname, i)
				}
			}
			if !sameOutcome(got.err, whole.err) || got.buffered != whole.buffered || !sameOutcome(got.finalize, whole.finalize) {
				t.Fatalf("%s / %s: outcome differs from delivering it whole", name, pname)
			}
		}
	}
}
