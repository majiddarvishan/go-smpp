package protocol

import (
	"strings"
	"testing"
)

func TestProtocolErrorStringsOmitUntrustedReason(t *testing.T) {
	const secret = "PROTOCOL_REASON_SECRET_MARKER"
	semantic := &SemanticError{
		Status:   StatusInvalidParameterLength,
		Command:  CommandSubmitSM,
		Sequence: 7,
		Reason:   secret,
	}
	if got := semantic.Error(); strings.Contains(got, secret) {
		t.Fatalf("SemanticError.Error exposed untrusted Reason: %q", got)
	}
	if semantic.Reason != secret {
		t.Fatal("SemanticError.Reason should remain available for explicit inspection")
	}

	fatal := &FatalError{
		Kind:           FatalInvalidFieldLength,
		DeclaredLength: 64,
		Command:        CommandBindTransceiver,
		Sequence:       9,
		Reason:         secret,
	}
	if got := fatal.Error(); strings.Contains(got, secret) {
		t.Fatalf("FatalError.Error exposed untrusted Reason: %q", got)
	}
	if fatal.Reason != secret {
		t.Fatal("FatalError.Reason should remain available for explicit inspection")
	}
}
