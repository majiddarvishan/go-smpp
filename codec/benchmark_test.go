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

func BenchmarkEncodeSubmitSM(b *testing.B) {
	registry, err := NewSMPP34Registry(RegistryCompatible)
	if err != nil {
		b.Fatal(err)
	}
	body := protocol.SubmitSM{
		SourceAddrTON:   protocol.TONInternational,
		SourceAddrNPI:   protocol.NPIISDN,
		SourceAddr:      []byte("12025550100"),
		DestAddrTON:     protocol.TONInternational,
		DestAddrNPI:     protocol.NPIISDN,
		DestinationAddr: []byte("12025550101"),
		DataCoding:      protocol.DataCodingSMSCDefault,
		ShortMessage:    []byte("phase16-codec-submit"),
	}
	header := Header{CommandID: protocol.CommandSubmitSM, SequenceNumber: 1}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := EncodePDU(nil, header, body, registry); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeSubmitSM(b *testing.B) {
	registry, err := NewSMPP34Registry(RegistryCompatible)
	if err != nil {
		b.Fatal(err)
	}
	frame, err := EncodePDU(nil, Header{CommandID: protocol.CommandSubmitSM, SequenceNumber: 1}, protocol.SubmitSM{
		SourceAddrTON:   protocol.TONInternational,
		SourceAddrNPI:   protocol.NPIISDN,
		SourceAddr:      []byte("12025550100"),
		DestAddrTON:     protocol.TONInternational,
		DestAddrNPI:     protocol.NPIISDN,
		DestinationAddr: []byte("12025550101"),
		DataCoding:      protocol.DataCodingSMSCDefault,
		ShortMessage:    []byte("phase16-codec-submit"),
	}, registry)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(frame)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := DecodePDU(frame, registry); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncodeDeliverSM(b *testing.B) {
	registry, err := NewSMPP34Registry(RegistryCompatible)
	if err != nil {
		b.Fatal(err)
	}
	body := protocol.DeliverSM{
		SourceAddrTON:   protocol.TONInternational,
		SourceAddrNPI:   protocol.NPIISDN,
		SourceAddr:      []byte("12025550101"),
		DestAddrTON:     protocol.TONInternational,
		DestAddrNPI:     protocol.NPIISDN,
		DestinationAddr: []byte("12025550100"),
		DataCoding:      protocol.DataCodingSMSCDefault,
		ShortMessage:    []byte("phase16-codec-deliver"),
	}
	header := Header{CommandID: protocol.CommandDeliverSM, SequenceNumber: 1}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := EncodePDU(nil, header, body, registry); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodeDeliverSM(b *testing.B) {
	registry, err := NewSMPP34Registry(RegistryCompatible)
	if err != nil {
		b.Fatal(err)
	}
	frame, err := EncodePDU(nil, Header{CommandID: protocol.CommandDeliverSM, SequenceNumber: 1}, protocol.DeliverSM{
		SourceAddrTON:   protocol.TONInternational,
		SourceAddrNPI:   protocol.NPIISDN,
		SourceAddr:      []byte("12025550101"),
		DestAddrTON:     protocol.TONInternational,
		DestAddrNPI:     protocol.NPIISDN,
		DestinationAddr: []byte("12025550100"),
		DataCoding:      protocol.DataCodingSMSCDefault,
		ShortMessage:    []byte("phase16-codec-deliver"),
	}, registry)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(frame)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := DecodePDU(frame, registry); err != nil {
			b.Fatal(err)
		}
	}
}
