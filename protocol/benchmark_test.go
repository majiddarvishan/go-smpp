package protocol

import "testing"

func BenchmarkPutUint32(b *testing.B) {
	var buf [4]byte
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		PutUint32(buf[:], uint32(i))
	}
}

func BenchmarkReadUint32(b *testing.B) {
	buf := []byte{0x01, 0x02, 0x03, 0x04}
	b.ReportAllocs()
	var sink uint32
	for i := 0; i < b.N; i++ {
		sink, _ = ReadUint32(buf)
	}
	_ = sink
}

func BenchmarkCommandResponseID(b *testing.B) {
	b.ReportAllocs()
	var sink CommandID
	for i := 0; i < b.N; i++ {
		sink = CommandSubmitSM.ResponseID()
	}
	_ = sink
}
