package session

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/majiddarvishan/go-smpp/protocol"
)

// Role identifies which SMPP peer behavior the local session represents.
type Role uint8

const (
	RoleESME Role = iota + 1
	RoleSMSC
)

func (r Role) String() string {
	switch r {
	case RoleESME:
		return "ESME"
	case RoleSMSC:
		return "SMSC"
	default:
		return "Unknown"
	}
}

func (r Role) valid() bool { return r == RoleESME || r == RoleSMSC }

func (r Role) peer() Role {
	if r == RoleESME {
		return RoleSMSC
	}
	if r == RoleSMSC {
		return RoleESME
	}
	return 0
}

// BindMode is the negotiated SMPP bind mode for a bound session.
type BindMode uint8

const (
	BindNone BindMode = iota
	BindTX
	BindRX
	BindTRX
)

func (m BindMode) String() string {
	switch m {
	case BindTX:
		return "TX"
	case BindRX:
		return "RX"
	case BindTRX:
		return "TRX"
	default:
		return "None"
	}
}

// CanIssue implements the SMPP 3.4 operation/state direction rules for the
// command set known by the protocol package. generic_nack is allowed in any
// connected state so a structurally valid unknown command can be rejected.
func CanIssue(state protocol.SessionState, role Role, command protocol.CommandID) bool {
	if !role.valid() || state == protocol.StateClosed {
		return false
	}

	bound := state == protocol.StateBoundTX || state == protocol.StateBoundRX || state == protocol.StateBoundTRX
	if command == protocol.CommandGenericNACK {
		return true
	}

	switch command {
	case protocol.CommandBindTransmitter, protocol.CommandBindTransceiver:
		return state == protocol.StateOpen && role == RoleESME
	case protocol.CommandBindReceiver:
		return role == RoleESME && (state == protocol.StateOpen || state == protocol.StateOutbound)
	case protocol.CommandBindTransmitterResp, protocol.CommandBindTransceiverResp:
		return state == protocol.StateOpen && role == RoleSMSC
	case protocol.CommandBindReceiverResp:
		return role == RoleSMSC && (state == protocol.StateOpen || state == protocol.StateOutbound)
	case protocol.CommandOutbind:
		return state == protocol.StateOpen && role == RoleSMSC
	case protocol.CommandUnbind:
		return bound
	case protocol.CommandUnbindResp:
		return bound || state == protocol.StateUnbound
	case protocol.CommandEnquireLink, protocol.CommandEnquireLinkResp:
		return bound
	case protocol.CommandSubmitSM, protocol.CommandSubmitMulti, protocol.CommandQuerySM, protocol.CommandCancelSM,
		protocol.CommandBroadcastSM, protocol.CommandQueryBroadcastSM, protocol.CommandCancelBroadcastSM:
		return role == RoleESME && (state == protocol.StateBoundTX || state == protocol.StateBoundTRX)
	case protocol.CommandReplaceSM:
		return role == RoleESME && state == protocol.StateBoundTX
	case protocol.CommandSubmitSMResp, protocol.CommandSubmitMultiResp, protocol.CommandQuerySMResp, protocol.CommandCancelSMResp,
		protocol.CommandBroadcastSMResp, protocol.CommandQueryBroadcastSMResp, protocol.CommandCancelBroadcastSMResp:
		return role == RoleSMSC && (state == protocol.StateBoundTX || state == protocol.StateBoundTRX)
	case protocol.CommandReplaceSMResp:
		return role == RoleSMSC && state == protocol.StateBoundTX
	case protocol.CommandDeliverSM:
		return role == RoleSMSC && (state == protocol.StateBoundRX || state == protocol.StateBoundTRX)
	case protocol.CommandDeliverSMResp:
		return role == RoleESME && (state == protocol.StateBoundRX || state == protocol.StateBoundTRX)
	case protocol.CommandDataSM, protocol.CommandDataSMResp:
		return bound
	case protocol.CommandAlertNotification:
		return role == RoleSMSC && (state == protocol.StateBoundRX || state == protocol.StateBoundTRX)
	default:
		return false
	}
}

// StateMachine is shared by ESME and SMSC sessions. Normal state reads are
// atomic; the mutex protects multi-step bind/unbind lifecycle transitions.
type StateMachine struct {
	role  Role
	state atomic.Uint32

	mu               sync.Mutex
	pendingLocalBind protocol.CommandID
	pendingPeerBind  protocol.CommandID
	localUnbindFrom  protocol.SessionState
	peerUnbindFrom   protocol.SessionState
}

func NewStateMachine(role Role) (*StateMachine, error) {
	if !role.valid() {
		return nil, fmt.Errorf("%w: invalid role %d", ErrInvalidConfig, role)
	}
	m := &StateMachine{role: role}
	m.state.Store(uint32(protocol.StateClosed))
	return m, nil
}

func (m *StateMachine) Role() Role { return m.role }

func (m *StateMachine) State() protocol.SessionState {
	return protocol.SessionState(m.state.Load())
}

func (m *StateMachine) BindMode() BindMode {
	switch m.State() {
	case protocol.StateBoundTX:
		return BindTX
	case protocol.StateBoundRX:
		return BindRX
	case protocol.StateBoundTRX:
		return BindTRX
	default:
		return BindNone
	}
}

// Open marks an established transport as connected and awaiting session bind.
func (m *StateMachine) Open() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.State() != protocol.StateClosed {
		return &StateError{State: m.State(), Role: m.role}
	}
	m.state.Store(uint32(protocol.StateOpen))
	return nil
}

// MarkOutbound records the v5-aware Outbound state used by later outbind work.
func (m *StateMachine) MarkOutbound() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.State() != protocol.StateOpen {
		return &StateError{State: m.State(), Command: protocol.CommandOutbind, Role: m.role}
	}
	m.state.Store(uint32(protocol.StateOutbound))
	return nil
}

// BeginOutbound validates and reserves state-sensitive local operations.
func (m *StateMachine) BeginOutbound(command protocol.CommandID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.State()
	if !CanIssue(state, m.role, command) {
		return &StateError{State: state, Command: command, Role: m.role}
	}
	if isBindRequest(command) {
		if m.pendingLocalBind != 0 {
			return ErrBindInProgress
		}
		m.pendingLocalBind = command
	}
	if command == protocol.CommandUnbind {
		m.localUnbindFrom = state
		m.state.Store(uint32(protocol.StateUnbound))
	}
	if command == protocol.CommandOutbind {
		m.state.Store(uint32(protocol.StateOutbound))
	}
	return nil
}

// CancelOutbound rolls back only lifecycle reservations that did not complete.
func (m *StateMachine) CancelOutbound(command protocol.CommandID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if isBindRequest(command) && m.pendingLocalBind == command {
		m.pendingLocalBind = 0
	}
	if command == protocol.CommandUnbind && m.State() == protocol.StateUnbound && m.localUnbindFrom != protocol.StateClosed {
		m.state.Store(uint32(m.localUnbindFrom))
		m.localUnbindFrom = protocol.StateClosed
	}
	if command == protocol.CommandOutbind && m.State() == protocol.StateOutbound {
		m.state.Store(uint32(protocol.StateOpen))
	}
}

// CompleteOutbound applies the response to a locally originated lifecycle PDU.
func (m *StateMachine) CompleteOutbound(command protocol.CommandID, status protocol.CommandStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if isBindRequest(command) {
		if m.pendingLocalBind != command {
			return
		}
		m.pendingLocalBind = 0
		if status.OK() {
			m.state.Store(uint32(boundStateForBind(command)))
		}
		return
	}
	if command == protocol.CommandUnbind && m.State() == protocol.StateUnbound {
		if status.OK() {
			m.state.Store(uint32(protocol.StateClosed))
		} else if m.localUnbindFrom != protocol.StateClosed {
			m.state.Store(uint32(m.localUnbindFrom))
		}
		m.localUnbindFrom = protocol.StateClosed
	}
}

// BeginInbound validates a peer-originated request and reserves state-sensitive
// bind/unbind transitions until the response is actually written.
func (m *StateMachine) BeginInbound(command protocol.CommandID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.State()
	peerRole := m.role.peer()
	if !CanIssue(state, peerRole, command) {
		return &StateError{State: state, Command: command, Role: peerRole}
	}
	if isBindRequest(command) {
		if m.pendingPeerBind != 0 {
			return ErrBindInProgress
		}
		m.pendingPeerBind = command
	}
	if command == protocol.CommandUnbind {
		m.peerUnbindFrom = state
		m.state.Store(uint32(protocol.StateUnbound))
	}
	if command == protocol.CommandOutbind {
		m.state.Store(uint32(protocol.StateOutbound))
	}
	return nil
}

// CompleteInbound is called only after the response to a peer lifecycle request
// has been fully dispatched to the transport.
func (m *StateMachine) CompleteInbound(command protocol.CommandID, status protocol.CommandStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if isBindRequest(command) {
		if m.pendingPeerBind != command {
			return
		}
		m.pendingPeerBind = 0
		if status.OK() {
			m.state.Store(uint32(boundStateForBind(command)))
		}
		return
	}
	if command == protocol.CommandUnbind && m.State() == protocol.StateUnbound {
		if status.OK() {
			m.state.Store(uint32(protocol.StateClosed))
		} else if m.peerUnbindFrom != protocol.StateClosed {
			m.state.Store(uint32(m.peerUnbindFrom))
		}
		m.peerUnbindFrom = protocol.StateClosed
	}
}

func (m *StateMachine) Close() {
	m.mu.Lock()
	m.pendingLocalBind = 0
	m.pendingPeerBind = 0
	m.localUnbindFrom = protocol.StateClosed
	m.peerUnbindFrom = protocol.StateClosed
	m.state.Store(uint32(protocol.StateClosed))
	m.mu.Unlock()
}

func isBindRequest(command protocol.CommandID) bool {
	return command == protocol.CommandBindTransmitter || command == protocol.CommandBindReceiver || command == protocol.CommandBindTransceiver
}

func boundStateForBind(command protocol.CommandID) protocol.SessionState {
	switch command {
	case protocol.CommandBindTransmitter:
		return protocol.StateBoundTX
	case protocol.CommandBindReceiver:
		return protocol.StateBoundRX
	case protocol.CommandBindTransceiver:
		return protocol.StateBoundTRX
	default:
		return protocol.StateOpen
	}
}
