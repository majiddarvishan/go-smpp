package session

import (
	"fmt"
	"time"

	"github.com/majiddarvishan/go-smpp/protocol"
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

// Timeout reports that this error represents a timeout condition.
func (e *TimeoutError) Timeout() bool { return true }
