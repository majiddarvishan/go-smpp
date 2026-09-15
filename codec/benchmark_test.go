package codec

import (
	"testing"

	"github.com/majiddarvishan/go-smpp/protocol"
)

func BenchmarkDecodeHeader(b *testing.B) {
	frame := makePDU(protocol.CommandSubmitSM, 0xFFFFFFFF, nil)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = DecodeHeader(frame)
	}
}

func BenchmarkFramerCompletePDU(b *testing.B) {
	frame := makePDU(protocol.CommandSubmitSM, 1, make([]byte, 64))
	framer, _ := NewFramer(1024)
	emit := func([]byte) error { return nil }
	b.ReportAllocs()
	b.SetBytes(int64(len(frame)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := framer.Feed(frame, emit); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkScanTLVs(b *testing.B) {
	var buf []byte
	for i := 0; i < 8; i++ {
		buf, _ = AppendTLV(buf, uint16(0x1000+i), []byte{1, 2, 3, 4})
	}
	visit := func(TLV) bool { return true }
	b.ReportAllocs()
	b.SetBytes(int64(len(buf)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := ScanTLVs(buf, visit); err != nil {
			b.Fatal(err)
		}
	}
}
