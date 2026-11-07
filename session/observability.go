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
	// EventWindowUnderflow is emitted when the outbound window was released
	// without a matching acquisition. It always indicates a library defect and
	// should be alarmed on; window occupancy accounting is unreliable from that
	// point on, though the session keeps running.
	EventWindowUnderflow
	// EventPanicRecovered is emitted when the session recovered a panic instead
	// of letting it crash the process. Reason holds the *protocol.PanicError
	// text, which names the source; the stack is in the session's log. An
	// Observer that itself panicked is not told about its own panic.
	EventPanicRecovered
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
	case EventWindowUnderflow:
		return "window_underflow"
	case EventPanicRecovered:
		return "panic_recovered"
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
// owned copy of the complete, unredacted wire PDU. It may contain clear-text
// bind/outbind passwords, system IDs, addresses, message bodies, message IDs,
// arbitrary optional parameters and vendor TLVs. Treat the entire byte slice
// as secret material; the generic library cannot safely redact unknown or
// peer-specific fields. See PacketTracer for capture guidance.
type PacketTrace struct {
	Direction PacketDirection
	At        time.Time
	Header    codec.Header
	Length    int
	RawPDU    []byte
}

// PacketTracer receives optional packet traces synchronously on the session's
// RX or TX goroutine. It must return promptly and must not perform blocking I/O;
// the session does not add a worker or queue. Packet tracing is disabled by
// default and is intentionally outside the base performance target.
//
// SECURITY: Config.TraceRawPDU is not a redaction mode. When enabled, every
// trace carries an owned copy of the complete, unredacted wire PDU. SMPP bind
// and outbind passwords are sent in clear text; other fields can contain
// system IDs, source/destination addresses, message bodies, message IDs,
// optional parameters and arbitrary vendor TLVs. A generic library cannot
// reliably identify every sensitive vendor/peer field, so treat every RawPDU
// as secret material. Do not write raw traces to ordinary logs, shared
// telemetry, support bundles or third-party services.
//
// Prefer the default metadata-only trace (direction, timestamp, header and
// length). If raw capture is essential, use synthetic data where possible and
// a short-lived, isolated local capture with restricted access, encryption at
// rest and prompt deletion. Drop bind_transmitter, bind_receiver,
// bind_transceiver and outbind frames entirely before persistence; sanitize
// other frames with a peer-aware allowlist that removes addresses, message
// content, identifiers and unknown/vendor TLVs. If a trustworthy sanitizer is
// unavailable, discard RawPDU and retain only metadata. Do not rely on a
// fixed byte offset or password-only mask.
//
// With TraceRawPDU off (the default), RawPDU is nil and a configured tracer
// receives only metadata. Config.TraceRawPDU has no effect unless
// Config.PacketTracer is also non-nil. Library-owned logs and Observer events
// do not format caller-controlled panic or callback/decoder error text; see
// protocol.PanicError and docs/INTEROPERABILITY.md.
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
	// PanicsRecovered counts panics the session recovered instead of letting
	// them crash the process; see EventPanicRecovered. Any non-zero value is
	// worth an alarm: it is a bug in a callback, a net.Conn or this library.
	PanicsRecovered uint64
	Window          WindowSnapshot
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
	panicsRecovered      atomic.Uint64
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
	if !s.initialized() {
		return MetricsSnapshot{}
	}
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
		PanicsRecovered:      s.metrics.panicsRecovered.Load(),
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
	defer s.recoverCallback(sourceObserver)
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
	defer s.recoverCallback(sourcePacketTracer)
	s.config.PacketTracer.TracePacket(trace)
}

// reportWindowUnderflow is invoked by the request window when a release found
// no token to return. The window keeps running with degraded accounting; this
// hook exists so operators can alarm on the defect rather than discover it as
// a process crash.
func (s *Session) reportWindowUnderflow() {
	s.emitEvent(Event{
		Kind:   EventWindowUnderflow,
		At:     time.Now(),
		Reason: "outbound window released without acquisition",
	})
}
