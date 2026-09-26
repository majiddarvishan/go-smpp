package session

import "sync"

// defaultFramePoolCapacity seeds a brand-new pooled buffer at a size that
// covers a typical SMPP header plus a modest short message without an
// immediate regrowth on its first use, while staying small enough that an
// idle or lightly used session isn't holding onto an oversized buffer for no
// reason.
const defaultFramePoolCapacity = 256

// maxPooledFrameCapacity bounds how large a buffer this pool will retain.
// EncodePDU/the framer already cap a single PDU at Config.MaxPDUSize (1 MiB
// by default), and a session is free to encode a PDU that large — but
// permanently retaining a MiB-sized buffer in the pool after one outlier PDU
// would trade the allocation this pool exists to remove for a standing
// per-session memory floor instead. A buffer larger than this is simply left
// for the garbage collector on put; get falls back to allocating fresh (or
// growing a smaller pooled buffer) rather than ever holding one this large.
const maxPooledFrameCapacity = 64 * 1024

// framePool is a per-session pool of reusable outbound frame ([]byte)
// buffers, used by request(), SendOneWay(), and the response path (Finding
// P1). See docs/CONCURRENCY.md, "Outbound frame pool lifetime", for the
// write-completion boundary that makes returning a buffer here safe.
//
// Buffers are pooled as *[]byte rather than []byte. sync.Pool's Get/Put take
// and return `any`, and converting a []byte value (a 3-word slice header) to
// `any` requires the runtime to box it — an allocation on every single Put,
// which would quietly reintroduce exactly the steady-state allocation this
// pool exists to remove. A *[]byte is pointer-shaped, so boxing it into `any`
// stores the existing pointer directly with no extra allocation. get and put
// both operate on that same *[]byte throughout a request's lifetime: get
// hands out a pointer, the caller encodes into (and may grow) the slice it
// points to, and put stores the same pointer back — never a fresh
// &localVariable taken at Put time, which would defeat the same purpose by
// allocating a new slice header to hold the address of on every call.
type framePool struct {
	pool sync.Pool
}

func newFramePool() *framePool {
	return &framePool{
		pool: sync.Pool{
			New: func() any {
				buf := make([]byte, 0, defaultFramePoolCapacity)
				return &buf
			},
		},
	}
}

// get returns a pointer to a zero-length buffer with at least hint bytes of
// capacity (hint <= 0 is treated as "no estimate available" and is not used
// to grow the buffer). The caller encodes into *result — EncodePDU grows it
// further on its own if hint undershoots the actual size — and must pass the
// same pointer to put once done, whether or not the frame was actually sent.
//
// Per sync.Pool's own documented contract, get() may return a brand-new
// buffer even when a previously put() one was available — nothing here
// should be written to assume otherwise, in this package or in a test of it.
func (p *framePool) get(hint int) *[]byte {
	ptr := p.pool.Get().(*[]byte)
	buf := (*ptr)[:0]
	if hint > cap(buf) {
		buf = make([]byte, 0, hint)
	}
	*ptr = buf
	return ptr
}

// put returns ptr's buffer to the pool for a future get to reuse. The caller
// must not read or write *ptr, or any slice EncodePDU returned when encoding
// into it, after calling put — see the write-completion boundary in
// docs/CONCURRENCY.md. put(nil) is a safe no-op, so callers do not need to
// track whether a given txItem's frame actually came from this pool.
func (p *framePool) put(ptr *[]byte) {
	if ptr == nil {
		return
	}
	if cap(*ptr) > maxPooledFrameCapacity {
		return
	}
	*ptr = (*ptr)[:0]
	p.pool.Put(ptr)
}
