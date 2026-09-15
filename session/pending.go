package session

import (
	"sync"
	"sync/atomic"

	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/protocol"
)

type requestResult struct {
	pdu codec.DecodedPDU
	err error
}

type pendingRequest struct {
	requestID  protocol.CommandID
	expectedID protocol.CommandID
	done       chan requestResult
	dispatched atomic.Bool
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

func (t *pendingTable) markDispatched(sequence protocol.SequenceNumber) bool {
	t.mu.Lock()
	request, ok := t.m[sequence]
	if ok {
		request.dispatched.Store(true)
	}
	t.mu.Unlock()
	return ok
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
		request.done <- requestResult{err: err}
	}
}

func (t *pendingTable) len() int {
	t.mu.Lock()
	n := len(t.m)
	t.mu.Unlock()
	return n
}
