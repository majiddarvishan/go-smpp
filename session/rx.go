package session

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/protocol"
)

func isBoundState(state protocol.SessionState) bool {
	return state == protocol.StateBoundTX || state == protocol.StateBoundRX || state == protocol.StateBoundTRX
}

func (s *Session) rxLoop() {
	defer s.wg.Done()
	buffer := make([]byte, s.config.ReadBufferSize)
	for {
		n, readErr := s.conn.Read(buffer)
		if n > 0 {
			if err := s.framer.Feed(buffer[:n], s.processFrame); err != nil {
				s.terminate(err)
				return
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				if err := s.framer.Finalize(); err != nil {
					s.terminate(err)
				} else {
					s.terminate(io.EOF)
				}
			} else {
				s.terminate(readErr)
			}
			return
		}
		select {
		case <-s.done:
			return
		default:
		}
	}
}

func (s *Session) processFrame(frame []byte) error {
	pdu, err := codec.DecodePDU(frame, s.registry)
	if err != nil {
		var fatal *protocol.FatalError
		if errors.As(err, &fatal) {
			return err
		}
		s.metrics.decodeFailures.Add(1)
		s.emitEvent(Event{Kind: EventDecodeFailure, Reason: err.Error()})
		header, headerErr := codec.DecodeHeader(frame)
		if headerErr != nil {
			return headerErr
		}
		// Framing is still trustworthy. Reject the semantic/body error without
		// poisoning or resynchronizing the TCP stream, with the specific status
		// the decoder attached when it recognized the error's shape (Finding B6)
		// — a wrong-length TLV and a body that's simply too long are different
		// failures and peers can reasonably expect different codes back.
		// StatusInvalidMessageLength is the fallback for a decode error the
		// decoder didn't classify, not a claim that every semantic error is
		// actually a message-length problem.
		status := protocol.StatusInvalidMessageLength
		var semantic *protocol.SemanticError
		if errors.As(err, &semantic) && semantic.Status != 0 {
			status = semantic.Status
		}
		_ = s.queueGenericNACK(header, status)
		return nil
	}
	now := time.Now()
	s.noteActivityAt(now)
	s.tracePacket(PacketInbound, pdu.Header, frame)
	if pdu.Header.CommandID.IsResponse() {
		s.metrics.responsesReceived.Add(1)
		s.emitEvent(Event{Kind: EventResponseReceived, At: now, Command: pdu.Header.CommandID, Sequence: pdu.Header.SequenceNumber, Status: pdu.Header.CommandStatus})
		if pdu.Header.CommandID == protocol.CommandEnquireLinkResp {
			s.metrics.enquireLinkResponses.Add(1)
		}
		s.handleResponse(pdu, now)
		return nil
	}
	s.metrics.requestsReceived.Add(1)
	s.emitEvent(Event{Kind: EventRequestReceived, At: now, Command: pdu.Header.CommandID, Sequence: pdu.Header.SequenceNumber, Status: pdu.Header.CommandStatus})
	if pdu.Header.CommandID == protocol.CommandEnquireLink {
		s.metrics.enquireLinkReceived.Add(1)
		s.emitEvent(Event{Kind: EventEnquireLinkReceived, At: now, Command: pdu.Header.CommandID, Sequence: pdu.Header.SequenceNumber})
	}
	return s.handleRequest(pdu)
}

func (s *Session) handleResponse(pdu codec.DecodedPDU, receivedAt time.Time) {
	request, ok := s.pending.takeResponse(pdu.Header.SequenceNumber, pdu.Header.CommandID)
	if !ok {
		return // late, unmatched, duplicate, or response from another sequence space
	}
	if rtt, ok := request.markResponseAt(receivedAt, pdu.Header.CommandID, pdu.Header.CommandStatus); ok {
		s.observeRTT(request, pdu.Header.SequenceNumber, rtt)
	}
	s.machine.CompleteOutbound(request.requestID, pdu.Header.CommandStatus)
	if pdu.Header.CommandStatus.OK() && isBindRequest(request.requestID) {
		if body, ok := pdu.Body.(protocol.BindResponse); ok {
			peerVersion, advertised := protocol.SCInterfaceVersion(body.Optional)
			localProfile, negotiable := bindNegotiationProfile(s.config.Profile, request.bindVersion)
			if !negotiable {
				localProfile = protocol.Profile{}
			}
			s.peerCaps.Store(protocol.NegotiatePeerCapabilities(localProfile, peerVersion, advertised))
		}
	}
	s.observeCongestion(pdu)
	owned := ownDecodedPDU(pdu)
	request.done <- requestResult{pdu: owned}
	if request.requestID == protocol.CommandUnbind && pdu.Header.CommandStatus.OK() {
		s.terminate(ErrSessionClosed)
	}
}

func (s *Session) observeRTT(request *pendingRequest, sequence protocol.SequenceNumber, rtt time.Duration) {
	s.metrics.noteRTT(rtt)
	command, status := request.responseMetadata()
	s.emitEvent(Event{Kind: EventResponseRTT, Command: command, Sequence: sequence, Status: status, Duration: rtt})
}

func (s *Session) handleRequest(pdu codec.DecodedPDU) error {
	if !pdu.Header.SequenceNumber.ValidInbound() {
		// Not a length problem (Finding B6) — SMPP has no status code for "the
		// header's sequence_number is outside the valid inbound range" at all.
		// StatusSystemError is the closest available signal that this is some
		// other kind of processing failure, not a claim that it fits precisely.
		return s.queueGenericNACK(pdu.Header, protocol.StatusSystemError)
	}
	_, registered := s.registry.Command(pdu.Header.CommandID)
	if !registered {
		return s.queueGenericNACK(pdu.Header, protocol.StatusInvalidCommandID)
	}
	if requiresSMPP50(pdu.Header.CommandID) && !s.PeerCapabilities().SupportsSMPP50 {
		return s.queueGenericNACK(pdu.Header, protocol.StatusInvalidCommandID)
	}

	if err := s.beginInbound(pdu.Header.CommandID); err != nil {
		if isOneWayCommand(pdu.Header.CommandID) {
			return s.queueGenericNACK(pdu.Header, protocol.StatusInvalidBindState)
		}
		return s.queueResponse(pdu.Header, pdu.Header.CommandID.ResponseID(), protocol.StatusInvalidBindState, protocol.EmptyBody{})
	}
	if isOneWayCommand(pdu.Header.CommandID) {
		if s.config.Handler != nil {
			if _, err := s.config.Handler.Handle(s.ctx, s, pdu); err != nil {
				s.logger.Error("SMPP one-way handler failed",
					"event", "smpp_one_way_handler_error",
					"command_id", fmt.Sprintf("0x%08x", uint32(pdu.Header.CommandID)),
					"sequence_number", uint32(pdu.Header.SequenceNumber),
					"error", err)
			}
		}
		return nil
	}
	lifecycleReserved := isLifecycleRequest(pdu.Header.CommandID)

	var response Response
	var handlerErr error
	switch pdu.Header.CommandID {
	case protocol.CommandEnquireLink, protocol.CommandUnbind:
		response = Response{Status: protocol.StatusOK, Body: protocol.EmptyBody{}}
	default:
		if s.config.Handler == nil {
			if isBindRequest(pdu.Header.CommandID) {
				response = Response{Status: protocol.StatusBindFailed, Body: protocol.EmptyBody{}}
			} else {
				response = Response{Status: protocol.StatusSystemError, Body: protocol.EmptyBody{}}
			}
		} else {
			response, handlerErr = s.config.Handler.Handle(s.ctx, s, pdu)
			if handlerErr != nil {
				response = Response{Status: protocol.StatusSystemError, Body: protocol.EmptyBody{}}
			}
		}
	}
	if response.Status.OK() && isBindRequest(pdu.Header.CommandID) {
		if bind, ok := pdu.Body.(protocol.BindRequest); ok {
			s.peerCaps.Store(protocol.NegotiatePeerCapabilities(s.config.Profile, bind.InterfaceVersion, true))
			if bind.InterfaceVersion == protocol.InterfaceVersion34 || bind.InterfaceVersion == protocol.InterfaceVersion50 {
				response.Body = ensureSCInterfaceVersion(response.Body, s.config.Profile.InterfaceVersion)
			}
		}
	}
	if err := s.queueResponse(pdu.Header, pdu.Header.CommandID.ResponseID(), response.Status, response.Body); err != nil {
		if lifecycleReserved {
			s.machine.CancelInbound(pdu.Header.CommandID)
		}
		return err
	}
	return nil
}

// actorMatchesRole reports whether a vendor command's declared policy
// (codec.CommandActor) permits role to issue it. ActorAny permits either
// role — see codec.CommandDefinition's doc comment.
func actorMatchesRole(actor codec.CommandActor, role Role) bool {
	switch actor {
	case codec.ActorESME:
		return role == RoleESME
	case codec.ActorSMSC:
		return role == RoleSMSC
	default:
		return true
	}
}

// beginInbound validates a peer-originated request's direction and state.
// A standard command is checked by the state machine's own operation matrix
// (CanIssue/IsStandardCommand, session/state.go). A vendor command has no
// entry there; one that declared its own Actor/AllowedStates when registered
// (Finding B10) is checked against that uniformly, the same shape a standard
// command's rule has internally — a wrong-direction or wrong-state vendor
// request is refused exactly as its standard counterpart would be. A vendor
// command that declared neither keeps this library's previous, conservative
// default: permitted in any bound state, from either role.
func (s *Session) beginInbound(command protocol.CommandID) error {
	peerRole := s.Role().peer()
	state := s.State()
	if IsStandardCommand(command) {
		return s.machine.BeginInbound(command)
	}
	if def, ok := s.registry.Command(command); ok && len(def.AllowedStates) > 0 {
		if actorMatchesRole(def.Actor, peerRole) && def.AllowsState(state) {
			return nil
		}
		return &StateError{State: state, Command: command, Role: peerRole}
	}
	if isBoundState(state) {
		return nil
	}
	return &StateError{State: state, Command: command, Role: peerRole}
}

func isOneWayCommand(command protocol.CommandID) bool {
	return command == protocol.CommandOutbind || command == protocol.CommandAlertNotification
}

func isLifecycleRequest(command protocol.CommandID) bool {
	return isBindRequest(command) || command == protocol.CommandUnbind
}

func (s *Session) observeCongestion(pdu codec.DecodedPDU) {
	if !s.PeerCapabilities().SupportsSMPP50 {
		return
	}
	optional := responseOptionalParameters(pdu.Body)
	state, present, err := protocol.CongestionStateFromOptional(optional)
	if err != nil || !present {
		return
	}
	now := time.Now()
	s.metrics.noteCongestion(state)
	s.emitEvent(Event{Kind: EventCongestionState, At: now, Command: pdu.Header.CommandID, Sequence: pdu.Header.SequenceNumber, Congestion: state})
	if s.config.FlowController != nil {
		s.config.FlowController.OnCongestion(CongestionEvent{
			State: state, Command: pdu.Header.CommandID, Sequence: pdu.Header.SequenceNumber, At: now,
		})
	}
}
