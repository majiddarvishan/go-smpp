package session

import (
	"time"

	"github.com/majiddarvishan/go-smpp/protocol"
)

// CongestionEvent reports SMPP 5.0 congestion feedback received in a response.
// Implementations should return quickly: the callback runs on the session RX
// path so applications can connect it to their own rate controller without the
// library imposing a second, hidden queue.
type CongestionEvent struct {
	State    protocol.CongestionState
	Command  protocol.CommandID
	Sequence protocol.SequenceNumber
	At       time.Time
}

// FlowController is the SMPP 5.0 adaptive flow-control extension point. Window
// size remains a hard bound on in-flight requests; congestion feedback is a
// separate rate-control signal as defined by SMPP 5.0.
type FlowController interface {
	OnCongestion(CongestionEvent)
}

// FlowControllerFunc adapts a function to FlowController.
type FlowControllerFunc func(CongestionEvent)

func (f FlowControllerFunc) OnCongestion(event CongestionEvent) {
	if f != nil {
		f(event)
	}
}
