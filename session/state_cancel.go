package session

import (
	"github.com/majiddarvishan/go-smpp/protocol"
)

// CancelInbound rolls back a state-sensitive peer request when its response
// could not be encoded/queued. Session shutdown still wins independently.
func (m *StateMachine) CancelInbound(command protocol.CommandID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if isBindRequest(command) && m.pendingPeerBind == command {
		m.pendingPeerBind = 0
	}
	if command == protocol.CommandUnbind && m.State() == protocol.StateUnbound && m.peerUnbindFrom != protocol.StateClosed {
		m.state.Store(uint32(m.peerUnbindFrom))
		m.peerUnbindFrom = protocol.StateClosed
	}
}
