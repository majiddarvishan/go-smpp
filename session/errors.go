package session

import (
	"errors"
	"fmt"
	"time"

	"github.com/majiddarvishan/go-smpp/protocol"
)

var (
	ErrSessionClosed     = errors.New("smpp session: closed")
	ErrInvalidConfig     = errors.New("smpp session: invalid configuration")
	ErrPendingLimit      = errors.New("smpp session: pending request limit reached")
	ErrSequenceInUse     = errors.New("smpp session: sequence number already pending")
	ErrSequenceExhausted = errors.New("smpp session: no free sequence number")
	ErrBindInProgress    = errors.New("smpp session: bind already in progress")
	ErrUnexpectedPDU     = errors.New("smpp session: unexpected PDU")
)

// TimeoutKind distinguishes independent SMPP timeout/liveness mechanisms.
type TimeoutKind uint8

const (
	TimeoutResponse TimeoutKind = iota + 1
	TimeoutSessionInit
	TimeoutEnquireLink
	TimeoutInactivity
)

func (k TimeoutKind) String() string {
	switch k {
	case TimeoutResponse:
		return "response"
	case TimeoutSessionInit:
		return "session_init"
	case TimeoutEnquireLink:
		return "enquire_link"
	case TimeoutInactivity:
		return "inactivity"
	default:
		return "unknown"
	}
}

// TimeoutError is returned when a configured SMPP protocol/session deadline
// expires. It is intentionally distinct from caller context cancellation.
type TimeoutError struct {
	Kind     TimeoutKind
	Command  protocol.CommandID
	Sequence protocol.SequenceNumber
	After    time.Duration
}

func (e *TimeoutError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("smpp %s timeout: command=0x%08x sequence=%d after=%s", e.Kind, uint32(e.Command), uint32(e.Sequence), e.After)
}

func (e *TimeoutError) Timeout() bool { return true }

// StateError reports that a PDU is not legal for the local role/session state.
type StateError struct {
	State   protocol.SessionState
	Command protocol.CommandID
	Role    Role
}

func (e *StateError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("smpp session: command 0x%08x is invalid in state %s for role %s", uint32(e.Command), e.State, e.Role)
}

// ResponseError reports a non-success command_status or a generic_nack returned
// for a synchronous request.
type ResponseError struct {
	Command  protocol.CommandID
	Status   protocol.CommandStatus
	Sequence protocol.SequenceNumber
}

func (e *ResponseError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("smpp response error: command=0x%08x status=0x%08x sequence=%d", uint32(e.Command), uint32(e.Status), uint32(e.Sequence))
}

// LossError reports that an active session disappeared before a request could
// complete. Cause preserves transport/fatal-protocol diagnostics.
type LossError struct {
	Cause error
}

func (e *LossError) Error() string {
	if e == nil || e.Cause == nil {
		return "smpp session lost"
	}
	return "smpp session lost: " + e.Cause.Error()
}

func (e *LossError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}
