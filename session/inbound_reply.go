package session

import (
	"errors"

	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/protocol"
)

// ResponseAction controls the response to an inbound deliver_sm.
//
// ResponseImmediate (the zero value) preserves the historic behavior.
// ResponseSuppress deliberately emits no deliver_sm_resp and creates no timer
// or deferred entry. ResponseDeferred reserves a bounded one-shot reply slot;
// the application must later call RespondDeferred or AbandonDeferredResponse.
// All other inbound commands retain their mandatory automatic response.
type ResponseAction uint8

const (
	ResponseImmediate ResponseAction = iota
	ResponseSuppress
	ResponseDeferred
)

const DefaultMaxDeferredResponses = 1024

var (
	ErrDeferredResponseLimit   = errors.New("smpp session: deferred response capacity exceeded")
	ErrDeferredResponseUnknown = errors.New("smpp session: no deferred response for request")
	ErrDeferredResponseBusy    = errors.New("smpp session: deferred response already being sent")
	ErrInvalidResponseAction   = errors.New("smpp session: response action not supported for request")
)

// registerDeferredReply is called by the RX loop after the application handler
// returns ResponseDeferred. The map is session-local and intentionally does
// not allocate for immediate or suppressed acknowledgments.
func (s *Session) registerDeferredReply(request codec.Header) error {
	s.deferredMu.Lock()
	defer s.deferredMu.Unlock()
	if s.deferredReplies == nil {
		s.deferredReplies = make(map[protocol.SequenceNumber]bool)
	}
	if _, exists := s.deferredReplies[request.SequenceNumber]; exists {
		return ErrDeferredResponseBusy
	}
	if len(s.deferredReplies) >= s.config.MaxDeferredResponses {
		return ErrDeferredResponseLimit
	}
	s.deferredReplies[request.SequenceNumber] = false
	return nil
}

// PendingDeferredResponses reports how many inbound deliver_sm replies remain
// reserved, including replies currently waiting for TX queue admission.
func (s *Session) PendingDeferredResponses() int {
	if !s.initialized() {
		return 0
	}
	s.deferredMu.Lock()
	n := len(s.deferredReplies)
	s.deferredMu.Unlock()
	return n
}

// RespondDeferred emits one deferred deliver_sm_resp with the exact sequence
// number of the original inbound request. The caller must retain only the
// value-copy of pdu.Header: inbound body buffers cannot be retained after
// Handler returns. RespondDeferred is safe for concurrent callers.
//
// This API never creates an application timer or goroutine. A caller can use
// one bounded application scheduler for delayed replies; choosing
// ResponseSuppress instead requires no scheduling at all.
//
// The response is admitted to the session TX queue before the reply slot is
// removed. If encoding/queue admission fails while the session is alive,
// callers can correct the reply and retry. A lost/closed session never causes
// an automatic replay on its replacement.
func (s *Session) RespondDeferred(request codec.Header, response Response) error {
	if !s.initialized() {
		return ErrNotInitialized
	}
	if request.CommandID != protocol.CommandDeliverSM || !request.SequenceNumber.ValidInbound() || response.Action != ResponseImmediate {
		return ErrInvalidResponseAction
	}
	select {
	case <-s.done:
		return s.requestCloseError()
	default:
	}
	s.deferredMu.Lock()
	busy, exists := s.deferredReplies[request.SequenceNumber]
	if !exists {
		s.deferredMu.Unlock()
		return ErrDeferredResponseUnknown
	}
	if busy {
		s.deferredMu.Unlock()
		return ErrDeferredResponseBusy
	}
	s.deferredReplies[request.SequenceNumber] = true
	s.deferredMu.Unlock()

	err := s.queueResponse(request, protocol.CommandDeliverSMResp, response.Status, response.Body)
	s.deferredMu.Lock()
	if err == nil {
		delete(s.deferredReplies, request.SequenceNumber)
	} else if _, exists := s.deferredReplies[request.SequenceNumber]; exists {
		s.deferredReplies[request.SequenceNumber] = false
	}
	s.deferredMu.Unlock()
	return err
}

// AbandonDeferredResponse discards an unsent reserved deliver_sm_resp. It is
// intended for controlled cancellation; a suppressed response should instead
// use ResponseSuppress directly. It returns false for unknown or already
// sending requests, and never writes an SMPP PDU.
func (s *Session) AbandonDeferredResponse(request codec.Header) bool {
	if !s.initialized() || request.CommandID != protocol.CommandDeliverSM || !request.SequenceNumber.ValidInbound() {
		return false
	}
	s.deferredMu.Lock()
	defer s.deferredMu.Unlock()
	busy, exists := s.deferredReplies[request.SequenceNumber]
	if !exists || busy {
		return false
	}
	delete(s.deferredReplies, request.SequenceNumber)
	return true
}
