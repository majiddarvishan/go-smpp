package session

import (
	"net"
	"time"

	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/transport"
)

type txKind uint8

type txItem struct {
	kind       txKind
	header     codec.Header
	frame      []byte
	framePtr   *[]byte
	sequence   protocol.SequenceNumber
	requestID  protocol.CommandID
	responseTo protocol.CommandID
	status     protocol.CommandStatus
	pending    *pendingRequest
	dispatched chan error
}

func (s *Session) txLoop() {
	defer s.wg.Done()

	// Opportunistically coalesce only work that is already queued. The TX loop
	// never waits to form a batch, so an isolated PDU keeps the same latency
	// behavior while bursts can amortize channel scheduling and TCP write
	// syscalls. The bounds are per session and configurable.
	batch := make([]txItem, 0, s.config.TXBatchItems)
	var writeBuffer []byte
	var writeBuffers net.Buffers
	var batchTCP *net.TCPConn
	// writeArmedUntil is when the write deadline currently set on s.conn
	// expires (zero until the first write arms one). Local to this goroutine:
	// txLoop is the only writer, so it needs no synchronization.
	var writeArmedUntil time.Time
	if s.config.TXBatchItems > 1 {
		batchTCP, _ = s.conn.(*net.TCPConn)
		if batchTCP != nil {
			writeBuffers = make(net.Buffers, 0, s.config.TXBatchItems)
		} else {
			writeBuffer = make([]byte, 0, s.config.TXBatchBytes)
		}
	}

	for {
		batch = batch[:0]
		select {
		case item := <-s.tx:
			if s.txItemActive(item) {
				batch = append(batch, item)
			} else {
				// A txRequest whose pending entry is already gone (cancelled or
				// completed by a race elsewhere) before this loop ever dequeued
				// it. Its frame was never going to be written; hand the buffer
				// back now rather than losing it to the garbage collector.
				s.frames.put(item.framePtr)
			}
		case <-s.done:
			return
		}
		if len(batch) == 0 {
			continue
		}

		batchBytes := len(batch[0].frame)
		terminal := txItemTerminatesAfterWrite(batch[0])
		for !terminal && len(batch) < s.config.TXBatchItems && batchBytes < s.config.TXBatchBytes {
			select {
			case item := <-s.tx:
				if !s.txItemActive(item) {
					s.frames.put(item.framePtr)
					continue
				}
				batch = append(batch, item)
				batchBytes += len(item.frame)
				terminal = txItemTerminatesAfterWrite(item)
			default:
				terminal = true // stop draining; do not wait for more work
			}
		}

		// Lifecycle state must be committed before a response becomes visible on
		// the wire. This preserves the existing bind-response race guarantee even
		// when several already-queued PDUs share one transport write.
		for i := range batch {
			item := &batch[i]
			if item.kind == txResponse && item.responseTo != 0 {
				s.machine.CompleteInbound(item.responseTo, item.status)
			}
		}

		var err error
		if s.config.WriteTimeout > 0 {
			// Re-arm only when less than half the window remains, not on every
			// batch. On real TCP each SetWriteDeadline is a runtime poller
			// timer update, and doing it (plus a clear) around every write cost
			// about 9% of localhost throughput (Task 1.4's first version, measured
			// afterward: ~147k vs ~162k request PDU/s with it disabled). What the
			// deadline exists to bound is a write that stops making progress,
			// and that is still bounded: a write never starts with less than
			// WriteTimeout/2 left on the clock, and never runs past
			// WriteTimeout from when the deadline was last armed. The one
			// semantic difference from arming per batch is the lower bound — a
			// slow but progressing write can be cut off as early as
			// WriteTimeout/2 after it began rather than only after a full
			// WriteTimeout. There is deliberately no clear afterward: every
			// write re-arms itself first if needed, so an expired deadline left
			// on an idle conn can only ever affect a write that would have
			// re-armed it anyway.
			if now := time.Now(); writeArmedUntil.Sub(now) < s.config.WriteTimeout/2 {
				writeArmedUntil = now.Add(s.config.WriteTimeout)
				_ = s.conn.SetWriteDeadline(writeArmedUntil)
			}
		}
		if len(batch) == 1 {
			err = transport.WriteFull(s.conn, batch[0].frame)
		} else if batchTCP != nil {
			writeBuffers = writeBuffers[:0]
			for i := range batch {
				writeBuffers = append(writeBuffers, batch[i].frame)
			}
			err = transport.WriteBuffers(batchTCP, writeBuffers)
		} else {
			writeBuffer = writeBuffer[:0]
			for i := range batch {
				writeBuffer = append(writeBuffer, batch[i].frame...)
			}
			err = transport.WriteFull(s.conn, writeBuffer)
		}
		if err != nil {
			for i := range batch {
				if batch[i].dispatched != nil {
					batch[i].dispatched <- err
				}
				// The write attempt for this batch has already returned (with
				// this error), so nothing will read batch[i].frame again — the
				// same write-completion boundary as the success path below,
				// just reached via the error branch instead.
				s.frames.put(batch[i].framePtr)
			}
			s.terminate(err)
			return
		}

		now := time.Now()
		s.noteActivityAt(now)
		for i := range batch {
			item := &batch[i]
			s.tracePacket(PacketOutbound, item.header, item.frame)
			// The write has completed and tracePacket — the only reader of
			// item.frame after the write — has already run, so the buffer is
			// safe to return now. Clear both fields so any future change to
			// this loop that accidentally reads item.frame afterward fails
			// loudly (nil slice) instead of silently reading recycled memory.
			s.frames.put(item.framePtr)
			item.frame = nil
			item.framePtr = nil
			s.observeOutbound(*item, now)
			if item.kind == txRequest && item.pending != nil {
				if rtt, ok := item.pending.markDispatchAt(now); ok {
					s.observeRTT(item.pending, item.sequence, rtt)
				}
			}
			if item.dispatched != nil {
				item.dispatched <- nil
			}
			if item.kind == txRequest {
				if request, ok := s.pending.markDispatched(item.sequence); ok {
					timeout, kind := s.responseTimeoutFor(item.requestID)
					deadline := s.deadlines.scheduleItemAt(&request.deadlineStorage, now, timeout, kind, item.requestID, item.sequence)
					request.attachDeadline(s.deadlines, deadline)
				}
				continue
			}
			if txItemTerminatesAfterWrite(*item) {
				s.terminate(ErrSessionClosed)
				return
			}
		}
	}
}

func (s *Session) txItemActive(item txItem) bool {
	return item.kind != txRequest || s.pending.exists(item.sequence)
}

func txItemTerminatesAfterWrite(item txItem) bool {
	return item.kind == txResponse && item.responseTo == protocol.CommandUnbind && item.status.OK()
}

func (s *Session) observeOutbound(item txItem, at time.Time) {
	event := Event{At: at, Command: item.header.CommandID, Sequence: item.header.SequenceNumber, Status: item.header.CommandStatus}
	if item.kind == txResponse {
		s.metrics.responsesSent.Add(1)
		event.Kind = EventResponseSent
		s.emitEvent(event)
		return
	}
	s.metrics.requestsSent.Add(1)
	event.Kind = EventRequestSent
	s.emitEvent(event)
	if item.header.CommandID == protocol.CommandEnquireLink {
		s.metrics.enquireLinkSent.Add(1)
		event.Kind = EventEnquireLinkSent
		s.emitEvent(event)
	}
}

func (s *Session) queueGenericNACK(request codec.Header, status protocol.CommandStatus) error {
	return s.queueResponse(request, protocol.CommandGenericNACK, status, protocol.EmptyBody{})
}

func (s *Session) queueResponse(request codec.Header, responseID protocol.CommandID, status protocol.CommandStatus, body any) error {
	header := codec.ResponseHeader(request, responseID, status)
	hint, _ := codec.EncodedPDUSizeHint(responseID, body)
	framePtr := s.frames.get(hint)
	frame, err := codec.EncodePDU(*framePtr, header, body, s.registry)
	if err != nil {
		s.frames.put(framePtr)
		return err
	}
	*framePtr = frame
	item := txItem{kind: txResponse, header: header, frame: frame, framePtr: framePtr, sequence: request.SequenceNumber, responseTo: request.CommandID, status: status}
	if responseID == protocol.CommandGenericNACK {
		item.responseTo = 0
	}
	select {
	case s.tx <- item:
		return nil
	case <-s.done:
		s.frames.put(framePtr)
		return s.requestCloseError()
	}
}
