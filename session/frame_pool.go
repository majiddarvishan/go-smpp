package session

import (
	"sync"

	"github.com/majiddarvishan/go-smpp/codec"
)

const (
	defaultTXFrameCapacity   = 512
	maxPooledTXFrameCapacity = 64 << 10
)

type txFrameBuffer struct {
	buf []byte
}

type txFramePool struct {
	pool sync.Pool
}

func newTXFramePool() *txFramePool {
	p := &txFramePool{}
	p.pool.New = func() any {
		return &txFrameBuffer{buf: make([]byte, 0, defaultTXFrameCapacity)}
	}
	return p
}

func (p *txFramePool) get() *txFrameBuffer {
	buffer := p.pool.Get().(*txFrameBuffer)
	if cap(buffer.buf) < defaultTXFrameCapacity {
		buffer.buf = make([]byte, 0, defaultTXFrameCapacity)
	} else {
		buffer.buf = buffer.buf[:0]
	}
	return buffer
}

func (p *txFramePool) put(buffer *txFrameBuffer) {
	if buffer == nil {
		return
	}
	if cap(buffer.buf) > maxPooledTXFrameCapacity {
		// Avoid a single unusually large message_payload permanently inflating
		// the session pool. The small default backing array will be recreated if
		// this pool entry is reused later.
		buffer.buf = nil
	} else {
		buffer.buf = buffer.buf[:0]
	}
	p.pool.Put(buffer)
}

func (s *Session) encodeTXPDU(header codec.Header, body any) ([]byte, *txFrameBuffer, error) {
	buffer := s.frames.get()
	frame, err := codec.EncodePDU(buffer.buf[:0], header, body, s.registry)
	if err != nil {
		buffer.buf = frame
		s.frames.put(buffer)
		return nil, nil, err
	}
	buffer.buf = frame
	return frame, buffer, nil
}

func (s *Session) releaseTXBuffer(buffer *txFrameBuffer) {
	if buffer != nil {
		s.frames.put(buffer)
	}
}

func (s *Session) releaseTXItem(item *txItem) {
	if item == nil || item.buffer == nil {
		return
	}
	s.frames.put(item.buffer)
	item.buffer = nil
	item.frame = nil
}
