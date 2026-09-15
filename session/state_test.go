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
