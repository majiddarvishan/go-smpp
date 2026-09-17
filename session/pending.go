package session

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/protocol"
)

type requestResult struct {
	pdu codec.DecodedPDU
	err error
}

type pendingRequest struct {
	requestID       protocol.CommandID
	bindVersion     protocol.InterfaceVersion
	expectedID      protocol.CommandID
	done            chan requestResult
	dispatched      atomic.Bool
	dispatchedAt    atomic.Int64
	responseAt      atomic.Int64
	responseCommand atomic.Uint32
	responseStatus  atomic.Uint32
	rttRecorded     atomic.Bool
	releaseWindow   func()

	deadlineStorage deadlineItem
	deadlineMu      sync.Mutex
	deadline        *deadlineItem
	deadlines  *deadlineManager
	finished   bool
}

type pendingTable struct {
	mu  sync.Mutex
	max int
	m   map[protocol.SequenceNumber]*pendingRequest
}

func newPendingTable(max int) *pendingTable {
	return &pendingTable{max: max, m: make(map[protocol.SequenceNumber]*pendingRequest, max)}
}

func (t *pendingTable) insert(sequence protocol.SequenceNumber, request *pendingRequest) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.m) >= t.max {
		return ErrPendingLimit
	}
	if _, exists := t.m[sequence]; exists {
		return ErrSequenceInUse
	}
	t.m[sequence] = request
	return nil
}

func (t *pendingTable) exists(sequence protocol.SequenceNumber) bool {
	t.mu.Lock()
	_, ok := t.m[sequence]
	t.mu.Unlock()
	return ok
}

func (t *pendingTable) markDispatched(sequence protocol.SequenceNumber) (*pendingRequest, bool) {
	t.mu.Lock()
	request, ok := t.m[sequence]
	if ok {
		request.dispatched.Store(true)
	}
	t.mu.Unlock()
	return request, ok
}

func (t *pendingTable) takeResponse(sequence protocol.SequenceNumber, responseID protocol.CommandID) (*pendingRequest, bool) {
	t.mu.Lock()
	request, ok := t.m[sequence]
	if !ok {
		t.mu.Unlock()
		return nil, false
	}
	if responseID != request.expectedID && responseID != protocol.CommandGenericNACK {
		t.mu.Unlock()
		return nil, false
	}
	delete(t.m, sequence)
	t.mu.Unlock()
	request.finish()
	return request, true
}

func (t *pendingTable) completeError(sequence protocol.SequenceNumber, err error) (*pendingRequest, bool) {
	t.mu.Lock()
	request, ok := t.m[sequence]
	if ok {
		delete(t.m, sequence)
	}
	t.mu.Unlock()
	if !ok {
		return nil, false
	}
	request.finish()
	request.done <- requestResult{err: err}
	return request, true
}

func (t *pendingTable) failAll(err error) {
	t.mu.Lock()
	requests := make([]*pendingRequest, 0, len(t.m))
	for sequence, request := range t.m {
		delete(t.m, sequence)
		requests = append(requests, request)
	}
	t.mu.Unlock()
	for _, request := range requests {
		request.finish()
		request.done <- requestResult{err: err}
	}
}

func (t *pendingTable) len() int {
	t.mu.Lock()
	n := len(t.m)
	t.mu.Unlock()
	return n
}

func (r *pendingRequest) attachDeadline(manager *deadlineManager, item *deadlineItem) {
	if item == nil {
		return
	}
	r.deadlineMu.Lock()
	if r.finished {
		r.deadlineMu.Unlock()
		manager.cancel(item)
		return
	}
	r.deadlines = manager
	r.deadline = item
	r.deadlineMu.Unlock()
}

func (r *pendingRequest) finish() {
	r.deadlineMu.Lock()
	if r.finished {
		r.deadlineMu.Unlock()
		return
	}
	r.finished = true
	manager := r.deadlines
	item := r.deadline
	r.deadline = nil
	r.deadlines = nil
	release := r.releaseWindow
	r.releaseWindow = nil
	r.deadlineMu.Unlock()
	if manager != nil && item != nil {
		manager.cancel(item)
	}
	if release != nil {
		release()
	}
}

func (r *pendingRequest) markDispatchAt(at time.Time) (time.Duration, bool) {
	r.dispatchedAt.Store(at.UnixNano())
	return r.claimRTT()
}

func (r *pendingRequest) markResponseAt(at time.Time, command protocol.CommandID, status protocol.CommandStatus) (time.Duration, bool) {
	r.responseCommand.Store(uint32(command))
	r.responseStatus.Store(uint32(status))
	r.responseAt.Store(at.UnixNano())
	return r.claimRTT()
}

func (r *pendingRequest) claimRTT() (time.Duration, bool) {
	dispatch := r.dispatchedAt.Load()
	response := r.responseAt.Load()
	if dispatch == 0 || response == 0 || !r.rttRecorded.CompareAndSwap(false, true) {
		return 0, false
	}
	// A peer on an in-memory or extremely low-latency transport can return a
	// response after WriteFull has completed but before the TX goroutine records
	// its post-write timestamp. In that instrumentation race the true RTT is
	// smaller than our scheduling gap, so expose a zero lower bound rather than
	// dropping the sample or reporting a negative duration.
	if response <= dispatch {
		return 0, true
	}
	return time.Duration(response - dispatch), true
}

func (r *pendingRequest) responseMetadata() (protocol.CommandID, protocol.CommandStatus) {
	command := protocol.CommandID(r.responseCommand.Load())
	if command == 0 {
		command = r.expectedID
	}
	return command, protocol.CommandStatus(r.responseStatus.Load())
}
