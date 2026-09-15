package codec

import (
	"testing"

	"github.com/majiddarvishan/go-smpp/protocol"
)

func BenchmarkRegistryCommandLookup(b *testing.B) {
	builder := NewRegistryBuilder(RegistryCompatible)
	if err := builder.RegisterCommand(CommandDefinition{ID: protocol.CommandSubmitSM, Name: "submit_sm"}); err != nil {
		b.Fatal(err)
	}
	registry, err := builder.Freeze()
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	var sink CommandDefinition
	for i := 0; i < b.N; i++ {
		sink, _ = registry.Command(protocol.CommandSubmitSM)
	}
	_ = sink
}

func BenchmarkRegistryTLVLookup(b *testing.B) {
	builder := NewRegistryBuilder(RegistryCompatible)
	if err := builder.RegisterTLV(TLVDefinition{Tag: 0x1400, Name: "vendor_tlv"}); err != nil {
		b.Fatal(err)
	}
	registry, err := builder.Freeze()
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	var sink TLVDefinition
	for i := 0; i < b.N; i++ {
		sink, _ = registry.TLV(0x1400)
	}
	_ = sink
}
