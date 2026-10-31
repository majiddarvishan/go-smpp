package session

import (
	"context"
	"runtime/debug"
	"sync/atomic"

	"github.com/majiddarvishan/go-smpp/protocol"
)

const DefaultWindowSize = 1024

// WindowSnapshot is a lock-free operational snapshot of the outbound request
// window. HighWater is the highest observed number of simultaneously acquired
// request slots since the session was created.
//
// Underflows counts release calls that found no token to return. A non-zero
// value is always a library defect, never peer-induced: it means some terminal
// path released a slot it did not own. It is surfaced as a counter rather than
// a panic so that a bug here degrades window accounting instead of killing the
// host process.
type WindowSnapshot struct {
	Capacity   uint32
	InUse      uint32
	HighWater  uint32
	Acquired   uint64
	Waits      uint64
	Underflows uint64
}

// WindowObserver is called after successful acquire/release operations when
// configured. It must return quickly and must not call back into a blocking
// session operation. A panic in it is recovered and drops only that one
// snapshot.
type WindowObserver func(WindowSnapshot)

type requestWindow struct {
	tokens      chan struct{}
	capacity    uint32
	inUse       atomic.Uint32
	high        atomic.Uint32
	acquired    atomic.Uint64
	waits       atomic.Uint64
	underflows  atomic.Uint64
	observer    WindowObserver
	onUnderflow func()
	onPanic     func(*protocol.PanicError)
}

func newRequestWindow(size int, observer WindowObserver) *requestWindow {
	return &requestWindow{tokens: make(chan struct{}, size), capacity: uint32(size), observer: observer}
}

func (w *requestWindow) acquire(ctx context.Context, sessionDone <-chan struct{}, wait bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case w.tokens <- struct{}{}:
		w.afterAcquire()
		return nil
	default:
	}
	if !wait {
		return ErrWindowFull
	}
	w.waits.Add(1)
	select {
	case w.tokens <- struct{}{}:
		w.afterAcquire()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-sessionDone:
		return ErrSessionClosed
	}
}

func (w *requestWindow) afterAcquire() {
	inUse := w.inUse.Add(1)
	w.acquired.Add(1)
	for {
		old := w.high.Load()
		if inUse <= old || w.high.CompareAndSwap(old, inUse) {
			break
		}
	}
	w.notify()
}

// release returns one slot to the window. It reports whether a slot was
// actually returned.
//
// A false return means release was called without a matching acquire. That is
// a library defect, but a library must not panic the host process over its own
// accounting error: an SMSC carrying live traffic would lose every other
// session on the same binary. The underflow is counted, surfaced through the
// window snapshot, and reported to the session for an observer event.
func (w *requestWindow) release() bool {
	select {
	case <-w.tokens:
		w.inUse.Add(^uint32(0)) // subtract one without a signed atomic
		w.notify()
		return true
	default:
		w.underflows.Add(1)
		if w.onUnderflow != nil {
			w.onUnderflow()
		}
		w.notify()
		return false
	}
}

func (w *requestWindow) snapshot() WindowSnapshot {
	return WindowSnapshot{
		Capacity:   w.capacity,
		InUse:      w.inUse.Load(),
		HighWater:  w.high.Load(),
		Acquired:   w.acquired.Load(),
		Waits:      w.waits.Load(),
		Underflows: w.underflows.Load(),
	}
}

func (w *requestWindow) notify() {
	if w.observer != nil {
		w.callObserver()
	}
}

// callObserver runs the observer, which is caller-supplied and runs on
// whichever goroutine acquired or released the slot, including the session's
// own. A panic in it costs that one snapshot, never the window's accounting.
func (w *requestWindow) callObserver() {
	defer func() {
		if r := recover(); r != nil && w.onPanic != nil {
			w.onPanic(&protocol.PanicError{Source: sourceWindowObserver, Value: r, Stack: debug.Stack()})
		}
	}()
	w.observer(w.snapshot())
}
