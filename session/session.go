package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/protocol"
)

const (
	DefaultMaxPending          = 1024
	DefaultTXQueueSize         = 1024
	DefaultTXBatchItems        = 32
	DefaultTXBatchBytes        = 64 << 10
	DefaultReadBufferSize      = 64 << 10
	DefaultResponseTimeout     = 30 * time.Second
	DefaultSessionInitTimeout  = 30 * time.Second
	DefaultEnquireLinkInterval = 30 * time.Second
	DefaultEnquireLinkTimeout  = 10 * time.Second
	DefaultInactivityTimeout   = 2 * time.Minute
	DefaultWriteTimeout        = 30 * time.Second
)

// Response is returned by an inbound request Handler. The response command ID
// and sequence number are derived from the request so handlers cannot
// accidentally break correlation.
type Response struct {
	Status protocol.CommandStatus
	Body   any
}

// InboundPDU is the session-layer view of one decoded inbound PDU. It aliases
// codec.DecodedPDU so higher-level client/server packages can implement session
// handlers without depending directly on the lower codec package. Borrowed body
// slices retain the same lifetime rules as codec.DecodedPDU.
type InboundPDU = codec.DecodedPDU

// Handler processes an inbound request PDU. The PDU may contain borrowed views
// into the receive frame and must not be retained after Handle returns unless
// the application copies the data it needs.
type Handler interface {
	Handle(context.Context, *Session, InboundPDU) (Response, error)
}

type HandlerFunc func(context.Context, *Session, InboundPDU) (Response, error)

func (f HandlerFunc) Handle(ctx context.Context, s *Session, pdu InboundPDU) (Response, error) {
	return f(ctx, s, pdu)
}

// Config configures one active SMPP session. The registry is immutable while a
// session is active. WindowSize is the protocol outstanding-request admission
// bound; MaxPending is a defensive correlation-table ceiling and is raised to
// WindowSize automatically when necessary.
type Config struct {
	Role                Role
	Profile             protocol.Profile
	Registry            *codec.Registry
	MaxPDUSize          uint32
	MaxPending          int
	WindowSize          int
	WindowObserver      WindowObserver
	FlowController      FlowController
	Observer            Observer
	PacketTracer        PacketTracer
	TraceRawPDU         bool
	ResponseTimeout     time.Duration
	SessionInitTimeout  time.Duration
	EnquireLinkInterval time.Duration
	EnquireLinkTimeout  time.Duration
	InactivityTimeout   time.Duration
	WriteTimeout        time.Duration
	TXQueueSize         int
	TXBatchItems        int
	TXBatchBytes        int
	ReadBufferSize      int
	Handler             Handler
	Logger              *slog.Logger
}

const (
	txRequest txKind = iota + 1
	txResponse
	txOneWay
)

// Session owns one TCP-compatible stream and runs independent long-lived RX and
// TX paths. Its exported methods are safe for concurrent use.
type Session struct {
	conn     net.Conn
	registry *codec.Registry
	framer   *codec.Framer
	machine  *StateMachine
	config   Config
	logger   *slog.Logger

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	tx     chan txItem

	pending   *pendingTable
	window    *requestWindow
	deadlines *deadlineManager
	seq       sequenceGenerator
	frames    *framePool

	// Caller-owned deadline slots for the three liveness checks (Task 2.4,
	// Finding P5), each rescheduled in place via deadlineManager.scheduleItemAt
	// rather than reallocated. Only ever touched from deadlines.run()'s single
	// goroutine (the initial schedule in New happens before that goroutine
	// starts, and nothing else ever schedules or cancels these three), so —
	// unlike pendingRequest's deadlineStorage — they need no mutex of their
	// own; there is no concurrent canceller to race against.
	sessionInitDeadline deadlineItem
	inactivityDeadline  deadlineItem
	enquireLinkDeadline deadlineItem

	createdAt    time.Time
	lastActivity atomic.Int64
	peerCaps     atomic.Value // protocol.PeerCapabilities
	metrics      sessionMetrics

	closeOnce sync.Once
	wg        sync.WaitGroup
	errMu     sync.RWMutex
	closeErr  error
}

func New(conn net.Conn, config Config) (*Session, error) {
	if conn == nil || !config.Role.valid() {
		return nil, ErrInvalidConfig
	}
	if config.Profile.InterfaceVersion == 0 {
		config.Profile = protocol.SMPP34Profile()
	}
	if !config.Profile.Valid() {
		return nil, fmt.Errorf("%w: unsupported local SMPP profile 0x%02x", ErrInvalidConfig, byte(config.Profile.InterfaceVersion))
	}
	if config.Registry == nil {
		var (
			registry *codec.Registry
			err      error
		)
		if config.Profile.IsSMPP50() {
			registry, err = codec.NewSMPP50Registry(codec.RegistryCompatible)
		} else {
			registry, err = codec.NewSMPP34Registry(codec.RegistryCompatible)
		}
		if err != nil {
			return nil, err
		}
		config.Registry = registry
	}
	if config.WindowSize <= 0 {
		config.WindowSize = DefaultWindowSize
	}
	if config.MaxPending <= 0 {
		config.MaxPending = DefaultMaxPending
	}
	if config.MaxPending < config.WindowSize {
		config.MaxPending = config.WindowSize
	}
	config.ResponseTimeout = defaultDuration(config.ResponseTimeout, DefaultResponseTimeout)
	config.SessionInitTimeout = defaultDuration(config.SessionInitTimeout, DefaultSessionInitTimeout)
	config.EnquireLinkInterval = defaultDuration(config.EnquireLinkInterval, DefaultEnquireLinkInterval)
	config.EnquireLinkTimeout = defaultDuration(config.EnquireLinkTimeout, DefaultEnquireLinkTimeout)
	config.InactivityTimeout = defaultDuration(config.InactivityTimeout, DefaultInactivityTimeout)
	config.WriteTimeout = defaultDuration(config.WriteTimeout, DefaultWriteTimeout)
	if config.TXQueueSize <= 0 {
		config.TXQueueSize = DefaultTXQueueSize
	}
	if config.TXBatchItems <= 0 {
		config.TXBatchItems = DefaultTXBatchItems
	}
	if config.TXBatchBytes <= 0 {
		config.TXBatchBytes = DefaultTXBatchBytes
	}
	if config.ReadBufferSize <= 0 {
		config.ReadBufferSize = DefaultReadBufferSize
	}
	framer, err := codec.NewFramer(config.MaxPDUSize)
	if err != nil {
		return nil, err
	}
	machine, err := NewStateMachine(config.Role)
	if err != nil {
		return nil, err
	}
	if err := machine.Open(); err != nil {
		return nil, err
	}
	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	now := time.Now()
	s := &Session{
		conn: conn, registry: config.Registry, framer: framer, machine: machine,
		config: config, logger: logger, ctx: ctx, cancel: cancel,
		done: make(chan struct{}), tx: make(chan txItem, config.TXQueueSize),
		pending:   newPendingTable(config.MaxPending),
		window:    newRequestWindow(config.WindowSize, config.WindowObserver),
		frames:    newFramePool(),
		createdAt: now,
	}
	s.lastActivity.Store(now.UnixNano())
	s.window.onUnderflow = s.reportWindowUnderflow
	s.peerCaps.Store(protocol.PeerCapabilities{})
	s.deadlines = newDeadlineManager(s.done, s.expireDeadline)
	s.scheduleLivenessDeadlines(now)
	s.wg.Add(3)
	go s.rxLoop()
	go s.txLoop()
	go func() { defer s.wg.Done(); s.deadlines.run() }()
	return s, nil
}

func (s *Session) Role() Role                   { return s.machine.Role() }
func (s *Session) State() protocol.SessionState { return s.machine.State() }
func (s *Session) BindMode() BindMode           { return s.machine.BindMode() }
func (s *Session) Done() <-chan struct{}        { return s.done }
func (s *Session) Pending() int                 { return s.pending.len() }
func (s *Session) Profile() protocol.Profile    { return s.config.Profile }

// PeerCapabilities returns the current negotiated remote-peer capabilities.
// Before a successful bind it returns the zero value.
func (s *Session) PeerCapabilities() protocol.PeerCapabilities {
	v := s.peerCaps.Load()
	if v == nil {
		return protocol.PeerCapabilities{}
	}
	return v.(protocol.PeerCapabilities)
}

// Window returns current bounded-outstanding-request utilization.
func (s *Session) Window() WindowSnapshot { return s.window.snapshot() }
func (s *Session) LocalAddr() net.Addr    { return s.conn.LocalAddr() }
func (s *Session) RemoteAddr() net.Addr   { return s.conn.RemoteAddr() }

// Err returns the terminal session error after Done is closed.
func (s *Session) Err() error {
	s.errMu.RLock()
	err := s.closeErr
	s.errMu.RUnlock()
	return err
}

// Wait blocks until both long-lived transport loops have exited.
func (s *Session) Wait() { s.wg.Wait() }

// Close is idempotent and safe to call concurrently, including from a Handler.
// It initiates shutdown without waiting for the RX/TX goroutines; use Wait when
// a caller needs to join them.
func (s *Session) Close() error {
	s.terminate(ErrSessionClosed)
	return nil
}

// Request synchronously waits for one response while the underlying session
// remains asynchronous and may carry many other requests concurrently. If the
// outstanding request window is full, Request waits until capacity is released,
// the caller context ends, or the session closes.
func (s *Session) Request(ctx context.Context, command protocol.CommandID, body any) (codec.DecodedPDU, error) {
	return s.request(ctx, command, body, true)
}

// TryRequest is the non-blocking admission variant. It returns ErrWindowFull
// instead of waiting when the configured outstanding request window is full.
func (s *Session) TryRequest(ctx context.Context, command protocol.CommandID, body any) (codec.DecodedPDU, error) {
	return s.request(ctx, command, body, false)
}

func (s *Session) request(ctx context.Context, command protocol.CommandID, body any, waitWindow bool) (codec.DecodedPDU, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if command.IsResponse() || isOneWayCommand(command) {
		return codec.DecodedPDU{}, fmt.Errorf("%w: command 0x%08x does not have synchronous request semantics", ErrUnexpectedPDU, uint32(command))
	}
	if isBindRequest(command) {
		if version, ok := bindRequestVersion(body); ok && version == protocol.InterfaceVersion50 && !s.config.Profile.IsSMPP50() {
			return codec.DecodedPDU{}, fmt.Errorf("%w: local profile 0x%02x cannot advertise SMPP 5.0", ErrUnsupportedCapability, byte(s.config.Profile.InterfaceVersion))
		}
	}
	if requiresSMPP50(command) && !s.PeerCapabilities().SupportsSMPP50 {
		return codec.DecodedPDU{}, fmt.Errorf("%w: command 0x%08x requires SMPP 5.0", ErrUnsupportedCapability, uint32(command))
	}
	select {
	case <-s.done:
		return codec.DecodedPDU{}, s.requestCloseError()
	default:
	}
	if err := s.window.acquire(ctx, s.done, waitWindow); err != nil {
		if errors.Is(err, ErrSessionClosed) {
			return codec.DecodedPDU{}, s.requestCloseError()
		}
		return codec.DecodedPDU{}, err
	}
	windowOwned := true
	defer func() {
		if windowOwned {
			_ = s.window.release()
		}
	}()

	if err := s.machine.BeginOutbound(command); err != nil {
		return codec.DecodedPDU{}, err
	}
	reserved := true
	defer func() {
		if reserved {
			s.machine.CancelOutbound(command)
		}
	}()

	// The completion channel is allocated fresh per request rather than
	// pooled. Session.completions used to hold recycled channels, but pooling
	// them was only safe if nothing could still be mid-send on a channel at
	// the moment it went back in the pool — an invariant spread across this
	// function, pendingTable, handleResponse, and the deadline manager,
	// enforced by nothing mechanical here. Against the other allocation work
	// in this codebase, one 1-element channel per request is a rounding
	// error, so the correctness risk was not worth carrying (Finding B4).
	done := make(chan requestResult, 1)
	request := &pendingRequest{requestID: command, expectedID: command.ResponseID(), done: done, releaseWindow: func() { _ = s.window.release() }}
	if isBindRequest(command) {
		switch typed := body.(type) {
		case protocol.BindRequest:
			request.bindVersion = typed.InterfaceVersion
		case *protocol.BindRequest:
			if typed != nil {
				request.bindVersion = typed.InterfaceVersion
			}
		}
	}
	var sequence protocol.SequenceNumber
	inserted := false
	for attempts := 0; attempts <= s.config.MaxPending; attempts++ {
		sequence = s.seq.Next()
		err := s.pending.insert(sequence, request)
		if err == nil {
			inserted = true
			break
		}
		if errors.Is(err, ErrSequenceInUse) {
			continue
		}
		return codec.DecodedPDU{}, err
	}
	if !inserted {
		return codec.DecodedPDU{}, ErrSequenceExhausted
	}
	windowOwned = false // pendingRequest now owns the slot until terminal removal.

	header := codec.Header{CommandID: command, CommandStatus: protocol.StatusOK, SequenceNumber: sequence}
	hint, _ := codec.EncodedPDUSizeHint(command, body)
	framePtr := s.frames.get(hint)
	frame, err := codec.EncodePDU(*framePtr, header, body, s.registry)
	if err != nil {
		s.frames.put(framePtr)
		_, _ = s.pending.completeError(sequence, err)
		return codec.DecodedPDU{}, err
	}
	*framePtr = frame

	item := txItem{kind: txRequest, header: header, frame: frame, framePtr: framePtr, sequence: sequence, requestID: command, pending: request}
	select {
	case s.tx <- item:
		reserved = false
	case <-ctx.Done():
		s.frames.put(framePtr)
		if _, won := s.pending.completeError(sequence, ctx.Err()); won {
			return codec.DecodedPDU{}, ctx.Err()
		}
		reserved = false
		return s.waitCompleted(request)
	case <-s.done:
		s.frames.put(framePtr)
		reserved = false
		return s.waitCompleted(request)
	}

	select {
	case result := <-request.done:
		return result.pdu, result.err
	case <-ctx.Done():
		if _, won := s.pending.completeError(sequence, ctx.Err()); won {
			s.machine.CancelOutbound(command)
			return codec.DecodedPDU{}, ctx.Err()
		}
		return s.waitCompleted(request)
	}
}

// SendOneWay dispatches an SMPP request primitive that has no response PDU.
// The context governs admission to the TX queue. Once admitted, the call waits
// until the complete frame has either been written or the session is lost, so
// callers never receive context cancellation while the library is still
// ambiguously deciding whether to put a one-way lifecycle PDU on the wire.
func (s *Session) SendOneWay(ctx context.Context, command protocol.CommandID, body any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if !isOneWayCommand(command) {
		return fmt.Errorf("%w: command 0x%08x is not a one-way SMPP primitive", ErrUnexpectedPDU, uint32(command))
	}
	select {
	case <-s.done:
		return s.requestCloseError()
	default:
	}
	if err := s.machine.BeginOutbound(command); err != nil {
		return err
	}
	reserved := true
	defer func() {
		if reserved {
			s.machine.CancelOutbound(command)
		}
	}()

	sequence := s.seq.Next()
	header := codec.Header{CommandID: command, CommandStatus: protocol.StatusOK, SequenceNumber: sequence}
	hint, _ := codec.EncodedPDUSizeHint(command, body)
	framePtr := s.frames.get(hint)
	frame, err := codec.EncodePDU(*framePtr, header, body, s.registry)
	if err != nil {
		s.frames.put(framePtr)
		return err
	}
	*framePtr = frame
	dispatched := make(chan error, 1)
	item := txItem{kind: txOneWay, header: header, frame: frame, framePtr: framePtr, sequence: sequence, requestID: command, dispatched: dispatched}
	select {
	case s.tx <- item:
		reserved = false
	case <-ctx.Done():
		s.frames.put(framePtr)
		return ctx.Err()
	case <-s.done:
		s.frames.put(framePtr)
		return s.requestCloseError()
	}

	select {
	case err := <-dispatched:
		return err
	case <-s.done:
		return s.requestCloseError()
	}
}

func (s *Session) waitCompleted(request *pendingRequest) (codec.DecodedPDU, error) {
	result := <-request.done
	return result.pdu, result.err
}

func defaultDuration(value, fallback time.Duration) time.Duration {
	if value == 0 {
		return fallback
	}
	if value < 0 {
		return 0
	}
	return value
}

func (s *Session) responseTimeoutFor(command protocol.CommandID) (time.Duration, TimeoutKind) {
	if command == protocol.CommandEnquireLink {
		if s.config.EnquireLinkTimeout > 0 {
			return s.config.EnquireLinkTimeout, TimeoutEnquireLink
		}
		return s.config.ResponseTimeout, TimeoutEnquireLink
	}
	return s.config.ResponseTimeout, TimeoutResponse
}

func (s *Session) terminate(cause error) {
	if cause == nil {
		cause = ErrSessionClosed
	}
	s.closeOnce.Do(func() {
		var fatal *protocol.FatalError
		if errors.As(cause, &fatal) {
			s.metrics.decodeFailures.Add(1)
			s.metrics.fatalProtocolErrors.Add(1)
			s.emitEvent(Event{Kind: EventDecodeFailure, Command: fatal.Command, Sequence: fatal.Sequence, FatalKind: fatal.Kind, Reason: fatal.Reason})
			s.emitEvent(Event{Kind: EventFatalProtocolError, Command: fatal.Command, Sequence: fatal.Sequence, FatalKind: fatal.Kind, Reason: fatal.Reason})
			s.logFatal(fatal)
		}
		s.errMu.Lock()
		s.closeErr = cause
		s.errMu.Unlock()
		s.machine.Close()
		s.cancel()
		close(s.done)
		_ = s.conn.Close()
		pendingErr := error(&LossError{Cause: cause})
		if errors.Is(cause, ErrSessionClosed) {
			pendingErr = ErrSessionClosed
		}
		s.pending.failAll(pendingErr)
	})
}

func (s *Session) requestCloseError() error {
	err := s.Err()
	if err == nil || errors.Is(err, ErrSessionClosed) {
		return ErrSessionClosed
	}
	return &LossError{Cause: err}
}

func (s *Session) logFatal(err *protocol.FatalError) {
	attrs := []any{
		"event", "smpp_protocol_fatal",
		"reason", err.Kind.String(),
		"state", s.State().String(),
		"role", s.Role().String(),
		"declared_length", err.DeclaredLength,
		"command_id", fmt.Sprintf("0x%08x", uint32(err.Command)),
		"sequence_number", uint32(err.Sequence),
		"action", "connection_closed",
	}
	if local := s.conn.LocalAddr(); local != nil {
		attrs = append(attrs, "local", local.String())
	}
	if remote := s.conn.RemoteAddr(); remote != nil {
		attrs = append(attrs, "remote", remote.String())
	}
	if err.Reason != "" {
		attrs = append(attrs, "detail", err.Reason)
	}
	s.logger.Error("fatal SMPP protocol error; closing TCP connection", attrs...)
}
