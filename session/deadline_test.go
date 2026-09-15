package session

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/majiddarvishan/go-smpp/protocol"
)

func TestDeadlineManagerCancelAndExpire(t *testing.T) {
	done := make(chan struct{})
	var expired atomic.Int64
	manager := newDeadlineManager(done, func(*deadlineItem) { expired.Add(1) })
	go manager.run()
	cancelled := manager.schedule(25*time.Millisecond, TimeoutResponse, protocol.CommandSubmitSM, 1)
	manager.cancel(cancelled)
	manager.schedule(15*time.Millisecond, TimeoutResponse, protocol.CommandSubmitSM, 2)
	time.Sleep(60 * time.Millisecond)
	close(done)
	if got := expired.Load(); got != 1 {
		t.Fatalf("expired=%d want 1", got)
	}
	if got := manager.len(); got != 0 {
		t.Fatalf("deadline entries=%d want 0", got)
	}
}

func TestDeadlineTimeoutStormIsDrained(t *testing.T) {
	done := make(chan struct{})
	const count = 2000
	var expired atomic.Int64
	manager := newDeadlineManager(done, func(*deadlineItem) { expired.Add(1) })
	go manager.run()
	for i := 0; i < count; i++ {
		manager.schedule(10*time.Millisecond, TimeoutResponse, protocol.CommandSubmitSM, protocol.SequenceNumber(i+1))
	}
	deadline := time.Now().Add(time.Second)
	for expired.Load() != count && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	close(done)
	if got := expired.Load(); got != count {
		t.Fatalf("expired=%d want %d", got, count)
	}
	if got := manager.len(); got != 0 {
		t.Fatalf("deadline entries=%d want 0", got)
	}
}

func TestDeadlineCancellationKeepsHeapBounded(t *testing.T) {
	done := make(chan struct{})
	manager := newDeadlineManager(done, nil)
	items := make([]*deadlineItem, 5000)
	for i := range items {
		items[i] = manager.schedule(time.Minute, TimeoutResponse, protocol.CommandSubmitSM, protocol.SequenceNumber(i+1))
	}
	if got := manager.len(); got != len(items) {
		t.Fatalf("deadline entries=%d want %d", got, len(items))
	}
	for _, item := range items {
		manager.cancel(item)
	}
	if got := manager.len(); got != 0 {
		t.Fatalf("cancelled deadline entries retained=%d", got)
	}
}

func BenchmarkDeadlineHeapScheduleCancel(b *testing.B) {
	done := make(chan struct{})
	manager := newDeadlineManager(done, nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		item := manager.schedule(time.Minute, TimeoutResponse, protocol.CommandSubmitSM, protocol.SequenceNumber(i+1))
		manager.cancel(item)
	}
}

func BenchmarkDeadlineTimeoutStorm1000(b *testing.B) {
	for i := 0; i < b.N; i++ {
		done := make(chan struct{})
		manager := newDeadlineManager(done, nil)
		now := time.Now()
		for j := 0; j < 1000; j++ {
			item := &deadlineItem{at: now, index: -1}
			manager.mu.Lock()
			heapPushForBenchmark(&manager.heap, item)
			manager.mu.Unlock()
		}
		manager.expireReady(now)
	}
}

func heapPushForBenchmark(h *deadlineHeap, item *deadlineItem) {
	// All benchmark deadlines are identical; direct append isolates batch-expiry
	// bookkeeping from the insertion benchmark above.
	item.index = len(*h)
	*h = append(*h, item)
}
