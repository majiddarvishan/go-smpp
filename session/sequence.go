package session

import (
	"sync/atomic"

	"github.com/majiddarvishan/go-smpp/protocol"
)

type sequenceGenerator struct {
	value atomic.Uint32
}

func (g *sequenceGenerator) Next() protocol.SequenceNumber {
	for {
		current := g.value.Load()
		next := current + 1
		if current >= uint32(protocol.SequenceOutboundMax) || next == 0 {
			next = uint32(protocol.SequenceMin)
		}
		if g.value.CompareAndSwap(current, next) {
			return protocol.SequenceNumber(next)
		}
	}
}
