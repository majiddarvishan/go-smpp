package session

import (
	"testing"

	"github.com/majiddarvishan/go-smpp/protocol"
)

// oldCanIssue is the pre-Task-3.1-refactor switch statement, copied verbatim
// (not reconstructed from memory of what it should say) as an independent
// oracle. This file's only job is TestCanIssueMatchesOldSwitchExhaustively,
// which checks every command this package names, in every SessionState, for
// both roles, against it — proof that converting the switch into
// standardRules (Finding B10, so a vendor command's own declared policy can
// be checked the same way) did not silently change any standard command's
// behavior. Delete this file once that confidence is no longer needed; it
// exists for this one refactor, not as an ongoing behavioral spec (that's
// TestCanIssueOperationMatrixCore and the other hand-written cases already in
// state_test.go/phase14_test.go, which exercise the real CanIssue directly).
func oldCanIssue(state protocol.SessionState, role Role, command protocol.CommandID) bool {
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

func TestCanIssueMatchesOldSwitchExhaustively(t *testing.T) {
	commands := []protocol.CommandID{
		protocol.CommandBindTransmitter, protocol.CommandBindTransceiver, protocol.CommandBindReceiver,
		protocol.CommandBindTransmitterResp, protocol.CommandBindTransceiverResp, protocol.CommandBindReceiverResp,
		protocol.CommandOutbind, protocol.CommandUnbind, protocol.CommandUnbindResp,
		protocol.CommandEnquireLink, protocol.CommandEnquireLinkResp,
		protocol.CommandSubmitSM, protocol.CommandSubmitMulti, protocol.CommandQuerySM, protocol.CommandCancelSM,
		protocol.CommandBroadcastSM, protocol.CommandQueryBroadcastSM, protocol.CommandCancelBroadcastSM,
		protocol.CommandReplaceSM,
		protocol.CommandSubmitSMResp, protocol.CommandSubmitMultiResp, protocol.CommandQuerySMResp, protocol.CommandCancelSMResp,
		protocol.CommandBroadcastSMResp, protocol.CommandQueryBroadcastSMResp, protocol.CommandCancelBroadcastSMResp,
		protocol.CommandReplaceSMResp,
		protocol.CommandDeliverSM, protocol.CommandDeliverSMResp,
		protocol.CommandDataSM, protocol.CommandDataSMResp,
		protocol.CommandAlertNotification,
		protocol.CommandGenericNACK,
		protocol.CommandID(0xFFFF0001), // an unknown/vendor-shaped ID, to check both oracles agree it's unrecognized
	}
	states := []protocol.SessionState{
		protocol.StateClosed, protocol.StateOpen, protocol.StateOutbound,
		protocol.StateBoundTX, protocol.StateBoundRX, protocol.StateBoundTRX, protocol.StateUnbound,
	}
	roles := []Role{RoleESME, RoleSMSC}

	mismatches := 0
	for _, command := range commands {
		for _, state := range states {
			for _, role := range roles {
				want := oldCanIssue(state, role, command)
				got := CanIssue(state, role, command)
				if got != want {
					mismatches++
					t.Errorf("CanIssue(state=%v, role=%v, command=%#x) = %v, old switch said %v", state, role, uint32(command), got, want)
				}
			}
		}
	}
	if mismatches > 0 {
		t.Fatalf("%d combination(s) disagree with the pre-refactor switch", mismatches)
	}
}
