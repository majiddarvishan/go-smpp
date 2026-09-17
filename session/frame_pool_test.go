package session

import (
	"testing"
)

func TestTXFramePoolBoundsRetainedCapacity(t *testing.T) {
	pool := newTXFramePool()
	buffer := pool.get()
	buffer.buf = make([]byte, 1, maxPooledTXFrameCapacity+1)
	pool.put(buffer)

	reused := pool.get()
	if cap(reused.buf) < defaultTXFrameCapacity {
		t.Fatalf("reused capacity=%d want at least %d", cap(reused.buf), defaultTXFrameCapacity)
	}
	if cap(reused.buf) > maxPooledTXFrameCapacity {
		t.Fatalf("oversized capacity retained: %d", cap(reused.buf))
	}
}
