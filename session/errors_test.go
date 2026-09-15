package session

import (
	"strings"
	"testing"
	"time"

	"github.com/majiddarvishan/go-smpp/protocol"
)

func TestTimeoutError(t *testing.T) {
	err := &TimeoutError{
		Kind:     TimeoutResponse,
		Command:  protocol.CommandSubmitSM,
		Sequence: 42,
		After:    5 * time.Second,
	}
	if !err.Timeout() {
		t.Fatal("TimeoutError must report timeout=true")
	}
	if !strings.Contains(err.Error(), "response") || !strings.Contains(err.Error(), "sequence=42") {
		t.Fatalf("unexpected timeout error string: %q", err.Error())
	}
}
