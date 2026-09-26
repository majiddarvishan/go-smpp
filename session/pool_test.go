package session

import (
	"sync"
	"testing"
)

// There is deliberately no test here asserting that a get() after a put()
// returns the exact same buffer. sync.Pool's own documentation is explicit:
// "callers should not assume any relationship between values passed to Put
// and the values returned by Get" — Get is free to ignore the pool and
// allocate fresh, and an item can be dropped on any GC cycle with no
// notification. A first version of this test asserted exactly that
// relationship anyway and flaked under `go test -race -count=10`, which is
// this contract being exercised, not a bug in framePool. The actual
// correctness property that matters here — that IF a buffer is reused, it
// never carries another request's still-relevant bytes — is what
// pool_content_test.go proves end to end, independent of whether reuse
// happens on any given run.

func TestFramePoolGetGrowsForHint(t *testing.T) {
	p := newFramePool()
	const hint = 1000
	ptr := p.get(hint)
	if cap(*ptr) < hint {
		t.Fatalf("cap=%d want >= %d", cap(*ptr), hint)
	}
	if len(*ptr) != 0 {
		t.Fatalf("len=%d want 0", len(*ptr))
	}
	p.put(ptr)
}

func TestFramePoolGetWithoutHintUsesDefaultCapacity(t *testing.T) {
	p := newFramePool()
	ptr := p.get(0)
	if cap(*ptr) != defaultFramePoolCapacity {
		t.Fatalf("cap=%d want the pool's default %d for a brand-new buffer", cap(*ptr), defaultFramePoolCapacity)
	}
}

// TestFramePoolDoesNotRetainOversizedBuffers reproduces the eviction path: a
// buffer larger than maxPooledFrameCapacity is never actually stored, so it
// can never come back out of a fresh pool that has seen no other buffer.
func TestFramePoolDoesNotRetainOversizedBuffers(t *testing.T) {
	p := newFramePool()
	big := p.get(maxPooledFrameCapacity + 1)
	if cap(*big) <= maxPooledFrameCapacity {
		t.Fatalf("test setup: expected an oversized buffer, got cap=%d", cap(*big))
	}
	p.put(big) // must be dropped, not pooled

	got := p.get(0)
	if cap(*got) != defaultFramePoolCapacity {
		t.Fatalf("cap=%d, want the fresh pool.New default %d — the oversized buffer must not have been retained", cap(*got), defaultFramePoolCapacity)
	}
}

func TestFramePoolPutNilIsNoop(t *testing.T) {
	p := newFramePool()
	p.put(nil) // must not panic
}

// TestFramePoolConcurrentGetPut exercises get/put from many goroutines at
// once. sync.Pool is documented safe for concurrent use, so this is a smoke
// test pinning that expectation under -race rather than a novel proof; the
// real correctness evidence for this task is the content-integrity tests in
// pool_content_test.go, which exercise the full encode -> write -> pool
// return cycle end to end.
func TestFramePoolConcurrentGetPut(t *testing.T) {
	p := newFramePool()
	var wg sync.WaitGroup
	for g := 0; g < 32; g++ {
		wg.Add(1)
		go func(seed byte) {
			defer wg.Done()
			for i := 0; i < 2000; i++ {
				ptr := p.get(64)
				*ptr = append(*ptr, seed, byte(i))
				p.put(ptr)
			}
		}(byte(g))
	}
	wg.Wait()
}
