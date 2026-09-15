package codec

import (
	"errors"
	"testing"

	"github.com/majiddarvishan/go-smpp/protocol"
)

func TestHeaderRoundTripAcceptsHighInboundSequence(t *testing.T) {
	h := Header{CommandLength: HeaderSize, CommandID: protocol.CommandSubmitSM, SequenceNumber: 0xFFFFFFFF}
	var buf [HeaderSize]byte
	if err := EncodeHeader(buf[:], h); err != nil {
		t.Fatal(err)
	}
	got, err := DecodeHeader(buf[:])
	if err != nil {
		t.Fatal(err)
	}
	if got != h {
		t.Fatalf("got %+v want %+v", got, h)
	}
}

func TestFramerFragmentedAndCoalesced(t *testing.T) {
	p1 := makePDU(protocol.CommandSubmitSM, 1, []byte{1, 2, 3})
	p2 := makePDU(protocol.CommandDeliverSM, 2, []byte{4, 5})
	stream := append(append([]byte{}, p1...), p2...)
	f, err := NewFramer(1024)
	if err != nil {
		t.Fatal(err)
	}
	var got [][]byte
	emit := func(frame []byte) error {
		got = append(got, append([]byte(nil), frame...))
		return nil
	}
	for _, cut := range []int{1, 2, 7, 3, 11, 5, 100} {
		if len(stream) == 0 {
			break
		}
		if cut > len(stream) {
			cut = len(stream)
		}
		if err := f.Feed(stream[:cut], emit); err != nil {
			t.Fatal(err)
		}
		stream = stream[cut:]
	}
	if len(got) != 2 {
		t.Fatalf("got %d frames", len(got))
	}
	if f.Buffered() != 0 {
		t.Fatalf("buffered=%d", f.Buffered())
	}
}

func TestFramerFatalLengthPoisonsStreamAndDoesNotResync(t *testing.T) {
	bad := make([]byte, HeaderSize)
	bad[3] = 15
	good := makePDU(protocol.CommandSubmitSM, 7, nil)
	input := append(bad, good...)

	f, _ := NewFramer(1024)
	count := 0
	err := f.Feed(input, func([]byte) error { count++; return nil })
	var fatal *protocol.FatalError
	if !errors.As(err, &fatal) || fatal.Kind != protocol.FatalInvalidCommandLength {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 || !f.Failed() {
		t.Fatalf("count=%d failed=%v", count, f.Failed())
	}
	if err := f.Feed(good, func([]byte) error { count++; return nil }); err == nil {
		t.Fatal("failed framer accepted later bytes")
	}
	if count != 0 {
		t.Fatal("framer resynchronized after fatal length")
	}
}

func TestFramerBodyFatalStopsFollowingFrames(t *testing.T) {
	p1 := makePDU(protocol.CommandSubmitSM, 1, nil)
	p2 := makePDU(protocol.CommandDeliverSM, 2, nil)
	input := append(p1, p2...)
	f, _ := NewFramer(1024)
	count := 0
	err := f.Feed(input, func([]byte) error {
		count++
		return &protocol.FatalError{Kind: protocol.FatalInvalidTLVLength, Reason: "bad TLV"}
	})
	if err == nil || count != 1 || !f.Failed() {
		t.Fatalf("err=%v count=%d failed=%v", err, count, f.Failed())
	}
}

func TestFramerRejectsOversizedPDU(t *testing.T) {
	frame := makePDU(protocol.CommandSubmitSM, 1, make([]byte, 64))
	f, _ := NewFramer(32)
	err := f.Feed(frame, nil)
	var fatal *protocol.FatalError
	if !errors.As(err, &fatal) || fatal.Kind != protocol.FatalPDUTooLarge {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFramerFinalizePartial(t *testing.T) {
	f, _ := NewFramer(1024)
	if err := f.Feed([]byte{0, 0, 0}, nil); err != nil {
		t.Fatal(err)
	}
	err := f.Finalize()
	var fatal *protocol.FatalError
	if !errors.As(err, &fatal) || fatal.Kind != protocol.FatalTruncatedFrame {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTLVPreservesOrderAndDuplicates(t *testing.T) {
	var buf []byte
	buf, _ = AppendTLV(buf, 0x1400, []byte{1})
	buf, _ = AppendTLV(buf, 0x1400, []byte{2})
	buf, _ = AppendTLV(buf, 0x001E, []byte{3, 4})
	tlvs, err := ParseTLVs(buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(tlvs) != 3 || tlvs[0].Tag != 0x1400 || tlvs[1].Tag != 0x1400 || tlvs[0].Value[0] != 1 || tlvs[1].Value[0] != 2 {
		t.Fatalf("unexpected TLVs: %#v", tlvs)
	}
}

func TestInvalidTLVLengthIsFatal(t *testing.T) {
	_, err := ParseTLVs([]byte{0, 1, 0, 5, 1})
	var fatal *protocol.FatalError
	if !errors.As(err, &fatal) || fatal.Kind != protocol.FatalInvalidTLVLength {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCStringBoundary(t *testing.T) {
	value, consumed, err := ReadCString([]byte{'a', 'b', 0, 'x'}, 3)
	if err != nil || string(value) != "ab" || consumed != 3 {
		t.Fatalf("value=%q consumed=%d err=%v", value, consumed, err)
	}
	_, _, err = ReadCString([]byte{'a', 'b', 0}, 2)
	var fatal *protocol.FatalError
	if !errors.As(err, &fatal) || fatal.Kind != protocol.FatalInvalidCString {
		t.Fatalf("unexpected error: %v", err)
	}
}

func makePDU(command protocol.CommandID, sequence uint32, body []byte) []byte {
	buf := make([]byte, HeaderSize+len(body))
	_ = EncodeHeader(buf, Header{
		CommandLength:  uint32(len(buf)),
		CommandID:      command,
		SequenceNumber: protocol.SequenceNumber(sequence),
	})
	copy(buf[HeaderSize:], body)
	return buf
}
