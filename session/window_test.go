package session

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRequestWindowBlocksAndReleases(t *testing.T) {
	w := newRequestWindow(1, nil)
	done := make(chan struct{})
	if err := w.acquire(context.Background(), done, true); err != nil {
		t.Fatal(err)
	}
	if got := w.snapshot(); got.InUse != 1 || got.HighWater != 1 || got.Capacity != 1 {
		t.Fatalf("snapshot after acquire: %+v", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := w.acquire(ctx, done, true); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked acquire error = %v", err)
	}
	if err := w.acquire(context.Background(), done, false); !errors.Is(err, ErrWindowFull) {
		t.Fatalf("nonblocking acquire error = %v", err)
	}
	w.release()
	if err := w.acquire(context.Background(), done, false); err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	w.release()
	if got := w.snapshot(); got.InUse != 0 || got.Waits != 1 || got.Acquired != 2 {
		t.Fatalf("final snapshot: %+v", got)
	}
}

func TestPendingTerminalPathsReleaseWindowExactlyOnce(t *testing.T) {
	w := newRequestWindow(1, nil)
	done := make(chan struct{})
	if err := w.acquire(context.Background(), done, true); err != nil {
		t.Fatal(err)
	}
	req := &pendingRequest{done: make(chan requestResult, 1), releaseWindow: func() { _ = w.release() }}
	table := newPendingTable(1)
	if err := table.insert(1, req); err != nil {
		t.Fatal(err)
	}
	if _, won := table.completeError(1, errors.New("x")); !won {
		t.Fatal("first completion lost")
	}
	if _, won := table.completeError(1, errors.New("y")); won {
		t.Fatal("second completion won")
	}
	if got := w.snapshot().InUse; got != 0 {
		t.Fatalf("in use = %d", got)
	}
}

func TestRequestWindowUnderflowDoesNotPanic(t *testing.T) {
	w := newRequestWindow(1, nil)
	var underflows int
	w.onUnderflow = func() { underflows++ }
	done := make(chan struct{})

	if err := w.acquire(context.Background(), done, true); err != nil {
		t.Fatal(err)
	}
	if released := w.release(); !released {
		t.Fatal("matched release reported underflow")
	}
	if got := w.snapshot().Underflows; got != 0 {
		t.Fatalf("underflows after matched release = %d, want 0", got)
	}

	// The unmatched release is a library defect. It must degrade accounting,
	// not kill the host process.
	if released := w.release(); released {
		t.Fatal("unmatched release reported success")
	}
	if got := w.snapshot().Underflows; got != 1 {
		t.Fatalf("underflows = %d, want 1", got)
	}
	if underflows != 1 {
		t.Fatalf("underflow callback fired %d times, want 1", underflows)
	}

	// The window must still be usable after an underflow.
	if err := w.acquire(context.Background(), done, false); err != nil {
		t.Fatalf("acquire after underflow: %v", err)
	}
	if got := w.snapshot().InUse; got != 1 {
		t.Fatalf("in use after reacquire = %d, want 1", got)
	}
}

func TestRequestWindowUnderflowEmitsEvent(t *testing.T) {
	var events []Event
	s := &Session{}
	s.config.Observer = ObserverFunc(func(e Event) { events = append(events, e) })
	w := newRequestWindow(1, nil)
	w.onUnderflow = s.reportWindowUnderflow

	if released := w.release(); released {
		t.Fatal("unmatched release reported success")
	}
	if len(events) != 1 {
		t.Fatalf("emitted %d events, want 1", len(events))
	}
	if events[0].Kind != EventWindowUnderflow {
		t.Fatalf("event kind = %v, want %v", events[0].Kind, EventWindowUnderflow)
	}
	if events[0].At.IsZero() {
		t.Fatal("event timestamp not set")
	}
}

func BenchmarkRequestWindowAcquireRelease(b *testing.B) {
	for _, size := range []int{1, 10, 100, 1000, 5000} {
		b.Run(windowBenchmarkName(size), func(b *testing.B) {
			w := newRequestWindow(size, nil)
			done := make(chan struct{})
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := w.acquire(context.Background(), done, true); err != nil {
					b.Fatal(err)
				}
				w.release()
			}
		})
	}
}

func windowBenchmarkName(size int) string {
	if size == 1 {
		return "window_1_rtt_serial"
	}
	if size == 10 {
		return "window_10_rtt_low"
	}
	if size == 100 {
		return "window_100_rtt_1ms_100k_estimate"
	}
	if size == 1000 {
		return "window_1000_rtt_10ms_100k_estimate"
	}
	return "window_5000_rtt_50ms_100k_estimate"
}
