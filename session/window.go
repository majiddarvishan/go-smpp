package session

import (
	"context"
	"sync/atomic"
)

const DefaultWindowSize = 1024

// WindowSnapshot is a lock-free operational snapshot of the outbound request
// window. HighWater is the highest observed number of simultaneously acquired
// request slots since the session was created.
type WindowSnapshot struct {
	Capacity  uint32
	InUse     uint32
	HighWater uint32
	Acquired  uint64
	Waits     uint64
}

// WindowObserver is called after successful acquire/release operations when
// configured. It must return quickly and must not call back into a blocking
// session operation.
type WindowObserver func(WindowSnapshot)

type requestWindow struct {
	tokens   chan struct{}
	capacity uint32
	inUse    atomic.Uint32
	high     atomic.Uint32
	acquired atomic.Uint64
	waits    atomic.Uint64
	observer WindowObserver
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

func (w *requestWindow) release() {
	select {
	case <-w.tokens:
		w.inUse.Add(^uint32(0)) // subtract one without a signed atomic
		w.notify()
	default:
		panic("smpp session: outbound window released without acquisition")
	}
}

func (w *requestWindow) snapshot() WindowSnapshot {
	return WindowSnapshot{
		Capacity:  w.capacity,
		InUse:     w.inUse.Load(),
		HighWater: w.high.Load(),
		Acquired:  w.acquired.Load(),
		Waits:     w.waits.Load(),
	}
}

func (w *requestWindow) notify() {
	if w.observer != nil {
		w.observer(w.snapshot())
	}
}
