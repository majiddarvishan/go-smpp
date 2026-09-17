package session

import (
	"errors"
	"sync"
	"testing"

	"github.com/majiddarvishan/go-smpp/protocol"
)

func TestCanIssueOperationMatrixCore(t *testing.T) {
	tests := []struct {
		name    string
		state   protocol.SessionState
		role    Role
		command protocol.CommandID
		want    bool
	}{
		{"esme bind open", protocol.StateOpen, RoleESME, protocol.CommandBindTransceiver, true},
		{"smsc cannot bind request", protocol.StateOpen, RoleSMSC, protocol.CommandBindTransceiver, false},
		{"smsc bind response open", protocol.StateOpen, RoleSMSC, protocol.CommandBindTransceiverResp, true},
		{"submit tx", protocol.StateBoundTX, RoleESME, protocol.CommandSubmitSM, true},
		{"submit rx invalid", protocol.StateBoundRX, RoleESME, protocol.CommandSubmitSM, false},
		{"deliver rx", protocol.StateBoundRX, RoleSMSC, protocol.CommandDeliverSM, true},
		{"deliver tx invalid", protocol.StateBoundTX, RoleSMSC, protocol.CommandDeliverSM, false},
		{"trx submit", protocol.StateBoundTRX, RoleESME, protocol.CommandSubmitSM, true},
		{"trx deliver", protocol.StateBoundTRX, RoleSMSC, protocol.CommandDeliverSM, true},
		{"enquire esme", protocol.StateBoundTRX, RoleESME, protocol.CommandEnquireLink, true},
		{"enquire smsc", protocol.StateBoundTRX, RoleSMSC, protocol.CommandEnquireLink, true},
		{"generic nack open", protocol.StateOpen, RoleESME, protocol.CommandGenericNACK, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanIssue(tc.state, tc.role, tc.command); got != tc.want {
				t.Fatalf("CanIssue(%s,%s,0x%08x)=%v want %v", tc.state, tc.role, uint32(tc.command), got, tc.want)
			}
		})
	}
}

func TestStateMachineBindLifecycle(t *testing.T) {
	m, err := NewStateMachine(RoleESME)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Open(); err != nil {
		t.Fatal(err)
	}
	if err := m.BeginOutbound(protocol.CommandBindTransceiver); err != nil {
		t.Fatal(err)
	}
	if err := m.BeginOutbound(protocol.CommandBindTransmitter); !errors.Is(err, ErrBindInProgress) {
		t.Fatalf("expected bind-in-progress, got %v", err)
	}
	m.CompleteOutbound(protocol.CommandBindTransceiver, protocol.StatusOK)
	if got := m.State(); got != protocol.StateBoundTRX {
		t.Fatalf("state=%s want Bound_TRX", got)
	}
	if m.BindMode() != BindTRX {
		t.Fatalf("bind mode=%s", m.BindMode())
	}
}

func TestStateMachineInboundBindAndUnbind(t *testing.T) {
	m, err := NewStateMachine(RoleSMSC)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Open(); err != nil {
		t.Fatal(err)
	}
	if err := m.BeginInbound(protocol.CommandBindReceiver); err != nil {
		t.Fatal(err)
	}
	m.CompleteInbound(protocol.CommandBindReceiver, protocol.StatusOK)
	if m.State() != protocol.StateBoundRX {
		t.Fatalf("state=%s", m.State())
	}
	if err := m.BeginInbound(protocol.CommandUnbind); err != nil {
		t.Fatal(err)
	}
	if m.State() != protocol.StateUnbound {
		t.Fatalf("state=%s want Unbound", m.State())
	}
	m.CompleteInbound(protocol.CommandUnbind, protocol.StatusOK)
	if m.State() != protocol.StateClosed {
		t.Fatalf("state=%s want Closed", m.State())
	}
}

func TestStateMachineConcurrentReadsAndClose(t *testing.T) {
	m, err := NewStateMachine(RoleESME)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Open(); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				_ = m.State()
				_ = m.BindMode()
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		m.Close()
	}()
	wg.Wait()
	if m.State() != protocol.StateClosed {
		t.Fatalf("state=%s", m.State())
	}
}

func TestSMPP34ExtendedOperationMatrix(t *testing.T) {
	cases := []struct {
		name    string
		state   protocol.SessionState
		role    Role
		command protocol.CommandID
		want    bool
	}{
		{"replace bound tx", protocol.StateBoundTX, RoleESME, protocol.CommandReplaceSM, true},
		{"replace trx forbidden by 3.4", protocol.StateBoundTRX, RoleESME, protocol.CommandReplaceSM, false},
		{"replace response bound tx", protocol.StateBoundTX, RoleSMSC, protocol.CommandReplaceSMResp, true},
		{"replace response trx forbidden", protocol.StateBoundTRX, RoleSMSC, protocol.CommandReplaceSMResp, false},
		{"data esme tx", protocol.StateBoundTX, RoleESME, protocol.CommandDataSM, true},
		{"data esme rx", protocol.StateBoundRX, RoleESME, protocol.CommandDataSM, true},
		{"data smsc trx", protocol.StateBoundTRX, RoleSMSC, protocol.CommandDataSM, true},
		{"alert smsc rx", protocol.StateBoundRX, RoleSMSC, protocol.CommandAlertNotification, true},
		{"alert esme invalid", protocol.StateBoundRX, RoleESME, protocol.CommandAlertNotification, false},
		{"outbind smsc open", protocol.StateOpen, RoleSMSC, protocol.CommandOutbind, true},
		{"bind receiver after outbind", protocol.StateOutbound, RoleESME, protocol.CommandBindReceiver, true},
		{"bind transmitter after outbind invalid", protocol.StateOutbound, RoleESME, protocol.CommandBindTransmitter, false},
		{"bind transceiver after outbind invalid", protocol.StateOutbound, RoleESME, protocol.CommandBindTransceiver, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanIssue(tc.state, tc.role, tc.command); got != tc.want {
				t.Fatalf("CanIssue(%s,%s,0x%08x)=%v want %v", tc.state, tc.role, uint32(tc.command), got, tc.want)
			}
		})
	}
}

func TestStateMachineOutbindToBindReceiver(t *testing.T) {
	esme, err := NewStateMachine(RoleESME)
	if err != nil {
		t.Fatal(err)
	}
	if err := esme.Open(); err != nil {
		t.Fatal(err)
	}
	if err := esme.BeginInbound(protocol.CommandOutbind); err != nil {
		t.Fatal(err)
	}
	if esme.State() != protocol.StateOutbound {
		t.Fatalf("ESME state=%s want Outbound", esme.State())
	}
	if err := esme.BeginOutbound(protocol.CommandBindReceiver); err != nil {
		t.Fatal(err)
	}
	esme.CompleteOutbound(protocol.CommandBindReceiver, protocol.StatusOK)
	if esme.State() != protocol.StateBoundRX {
		t.Fatalf("ESME state=%s want Bound_RX", esme.State())
	}

	smsc, err := NewStateMachine(RoleSMSC)
	if err != nil {
		t.Fatal(err)
	}
	if err := smsc.Open(); err != nil {
		t.Fatal(err)
	}
	if err := smsc.BeginOutbound(protocol.CommandOutbind); err != nil {
		t.Fatal(err)
	}
	if smsc.State() != protocol.StateOutbound {
		t.Fatalf("SMSC state=%s want Outbound", smsc.State())
	}
	if err := smsc.BeginInbound(protocol.CommandBindReceiver); err != nil {
		t.Fatal(err)
	}
	smsc.CompleteInbound(protocol.CommandBindReceiver, protocol.StatusOK)
	if smsc.State() != protocol.StateBoundRX {
		t.Fatalf("SMSC state=%s want Bound_RX", smsc.State())
	}
}

func TestCancelOutboundOutbindRestoresOpen(t *testing.T) {
	m, err := NewStateMachine(RoleSMSC)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Open(); err != nil {
		t.Fatal(err)
	}
	if err := m.BeginOutbound(protocol.CommandOutbind); err != nil {
		t.Fatal(err)
	}
	m.CancelOutbound(protocol.CommandOutbind)
	if m.State() != protocol.StateOpen {
		t.Fatalf("state=%s want Open", m.State())
	}
}
