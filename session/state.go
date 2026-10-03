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

// operationRule is the direction + permitted-state policy for one standard
// command: which role (0 means either) may issue it, and in which states.
// This is the same shape codec.CommandDefinition's Actor/AllowedStates
// fields use for a vendor command's own policy (Finding B10) — standardRules
// below is this package's hand-maintained source of truth for the standard
// command set specifically, exactly as it was before this type existed; only
// its representation changed, from a switch's control flow to this map's
// data, so that membership in it (IsStandardCommand) can be derived instead
// of needing its own separately hand-maintained list.
type operationRule struct {
	actor  Role
	states []protocol.SessionState
}

func (r operationRule) allows(state protocol.SessionState, role Role) bool {
	if r.actor != 0 && role != r.actor {
		return false
	}
	for _, s := range r.states {
		if s == state {
			return true
		}
	}
	return false
}

var (
	openOnly       = []protocol.SessionState{protocol.StateOpen}
	openOrOutbound = []protocol.SessionState{protocol.StateOpen, protocol.StateOutbound}
	boundStates    = []protocol.SessionState{protocol.StateBoundTX, protocol.StateBoundRX, protocol.StateBoundTRX}
	boundOrUnbound = []protocol.SessionState{protocol.StateBoundTX, protocol.StateBoundRX, protocol.StateBoundTRX, protocol.StateUnbound}
	boundTXOrTRX   = []protocol.SessionState{protocol.StateBoundTX, protocol.StateBoundTRX}
	boundRXOrTRX   = []protocol.SessionState{protocol.StateBoundRX, protocol.StateBoundTRX}
	boundTXOnly    = []protocol.SessionState{protocol.StateBoundTX}
)

// standardRules is the SMPP 3.4 operation/state direction matrix for the
// command set known by the protocol package. generic_nack is deliberately
// not in this map — it is allowed in any connected state so a structurally
// valid unknown command can be rejected, which CanIssue still special-cases
// directly, the same as before this table existed.
var standardRules = map[protocol.CommandID]operationRule{
	protocol.CommandBindTransmitter:       {actor: RoleESME, states: openOnly},
	protocol.CommandBindTransceiver:       {actor: RoleESME, states: openOnly},
	protocol.CommandBindReceiver:          {actor: RoleESME, states: openOrOutbound},
	protocol.CommandBindTransmitterResp:   {actor: RoleSMSC, states: openOnly},
	protocol.CommandBindTransceiverResp:   {actor: RoleSMSC, states: openOnly},
	protocol.CommandBindReceiverResp:      {actor: RoleSMSC, states: openOrOutbound},
	protocol.CommandOutbind:               {actor: RoleSMSC, states: openOnly},
	protocol.CommandUnbind:                {states: boundStates},
	protocol.CommandUnbindResp:            {states: boundOrUnbound},
	protocol.CommandEnquireLink:           {states: boundStates},
	protocol.CommandEnquireLinkResp:       {states: boundStates},
	protocol.CommandSubmitSM:              {actor: RoleESME, states: boundTXOrTRX},
	protocol.CommandSubmitMulti:           {actor: RoleESME, states: boundTXOrTRX},
	protocol.CommandQuerySM:               {actor: RoleESME, states: boundTXOrTRX},
	protocol.CommandCancelSM:              {actor: RoleESME, states: boundTXOrTRX},
	protocol.CommandBroadcastSM:           {actor: RoleESME, states: boundTXOrTRX},
	protocol.CommandQueryBroadcastSM:      {actor: RoleESME, states: boundTXOrTRX},
	protocol.CommandCancelBroadcastSM:     {actor: RoleESME, states: boundTXOrTRX},
	protocol.CommandReplaceSM:             {actor: RoleESME, states: boundTXOnly},
	protocol.CommandSubmitSMResp:          {actor: RoleSMSC, states: boundTXOrTRX},
	protocol.CommandSubmitMultiResp:       {actor: RoleSMSC, states: boundTXOrTRX},
	protocol.CommandQuerySMResp:           {actor: RoleSMSC, states: boundTXOrTRX},
	protocol.CommandCancelSMResp:          {actor: RoleSMSC, states: boundTXOrTRX},
	protocol.CommandBroadcastSMResp:       {actor: RoleSMSC, states: boundTXOrTRX},
	protocol.CommandQueryBroadcastSMResp:  {actor: RoleSMSC, states: boundTXOrTRX},
	protocol.CommandCancelBroadcastSMResp: {actor: RoleSMSC, states: boundTXOrTRX},
	protocol.CommandReplaceSMResp:         {actor: RoleSMSC, states: boundTXOnly},
	protocol.CommandDeliverSM:             {actor: RoleSMSC, states: boundRXOrTRX},
	protocol.CommandDeliverSMResp:         {actor: RoleESME, states: boundRXOrTRX},
	protocol.CommandDataSM:                {states: boundStates},
	protocol.CommandDataSMResp:            {states: boundStates},
	protocol.CommandAlertNotification:     {actor: RoleSMSC, states: boundRXOrTRX},
}

// CanIssue implements the SMPP 3.4 operation/state direction rules for the
// command set known by the protocol package. generic_nack is allowed in any
// connected state so a structurally valid unknown command can be rejected.
func CanIssue(state protocol.SessionState, role Role, command protocol.CommandID) bool {
	if !role.valid() || state == protocol.StateClosed {
		return false
	}
	if command == protocol.CommandGenericNACK {
		return true
	}
	rule, ok := standardRules[command]
	if !ok {
		return false
	}
	return rule.allows(state, role)
}

// IsStandardCommand reports whether command is part of the operation/state
// matrix CanIssue enforces directly — the set that replaced the old,
// separately hand-maintained isStandardSessionCommand list (Finding B10):
// membership here is derived from standardRules, the same data CanIssue
// itself consults, so the two can no longer drift apart.
func IsStandardCommand(command protocol.CommandID) bool {
	if command == protocol.CommandGenericNACK {
		return true
	}
	_, ok := standardRules[command]
	return ok
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
