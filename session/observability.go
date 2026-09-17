package session

import (
	"sync/atomic"
	"time"

	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/protocol"
)

// EventKind identifies a low-overhead session event emitted only when an
// Observer is configured. Per-PDU event callbacks are opt-in and are not part of
// the default hot path.
type EventKind uint8

const (
	EventRequestSent EventKind = iota + 1
	EventRequestReceived
	EventResponseSent
	EventResponseReceived
	EventResponseTimeout
	EventSessionInitTimeout
	EventInactivityTimeout
	EventEnquireLinkSent
	EventEnquireLinkReceived
	EventEnquireLinkTimeout
	EventDecodeFailure
	EventFatalProtocolError
	EventCongestionState
	EventResponseRTT
)

func (k EventKind) String() string {
	switch k {
	case EventRequestSent:
		return "request_sent"
	case EventRequestReceived:
		return "request_received"
	case EventResponseSent:
		return "response_sent"
	case EventResponseReceived:
		return "response_received"
	case EventResponseTimeout:
		return "response_timeout"
	case EventSessionInitTimeout:
		return "session_init_timeout"
	case EventInactivityTimeout:
		return "inactivity_timeout"
	case EventEnquireLinkSent:
		return "enquire_link_sent"
	case EventEnquireLinkReceived:
		return "enquire_link_received"
	case EventEnquireLinkTimeout:
		return "enquire_link_timeout"
	case EventDecodeFailure:
		return "decode_failure"
	case EventFatalProtocolError:
		return "fatal_protocol_error"
	case EventCongestionState:
		return "congestion_state"
	case EventResponseRTT:
		return "response_rtt"
	default:
		return "unknown"
	}
}

// Event is the common observability hook payload. Fields not relevant to a
// particular Kind are left at their zero value. Observer callbacks execute on
// the session goroutine that produced the event and therefore must return
// quickly and must not perform blocking calls back into the same Session.
type Event struct {
	Kind       EventKind
	At         time.Time
	Command    protocol.CommandID
	Sequence   protocol.SequenceNumber
	Status     protocol.CommandStatus
	Timeout    TimeoutKind
	Duration   time.Duration
	Congestion protocol.CongestionState
	FatalKind  protocol.FatalErrorKind
	Reason     string
}

// Observer receives opt-in session events. A nil Observer has no callback cost
// beyond one branch on event sites.
type Observer interface {
	OnEvent(Event)
}

// ObserverFunc adapts a function to Observer.
type ObserverFunc func(Event)

func (f ObserverFunc) OnEvent(event Event) {
	if f != nil {
		f(event)
	}
}

// PacketDirection identifies a packet trace direction.
type PacketDirection uint8

const (
	PacketInbound PacketDirection = iota + 1
	PacketOutbound
)

// PacketTrace is emitted only when Config.PacketTracer is non-nil. RawPDU is
// nil by default. If Config.TraceRawPDU is explicitly enabled, RawPDU is an
// owned copy of the complete wire PDU and may contain credentials or message
// content; callers are responsible for protecting it appropriately.
type PacketTrace struct {
	Direction PacketDirection
	At        time.Time
	Header    codec.Header
	Length    int
	RawPDU    []byte
}

// PacketTracer receives optional packet traces. Packet tracing is disabled by
// default and is intentionally outside the base performance target.
type PacketTracer interface {
	TracePacket(PacketTrace)
}

// PacketTracerFunc adapts a function to PacketTracer.
type PacketTracerFunc func(PacketTrace)

func (f PacketTracerFunc) TracePacket(trace PacketTrace) {
	if f != nil {
		f(trace)
	}
}

// MetricsSnapshot is a lock-free point-in-time view of session counters. The
// counters are monotonically increasing for the lifetime of one Session.
type MetricsSnapshot struct {
	RequestsSent         uint64
	RequestsReceived     uint64
	ResponsesSent        uint64
	ResponsesReceived    uint64
	ResponseTimeouts     uint64
	SessionInitTimeouts  uint64
	InactivityTimeouts   uint64
	EnquireLinkSent      uint64
	EnquireLinkReceived  uint64
	EnquireLinkResponses uint64
	EnquireLinkTimeouts  uint64
	DecodeFailures       uint64
	FatalProtocolErrors  uint64
	ResponseRTTSamples   uint64
	ResponseRTTTotal     time.Duration
	ResponseRTTLast      time.Duration
	ResponseRTTMax       time.Duration
	CongestionSamples    uint64
	LastCongestionState  protocol.CongestionState
	Window               WindowSnapshot
}

type sessionMetrics struct {
	requestsSent         atomic.Uint64
	requestsReceived     atomic.Uint64
	responsesSent        atomic.Uint64
	responsesReceived    atomic.Uint64
	responseTimeouts     atomic.Uint64
	sessionInitTimeouts  atomic.Uint64
	inactivityTimeouts   atomic.Uint64
	enquireLinkSent      atomic.Uint64
	enquireLinkReceived  atomic.Uint64
	enquireLinkResponses atomic.Uint64
	enquireLinkTimeouts  atomic.Uint64
	decodeFailures       atomic.Uint64
	fatalProtocolErrors  atomic.Uint64
	rttSamples           atomic.Uint64
	rttTotalNS           atomic.Int64
	rttLastNS            atomic.Int64
	rttMaxNS             atomic.Int64
	congestionSamples    atomic.Uint64
	lastCongestion       atomic.Uint32
}

func (m *sessionMetrics) noteRTT(duration time.Duration) {
	if duration < 0 {
		return
	}
	value := int64(duration)
	m.rttSamples.Add(1)
	m.rttTotalNS.Add(value)
	m.rttLastNS.Store(value)
	for {
		old := m.rttMaxNS.Load()
		if value <= old || m.rttMaxNS.CompareAndSwap(old, value) {
			return
		}
	}
}

func (m *sessionMetrics) noteCongestion(state protocol.CongestionState) {
	m.lastCongestion.Store(uint32(state))
	m.congestionSamples.Add(1)
}

func (s *Session) Metrics() MetricsSnapshot {
	return MetricsSnapshot{
		RequestsSent:         s.metrics.requestsSent.Load(),
		RequestsReceived:     s.metrics.requestsReceived.Load(),
		ResponsesSent:        s.metrics.responsesSent.Load(),
		ResponsesReceived:    s.metrics.responsesReceived.Load(),
		ResponseTimeouts:     s.metrics.responseTimeouts.Load(),
		SessionInitTimeouts:  s.metrics.sessionInitTimeouts.Load(),
		InactivityTimeouts:   s.metrics.inactivityTimeouts.Load(),
		EnquireLinkSent:      s.metrics.enquireLinkSent.Load(),
		EnquireLinkReceived:  s.metrics.enquireLinkReceived.Load(),
		EnquireLinkResponses: s.metrics.enquireLinkResponses.Load(),
		EnquireLinkTimeouts:  s.metrics.enquireLinkTimeouts.Load(),
		DecodeFailures:       s.metrics.decodeFailures.Load(),
		FatalProtocolErrors:  s.metrics.fatalProtocolErrors.Load(),
		ResponseRTTSamples:   s.metrics.rttSamples.Load(),
		ResponseRTTTotal:     time.Duration(s.metrics.rttTotalNS.Load()),
		ResponseRTTLast:      time.Duration(s.metrics.rttLastNS.Load()),
		ResponseRTTMax:       time.Duration(s.metrics.rttMaxNS.Load()),
		CongestionSamples:    s.metrics.congestionSamples.Load(),
		LastCongestionState:  protocol.CongestionState(s.metrics.lastCongestion.Load()),
		Window:               s.window.snapshot(),
	}
}

func (s *Session) emitEvent(event Event) {
	if s.config.Observer == nil {
		return
	}
	if event.At.IsZero() {
		event.At = time.Now()
	}
	s.config.Observer.OnEvent(event)
}

func (s *Session) tracePacket(direction PacketDirection, header codec.Header, frame []byte) {
	if s.config.PacketTracer == nil {
		return
	}
	trace := PacketTrace{Direction: direction, At: time.Now(), Header: header, Length: len(frame)}
	if s.config.TraceRawPDU {
		trace.RawPDU = append([]byte(nil), frame...)
	}
	s.config.PacketTracer.TracePacket(trace)
}
