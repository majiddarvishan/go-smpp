package session

import (
	"time"

	"github.com/majiddarvishan/go-smpp/protocol"
)

// Test-only convenience wrappers around deadlineManager.scheduleItemAt, the
// only scheduling entry point production code uses (tx.go and liveness.go).
// They used to live in deadline.go itself; since only tests called them, the
// production file no longer carries them (Finding C3).

func (m *deadlineManager) schedule(after time.Duration, kind TimeoutKind, command protocol.CommandID, sequence protocol.SequenceNumber) *deadlineItem {
	return m.scheduleItem(&deadlineItem{}, after, kind, command, sequence)
}

// scheduleItem inserts a caller-owned deadline record, stamped from the
// current time rather than from a write-completion timestamp.
func (m *deadlineManager) scheduleItem(item *deadlineItem, after time.Duration, kind TimeoutKind, command protocol.CommandID, sequence protocol.SequenceNumber) *deadlineItem {
	return m.scheduleItemAt(item, time.Now(), after, kind, command, sequence)
}
