package session

import (
	"testing"

	"github.com/majiddarvishan/go-smpp/protocol"
)

func TestSequenceGeneratorRangeAndWrap(t *testing.T) {
	var generator sequenceGenerator
	if got := generator.Next(); got != protocol.SequenceMin {
		t.Fatalf("first sequence=%d want %d", got, protocol.SequenceMin)
	}
	generator.value.Store(uint32(protocol.SequenceOutboundMax - 1))
	if got := generator.Next(); got != protocol.SequenceOutboundMax {
		t.Fatalf("max sequence=0x%08x", uint32(got))
	}
	if got := generator.Next(); got != protocol.SequenceMin {
		t.Fatalf("wrapped sequence=0x%08x want 0x%08x", uint32(got), uint32(protocol.SequenceMin))
	}
}
