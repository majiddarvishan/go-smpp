package protocol

import "fmt"

// SemanticError describes a recoverable SMPP protocol/semantic violation for
// which framing is still trustworthy. Later session code may answer such an
// error with an SMPP response/status where the specification permits it.
type SemanticError struct {
	Status   CommandStatus
	Command  CommandID
	Sequence SequenceNumber
	Reason   string
	Cause    error
}

func (e *SemanticError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Reason != "" {
		return fmt.Sprintf("smpp protocol error: status=0x%08x command=0x%08x sequence=%d reason=%s", uint32(e.Status), uint32(e.Command), uint32(e.Sequence), e.Reason)
	}
	return fmt.Sprintf("smpp protocol error: status=0x%08x command=0x%08x sequence=%d", uint32(e.Status), uint32(e.Command), uint32(e.Sequence))
}

func (e *SemanticError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// FatalErrorKind identifies structural/framing failures after which the current
// TCP byte stream must not be trusted or resynchronized.
type FatalErrorKind uint8

const (
	FatalUnknown FatalErrorKind = iota
	FatalInvalidCommandLength
	FatalPDUTooLarge
	FatalTruncatedFrame
	FatalInvalidFieldLength
	FatalInvalidCString
	FatalInvalidTLVLength
	FatalFrameBoundaryLost
)

func (k FatalErrorKind) String() string {
	switch k {
	case FatalInvalidCommandLength:
		return "invalid_command_length"
	case FatalPDUTooLarge:
		return "pdu_too_large"
	case FatalTruncatedFrame:
		return "truncated_frame"
	case FatalInvalidFieldLength:
		return "invalid_field_length"
	case FatalInvalidCString:
		return "invalid_c_octet_string"
	case FatalInvalidTLVLength:
		return "invalid_tlv_length"
	case FatalFrameBoundaryLost:
		return "frame_boundary_lost"
	default:
		return "unknown_fatal_protocol_error"
	}
}

// FatalError marks a protocol structural/framing failure that requires the
// current connection/session to be terminated after logging.
type FatalError struct {
	Kind           FatalErrorKind
	DeclaredLength uint32
	Command        CommandID
	Sequence       SequenceNumber
	Reason         string
	Cause          error
}

func (e *FatalError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Reason != "" {
		return fmt.Sprintf("fatal smpp protocol error: kind=%s declared_length=%d command=0x%08x sequence=%d reason=%s", e.Kind, e.DeclaredLength, uint32(e.Command), uint32(e.Sequence), e.Reason)
	}
	return fmt.Sprintf("fatal smpp protocol error: kind=%s declared_length=%d command=0x%08x sequence=%d", e.Kind, e.DeclaredLength, uint32(e.Command), uint32(e.Sequence))
}

func (e *FatalError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// Fatal always reports true and allows higher layers to distinguish structural
// failures from recoverable SMPP semantic/status errors.
func (e *FatalError) Fatal() bool { return true }
