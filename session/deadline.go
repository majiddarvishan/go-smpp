package session

import (
	"container/heap"
	"sync"
	"time"

	"github.com/majiddarvishan/go-smpp/protocol"
)

type deadlineItem struct {
	at       time.Time
	after    time.Duration
	kind     TimeoutKind
	command  protocol.CommandID
	sequence protocol.SequenceNumber
	index    int
}

type deadlineHeap []*deadlineItem

func (h deadlineHeap) Len() int           { return len(h) }
func (h deadlineHeap) Less(i, j int) bool { return h[i].at.Before(h[j].at) }
func (h deadlineHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}
func (h *deadlineHeap) Push(x any) {
	item := x.(*deadlineItem)
	item.index = len(*h)
	*h = append(*h, item)
}
func (h *deadlineHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*h = old[:n-1]
	return item
}

type deadlineManager struct {
	mu     sync.Mutex
	heap   deadlineHeap
	wake   chan struct{}
	done   <-chan struct{}
	expire func(*deadlineItem)
}

func newDeadlineManager(done <-chan struct{}, expire func(*deadlineItem)) *deadlineManager {
	return &deadlineManager{wake: make(chan struct{}, 1), done: done, expire: expire}
}

func (m *deadlineManager) schedule(after time.Duration, kind TimeoutKind, command protocol.CommandID, sequence protocol.SequenceNumber) *deadlineItem {
	return m.scheduleItem(&deadlineItem{}, after, kind, command, sequence)
}

// scheduleItem inserts a caller-owned deadline record. The session hot path
// embeds this record in pendingRequest so ordinary request scheduling does not
// require a separate heap allocation per outstanding deadline.
func (m *deadlineManager) scheduleItem(item *deadlineItem, after time.Duration, kind TimeoutKind, command protocol.CommandID, sequence protocol.SequenceNumber) *deadlineItem {
	if after <= 0 || item == nil {
		return nil
	}
	*item = deadlineItem{at: time.Now().Add(after), after: after, kind: kind, command: command, sequence: sequence, index: -1}
	m.mu.Lock()
	wasFirst := len(m.heap) == 0 || item.at.Before(m.heap[0].at)
	heap.Push(&m.heap, item)
	m.mu.Unlock()
	if wasFirst {
		m.signal()
	}
	return item
}

func (m *deadlineManager) cancel(item *deadlineItem) {
	if item == nil {
		return
	}
	m.mu.Lock()
	idx := item.index
	wasFirst := idx == 0
	if idx >= 0 && idx < len(m.heap) && m.heap[idx] == item {
		heap.Remove(&m.heap, idx)
	}
	m.mu.Unlock()
	if wasFirst {
		m.signal()
	}
}

func (m *deadlineManager) len() int {
	m.mu.Lock()
	n := len(m.heap)
	m.mu.Unlock()
	return n
}

func (m *deadlineManager) signal() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *deadlineManager) run() {
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()

	for {
		m.mu.Lock()
		if len(m.heap) == 0 {
			m.mu.Unlock()
			select {
			case <-m.wake:
				continue
			case <-m.done:
				return
			}
		}
		wait := time.Until(m.heap[0].at)
		m.mu.Unlock()
		if wait < 0 {
			wait = 0
		}
		timer.Reset(wait)
		select {
		case <-timer.C:
			m.expireReady(time.Now())
		case <-m.wake:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-m.done:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		}
	}
}

func (m *deadlineManager) expireReady(now time.Time) {
	var expired []*deadlineItem
	m.mu.Lock()
	for len(m.heap) > 0 && !m.heap[0].at.After(now) {
		expired = append(expired, heap.Pop(&m.heap).(*deadlineItem))
	}
	m.mu.Unlock()
	for _, item := range expired {
		if m.expire != nil {
			m.expire(item)
		}
	}
}
