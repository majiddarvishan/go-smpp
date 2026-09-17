package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/transport"
)

const (
	DefaultMaxPending          = 1024
	DefaultTXQueueSize         = 1024
	DefaultTXBatchItems        = 1
	DefaultTXBatchBytes        = 64 << 10
	DefaultReadBufferSize      = 64 << 10
	DefaultResponseTimeout     = 30 * time.Second
	DefaultSessionInitTimeout  = 30 * time.Second
	DefaultEnquireLinkInterval = 30 * time.Second
	DefaultEnquireLinkTimeout  = 10 * time.Second
	DefaultInactivityTimeout   = 2 * time.Minute
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
	TXQueueSize         int
	TXBatchItems        int
	TXBatchBytes        int
	ReadBufferSize      int
	Handler             Handler
	Logger              *slog.Logger
}

type txKind uint8

const (
	txRequest txKind = iota + 1
	txResponse
	txOneWay
)

type txItem struct {
	kind       txKind
	header     codec.Header
	frame      []byte
	sequence   protocol.SequenceNumber
	requestID  protocol.CommandID
	responseTo protocol.CommandID
	status     protocol.CommandStatus
	pending    *pendingRequest
	dispatched chan error
	buffer     *txFrameBuffer
}

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
	frames    *txFramePool
	seq       sequenceGenerator

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
		frames:    newTXFramePool(),
		createdAt: now,
	}
	s.lastActivity.Store(now.UnixNano())
	s.peerCaps.Store(protocol.PeerCapabilities{})
	s.deadlines = newDeadlineManager(s.done, s.expireDeadline)
	s.wg.Add(4)
	go s.rxLoop()
	go s.txLoop()
	go func() { defer s.wg.Done(); s.deadlines.run() }()
	go s.livenessLoop()
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
			s.window.release()
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

	request := &pendingRequest{requestID: command, expectedID: command.ResponseID(), done: make(chan requestResult, 1), releaseWindow: s.window.release}
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
	frame, frameBuffer, err := s.encodeTXPDU(header, body)
	if err != nil {
		_, _ = s.pending.completeError(sequence, err)
		return codec.DecodedPDU{}, err
	}

	item := txItem{kind: txRequest, header: header, frame: frame, sequence: sequence, requestID: command, pending: request, buffer: frameBuffer}
	select {
	case s.tx <- item:
		reserved = false
	case <-ctx.Done():
		s.releaseTXBuffer(frameBuffer)
		if _, won := s.pending.completeError(sequence, ctx.Err()); won {
			return codec.DecodedPDU{}, ctx.Err()
		}
		reserved = false
		return s.waitCompleted(request)
	case <-s.done:
		s.releaseTXBuffer(frameBuffer)
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
	frame, frameBuffer, err := s.encodeTXPDU(header, body)
	if err != nil {
		return err
	}
	dispatched := make(chan error, 1)
	item := txItem{kind: txOneWay, header: header, frame: frame, sequence: sequence, requestID: command, dispatched: dispatched, buffer: frameBuffer}
	select {
	case s.tx <- item:
		reserved = false
	case <-ctx.Done():
		s.releaseTXBuffer(frameBuffer)
		return ctx.Err()
	case <-s.done:
		s.releaseTXBuffer(frameBuffer)
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

func (s *Session) BindTransmitter(ctx context.Context, request protocol.BindRequest) (protocol.BindResponse, error) {
	return s.bind(ctx, protocol.CommandBindTransmitter, request)
}

func (s *Session) BindReceiver(ctx context.Context, request protocol.BindRequest) (protocol.BindResponse, error) {
	return s.bind(ctx, protocol.CommandBindReceiver, request)
}

func (s *Session) BindTransceiver(ctx context.Context, request protocol.BindRequest) (protocol.BindResponse, error) {
	return s.bind(ctx, protocol.CommandBindTransceiver, request)
}

func (s *Session) bind(ctx context.Context, command protocol.CommandID, request protocol.BindRequest) (protocol.BindResponse, error) {
	pdu, err := s.Request(ctx, command, request)
	if err != nil {
		return protocol.BindResponse{}, err
	}
	if err := responseError(pdu); err != nil {
		return protocol.BindResponse{}, err
	}
	body, ok := pdu.Body.(protocol.BindResponse)
	if !ok {
		return protocol.BindResponse{}, fmt.Errorf("%w: bind response body %T", ErrUnexpectedPDU, pdu.Body)
	}
	return body, nil
}

func (s *Session) SubmitSM(ctx context.Context, request protocol.SubmitSM) (protocol.SubmitSMResp, error) {
	pdu, err := s.Request(ctx, protocol.CommandSubmitSM, request)
	if err != nil {
		return protocol.SubmitSMResp{}, err
	}
	if err := responseError(pdu); err != nil {
		return protocol.SubmitSMResp{}, err
	}
	body, ok := pdu.Body.(protocol.SubmitSMResp)
	if !ok {
		return protocol.SubmitSMResp{}, fmt.Errorf("%w: submit_sm_resp body %T", ErrUnexpectedPDU, pdu.Body)
	}
	return body, nil
}

func (s *Session) DeliverSM(ctx context.Context, request protocol.DeliverSM) (protocol.DeliverSMResp, error) {
	pdu, err := s.Request(ctx, protocol.CommandDeliverSM, request)
	if err != nil {
		return protocol.DeliverSMResp{}, err
	}
	if err := responseError(pdu); err != nil {
		return protocol.DeliverSMResp{}, err
	}
	body, ok := pdu.Body.(protocol.DeliverSMResp)
	if !ok {
		return protocol.DeliverSMResp{}, fmt.Errorf("%w: deliver_sm_resp body %T", ErrUnexpectedPDU, pdu.Body)
	}
	return body, nil
}

func (s *Session) DataSM(ctx context.Context, request protocol.DataSM) (protocol.DataSMResp, error) {
	pdu, err := s.Request(ctx, protocol.CommandDataSM, request)
	if err != nil {
		return protocol.DataSMResp{}, err
	}
	if err := responseError(pdu); err != nil {
		return protocol.DataSMResp{}, err
	}
	body, ok := pdu.Body.(protocol.DataSMResp)
	if !ok {
		return protocol.DataSMResp{}, fmt.Errorf("%w: data_sm_resp body %T", ErrUnexpectedPDU, pdu.Body)
	}
	return body, nil
}

func (s *Session) SubmitMulti(ctx context.Context, request protocol.SubmitMulti) (protocol.SubmitMultiResp, error) {
	pdu, err := s.Request(ctx, protocol.CommandSubmitMulti, request)
	if err != nil {
		return protocol.SubmitMultiResp{}, err
	}
	if err := responseError(pdu); err != nil {
		return protocol.SubmitMultiResp{}, err
	}
	body, ok := pdu.Body.(protocol.SubmitMultiResp)
	if !ok {
		return protocol.SubmitMultiResp{}, fmt.Errorf("%w: submit_multi_resp body %T", ErrUnexpectedPDU, pdu.Body)
	}
	return body, nil
}

func (s *Session) QuerySM(ctx context.Context, request protocol.QuerySM) (protocol.QuerySMResp, error) {
	pdu, err := s.Request(ctx, protocol.CommandQuerySM, request)
	if err != nil {
		return protocol.QuerySMResp{}, err
	}
	if err := responseError(pdu); err != nil {
		return protocol.QuerySMResp{}, err
	}
	body, ok := pdu.Body.(protocol.QuerySMResp)
	if !ok {
		return protocol.QuerySMResp{}, fmt.Errorf("%w: query_sm_resp body %T", ErrUnexpectedPDU, pdu.Body)
	}
	return body, nil
}

func (s *Session) CancelSM(ctx context.Context, request protocol.CancelSM) error {
	pdu, err := s.Request(ctx, protocol.CommandCancelSM, request)
	if err != nil {
		return err
	}
	return responseError(pdu)
}

func (s *Session) ReplaceSM(ctx context.Context, request protocol.ReplaceSM) error {
	pdu, err := s.Request(ctx, protocol.CommandReplaceSM, request)
	if err != nil {
		return err
	}
	return responseError(pdu)
}

func (s *Session) BroadcastSM(ctx context.Context, request protocol.BroadcastSM) (protocol.BroadcastSMResp, error) {
	pdu, err := s.Request(ctx, protocol.CommandBroadcastSM, request)
	if err != nil {
		return protocol.BroadcastSMResp{}, err
	}
	if err := responseError(pdu); err != nil {
		return protocol.BroadcastSMResp{}, err
	}
	body, ok := pdu.Body.(protocol.BroadcastSMResp)
	if !ok {
		return protocol.BroadcastSMResp{}, fmt.Errorf("%w: broadcast_sm_resp body %T", ErrUnexpectedPDU, pdu.Body)
	}
	return body, nil
}

func (s *Session) QueryBroadcastSM(ctx context.Context, request protocol.QueryBroadcastSM) (protocol.QueryBroadcastSMResp, error) {
	pdu, err := s.Request(ctx, protocol.CommandQueryBroadcastSM, request)
	if err != nil {
		return protocol.QueryBroadcastSMResp{}, err
	}
	if err := responseError(pdu); err != nil {
		return protocol.QueryBroadcastSMResp{}, err
	}
	body, ok := pdu.Body.(protocol.QueryBroadcastSMResp)
	if !ok {
		return protocol.QueryBroadcastSMResp{}, fmt.Errorf("%w: query_broadcast_sm_resp body %T", ErrUnexpectedPDU, pdu.Body)
	}
	return body, nil
}

func (s *Session) CancelBroadcastSM(ctx context.Context, request protocol.CancelBroadcastSM) error {
	pdu, err := s.Request(ctx, protocol.CommandCancelBroadcastSM, request)
	if err != nil {
		return err
	}
	return responseError(pdu)
}

func (s *Session) AlertNotification(ctx context.Context, notification protocol.AlertNotification) error {
	return s.SendOneWay(ctx, protocol.CommandAlertNotification, notification)
}

func (s *Session) Outbind(ctx context.Context, request protocol.Outbind) error {
	return s.SendOneWay(ctx, protocol.CommandOutbind, request)
}

func (s *Session) EnquireLink(ctx context.Context) error {
	pdu, err := s.Request(ctx, protocol.CommandEnquireLink, protocol.EmptyBody{})
	if err != nil {
		return err
	}
	return responseError(pdu)
}

func (s *Session) Unbind(ctx context.Context) error {
	pdu, err := s.Request(ctx, protocol.CommandUnbind, protocol.EmptyBody{})
	if err != nil {
		return err
	}
	if err := responseError(pdu); err != nil {
		return err
	}
	return nil
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

func (s *Session) expireDeadline(item *deadlineItem) {
	if item == nil {
		return
	}
	err := &TimeoutError{Kind: item.kind, Command: item.command, Sequence: item.sequence, After: item.after}
	request, won := s.pending.completeError(item.sequence, err)
	if won {
		s.metrics.responseTimeouts.Add(1)
		eventKind := EventResponseTimeout
		if item.kind == TimeoutEnquireLink {
			s.metrics.enquireLinkTimeouts.Add(1)
			eventKind = EventEnquireLinkTimeout
		}
		s.emitEvent(Event{Kind: eventKind, Command: item.command, Sequence: item.sequence, Timeout: item.kind, Duration: item.after})
		s.machine.CancelOutbound(request.requestID)
	}
}

func (s *Session) noteActivity() {
	s.noteActivityAt(time.Now())
}

func (s *Session) noteActivityAt(at time.Time) {
	s.lastActivity.Store(at.UnixNano())
}

func (s *Session) lastActivityTime() time.Time {
	return time.Unix(0, s.lastActivity.Load())
}

func isBoundState(state protocol.SessionState) bool {
	return state == protocol.StateBoundTX || state == protocol.StateBoundRX || state == protocol.StateBoundTRX
}

func (s *Session) livenessLoop() {
	defer s.wg.Done()
	interval := s.livenessResolution()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case now := <-ticker.C:
			state := s.State()
			if (state == protocol.StateOpen || state == protocol.StateOutbound) && s.config.SessionInitTimeout > 0 && now.Sub(s.createdAt) >= s.config.SessionInitTimeout {
				s.metrics.sessionInitTimeouts.Add(1)
				s.emitEvent(Event{Kind: EventSessionInitTimeout, At: now, Timeout: TimeoutSessionInit, Duration: s.config.SessionInitTimeout})
				s.terminate(&TimeoutError{Kind: TimeoutSessionInit, After: s.config.SessionInitTimeout})
				return
			}
			if !isBoundState(state) {
				continue
			}
			idle := now.Sub(s.lastActivityTime())
			if s.config.InactivityTimeout > 0 && idle >= s.config.InactivityTimeout {
				s.metrics.inactivityTimeouts.Add(1)
				s.emitEvent(Event{Kind: EventInactivityTimeout, At: now, Timeout: TimeoutInactivity, Duration: s.config.InactivityTimeout})
				s.terminate(&TimeoutError{Kind: TimeoutInactivity, After: s.config.InactivityTimeout})
				return
			}
			if s.config.EnquireLinkInterval > 0 && idle >= s.config.EnquireLinkInterval {
				if err := s.EnquireLink(s.ctx); err != nil {
					select {
					case <-s.done:
						return
					default:
						s.terminate(err)
						return
					}
				}
			}
		case <-s.done:
			return
		}
	}
}

func (s *Session) livenessResolution() time.Duration {
	resolution := 500 * time.Millisecond
	for _, duration := range []time.Duration{s.config.SessionInitTimeout, s.config.EnquireLinkInterval, s.config.InactivityTimeout} {
		if duration > 0 && duration/4 < resolution {
			resolution = duration / 4
		}
	}
	if resolution < 10*time.Millisecond {
		return 10 * time.Millisecond
	}
	return resolution
}

func responseError(pdu codec.DecodedPDU) error {
	if pdu.Header.CommandID == protocol.CommandGenericNACK || !pdu.Header.CommandStatus.OK() {
		return &ResponseError{Command: pdu.Header.CommandID, Status: pdu.Header.CommandStatus, Sequence: pdu.Header.SequenceNumber}
	}
	return nil
}

func (s *Session) rxLoop() {
	defer s.wg.Done()
	buffer := make([]byte, s.config.ReadBufferSize)
	for {
		n, readErr := s.conn.Read(buffer)
		if n > 0 {
			if err := s.framer.Feed(buffer[:n], s.processFrame); err != nil {
				s.terminate(err)
				return
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				if err := s.framer.Finalize(); err != nil {
					s.terminate(err)
				} else {
					s.terminate(io.EOF)
				}
			} else {
				s.terminate(readErr)
			}
			return
		}
		select {
		case <-s.done:
			return
		default:
		}
	}
}

func (s *Session) txLoop() {
	defer s.wg.Done()

	// Opportunistically coalesce only work that is already queued. The TX loop
	// never waits to form a batch, so an isolated PDU keeps the same latency
	// behavior while bursts can amortize channel scheduling and TCP write
	// syscalls. The bounds are per session and configurable.
	batch := make([]txItem, 0, s.config.TXBatchItems)
	var writeBuffer []byte
	var writeBuffers net.Buffers
	var batchTCP *net.TCPConn
	if s.config.TXBatchItems > 1 {
		batchTCP, _ = s.conn.(*net.TCPConn)
		if batchTCP != nil {
			writeBuffers = make(net.Buffers, 0, s.config.TXBatchItems)
		} else {
			writeBuffer = make([]byte, 0, s.config.TXBatchBytes)
		}
	}

	for {
		batch = batch[:0]
		select {
		case item := <-s.tx:
			if s.txItemActive(item) {
				batch = append(batch, item)
			} else {
				s.releaseTXItem(&item)
			}
		case <-s.done:
			return
		}
		if len(batch) == 0 {
			continue
		}

		batchBytes := len(batch[0].frame)
		terminal := txItemTerminatesAfterWrite(batch[0])
		for !terminal && len(batch) < s.config.TXBatchItems && batchBytes < s.config.TXBatchBytes {
			select {
			case item := <-s.tx:
				if !s.txItemActive(item) {
					s.releaseTXItem(&item)
					continue
				}
				batch = append(batch, item)
				batchBytes += len(item.frame)
				terminal = txItemTerminatesAfterWrite(item)
			default:
				terminal = true // stop draining; do not wait for more work
			}
		}

		// Lifecycle state must be committed before a response becomes visible on
		// the wire. This preserves the existing bind-response race guarantee even
		// when several already-queued PDUs share one transport write.
		for i := range batch {
			item := &batch[i]
			if item.kind == txResponse && item.responseTo != 0 {
				s.machine.CompleteInbound(item.responseTo, item.status)
			}
		}

		var err error
		if len(batch) == 1 {
			err = transport.WriteFull(s.conn, batch[0].frame)
		} else if batchTCP != nil {
			writeBuffers = writeBuffers[:0]
			for i := range batch {
				writeBuffers = append(writeBuffers, batch[i].frame)
			}
			err = transport.WriteBuffers(batchTCP, writeBuffers)
		} else {
			writeBuffer = writeBuffer[:0]
			for i := range batch {
				writeBuffer = append(writeBuffer, batch[i].frame...)
			}
			err = transport.WriteFull(s.conn, writeBuffer)
		}
		if err != nil {
			for i := range batch {
				if batch[i].dispatched != nil {
					batch[i].dispatched <- err
				}
				s.releaseTXItem(&batch[i])
			}
			s.terminate(err)
			return
		}

		now := time.Now()
		s.noteActivityAt(now)
		for i := range batch {
			item := &batch[i]
			s.tracePacket(PacketOutbound, item.header, item.frame)
			s.observeOutbound(*item, now)
			if item.kind == txRequest && item.pending != nil {
				if rtt, ok := item.pending.markDispatchAt(now); ok {
					s.observeRTT(item.pending, item.sequence, rtt)
				}
			}
			if item.dispatched != nil {
				item.dispatched <- nil
			}
			if item.kind == txRequest {
				if request, ok := s.pending.markDispatched(item.sequence); ok {
					timeout, kind := s.responseTimeoutFor(item.requestID)
					deadline := s.deadlines.scheduleItemAt(&request.deadlineStorage, now, timeout, kind, item.requestID, item.sequence)
					request.attachDeadline(s.deadlines, deadline)
				}
				continue
			}
			if txItemTerminatesAfterWrite(*item) {
				s.terminate(ErrSessionClosed)
				return
			}
		}
	}
}

func (s *Session) txItemActive(item txItem) bool {
	return item.kind != txRequest || s.pending.exists(item.sequence)
}

func txItemTerminatesAfterWrite(item txItem) bool {
	return item.kind == txResponse && item.responseTo == protocol.CommandUnbind && item.status.OK()
}

func (s *Session) observeOutbound(item txItem, at time.Time) {
	event := Event{At: at, Command: item.header.CommandID, Sequence: item.header.SequenceNumber, Status: item.header.CommandStatus}
	if item.kind == txResponse {
		s.metrics.responsesSent.Add(1)
		event.Kind = EventResponseSent
		s.emitEvent(event)
		return
	}
	s.metrics.requestsSent.Add(1)
	event.Kind = EventRequestSent
	s.emitEvent(event)
	if item.header.CommandID == protocol.CommandEnquireLink {
		s.metrics.enquireLinkSent.Add(1)
		event.Kind = EventEnquireLinkSent
		s.emitEvent(event)
	}
}

func (s *Session) processFrame(frame []byte) error {
	pdu, err := codec.DecodePDU(frame, s.registry)
	if err != nil {
		var fatal *protocol.FatalError
		if errors.As(err, &fatal) {
			return err
		}
		s.metrics.decodeFailures.Add(1)
		s.emitEvent(Event{Kind: EventDecodeFailure, Reason: err.Error()})
		header, headerErr := codec.DecodeHeader(frame)
		if headerErr != nil {
			return headerErr
		}
		// Framing is still trustworthy. Reject the semantic/body error without
		// poisoning or resynchronizing the TCP stream.
		_ = s.queueGenericNACK(header, protocol.StatusInvalidMessageLength)
		return nil
	}
	now := time.Now()
	s.noteActivityAt(now)
	s.tracePacket(PacketInbound, pdu.Header, frame)
	if pdu.Header.CommandID.IsResponse() {
		s.metrics.responsesReceived.Add(1)
		s.emitEvent(Event{Kind: EventResponseReceived, At: now, Command: pdu.Header.CommandID, Sequence: pdu.Header.SequenceNumber, Status: pdu.Header.CommandStatus})
		if pdu.Header.CommandID == protocol.CommandEnquireLinkResp {
			s.metrics.enquireLinkResponses.Add(1)
		}
		s.handleResponse(pdu, now)
		return nil
	}
	s.metrics.requestsReceived.Add(1)
	s.emitEvent(Event{Kind: EventRequestReceived, At: now, Command: pdu.Header.CommandID, Sequence: pdu.Header.SequenceNumber, Status: pdu.Header.CommandStatus})
	if pdu.Header.CommandID == protocol.CommandEnquireLink {
		s.metrics.enquireLinkReceived.Add(1)
		s.emitEvent(Event{Kind: EventEnquireLinkReceived, At: now, Command: pdu.Header.CommandID, Sequence: pdu.Header.SequenceNumber})
	}
	return s.handleRequest(pdu)
}

func (s *Session) handleResponse(pdu codec.DecodedPDU, receivedAt time.Time) {
	request, ok := s.pending.takeResponse(pdu.Header.SequenceNumber, pdu.Header.CommandID)
	if !ok {
		return // late, unmatched, duplicate, or response from another sequence space
	}
	if rtt, ok := request.markResponseAt(receivedAt, pdu.Header.CommandID, pdu.Header.CommandStatus); ok {
		s.observeRTT(request, pdu.Header.SequenceNumber, rtt)
	}
	s.machine.CompleteOutbound(request.requestID, pdu.Header.CommandStatus)
	if pdu.Header.CommandStatus.OK() && isBindRequest(request.requestID) {
		if body, ok := pdu.Body.(protocol.BindResponse); ok {
			peerVersion, advertised := protocol.SCInterfaceVersion(body.Optional)
			localProfile, negotiable := bindNegotiationProfile(s.config.Profile, request.bindVersion)
			if !negotiable {
				localProfile = protocol.Profile{}
			}
			s.peerCaps.Store(protocol.NegotiatePeerCapabilities(localProfile, peerVersion, advertised))
		}
	}
	s.observeCongestion(pdu)
	owned := ownDecodedPDU(pdu)
	request.done <- requestResult{pdu: owned}
	if request.requestID == protocol.CommandUnbind && pdu.Header.CommandStatus.OK() {
		s.terminate(ErrSessionClosed)
	}
}

func (s *Session) observeRTT(request *pendingRequest, sequence protocol.SequenceNumber, rtt time.Duration) {
	s.metrics.noteRTT(rtt)
	command, status := request.responseMetadata()
	s.emitEvent(Event{Kind: EventResponseRTT, Command: command, Sequence: sequence, Status: status, Duration: rtt})
}

func (s *Session) handleRequest(pdu codec.DecodedPDU) error {
	if !pdu.Header.SequenceNumber.ValidInbound() {
		return s.queueGenericNACK(pdu.Header, protocol.StatusInvalidMessageLength)
	}
	_, registered := s.registry.Command(pdu.Header.CommandID)
	if !registered {
		return s.queueGenericNACK(pdu.Header, protocol.StatusInvalidCommandID)
	}
	if requiresSMPP50(pdu.Header.CommandID) && !s.PeerCapabilities().SupportsSMPP50 {
		return s.queueGenericNACK(pdu.Header, protocol.StatusInvalidCommandID)
	}

	if err := s.beginInbound(pdu.Header.CommandID); err != nil {
		if isOneWayCommand(pdu.Header.CommandID) {
			return s.queueGenericNACK(pdu.Header, protocol.StatusInvalidBindState)
		}
		return s.queueResponse(pdu.Header, pdu.Header.CommandID.ResponseID(), protocol.StatusInvalidBindState, protocol.EmptyBody{})
	}
	if isOneWayCommand(pdu.Header.CommandID) {
		if s.config.Handler != nil {
			if _, err := s.config.Handler.Handle(s.ctx, s, pdu); err != nil {
				s.logger.Error("SMPP one-way handler failed",
					"event", "smpp_one_way_handler_error",
					"command_id", fmt.Sprintf("0x%08x", uint32(pdu.Header.CommandID)),
					"sequence_number", uint32(pdu.Header.SequenceNumber),
					"error", err)
			}
		}
		return nil
	}
	lifecycleReserved := isLifecycleRequest(pdu.Header.CommandID)

	var response Response
	var handlerErr error
	switch pdu.Header.CommandID {
	case protocol.CommandEnquireLink, protocol.CommandUnbind:
		response = Response{Status: protocol.StatusOK, Body: protocol.EmptyBody{}}
	default:
		if s.config.Handler == nil {
			if isBindRequest(pdu.Header.CommandID) {
				response = Response{Status: protocol.StatusBindFailed, Body: protocol.EmptyBody{}}
			} else {
				response = Response{Status: protocol.StatusSystemError, Body: protocol.EmptyBody{}}
			}
		} else {
			response, handlerErr = s.config.Handler.Handle(s.ctx, s, pdu)
			if handlerErr != nil {
				response = Response{Status: protocol.StatusSystemError, Body: protocol.EmptyBody{}}
			}
		}
	}
	if response.Status.OK() && isBindRequest(pdu.Header.CommandID) {
		if bind, ok := pdu.Body.(protocol.BindRequest); ok {
			s.peerCaps.Store(protocol.NegotiatePeerCapabilities(s.config.Profile, bind.InterfaceVersion, true))
			if bind.InterfaceVersion == protocol.InterfaceVersion34 || bind.InterfaceVersion == protocol.InterfaceVersion50 {
				response.Body = ensureSCInterfaceVersion(response.Body, s.config.Profile.InterfaceVersion)
			}
		}
	}
	if err := s.queueResponse(pdu.Header, pdu.Header.CommandID.ResponseID(), response.Status, response.Body); err != nil {
		if lifecycleReserved {
			s.machine.CancelInbound(pdu.Header.CommandID)
		}
		return err
	}
	return nil
}

func (s *Session) beginInbound(command protocol.CommandID) error {
	peerRole := s.Role().peer()
	state := s.State()
	if isStandardSessionCommand(command) {
		return s.machine.BeginInbound(command)
	}
	// Vendor-specific request commands do not have a standard operation-matrix
	// entry. Keep their default policy conservative: they may run only after a
	// session is bound, with application semantics delegated to Handler.
	if !command.IsResponse() && (state == protocol.StateBoundTX || state == protocol.StateBoundRX || state == protocol.StateBoundTRX) {
		return nil
	}
	return &StateError{State: state, Command: command, Role: peerRole}
}

func isStandardSessionCommand(command protocol.CommandID) bool {
	switch command {
	case protocol.CommandBindReceiver,
		protocol.CommandBindTransmitter,
		protocol.CommandQuerySM,
		protocol.CommandSubmitSM,
		protocol.CommandDeliverSM,
		protocol.CommandUnbind,
		protocol.CommandReplaceSM,
		protocol.CommandCancelSM,
		protocol.CommandBindTransceiver,
		protocol.CommandOutbind,
		protocol.CommandEnquireLink,
		protocol.CommandSubmitMulti,
		protocol.CommandAlertNotification,
		protocol.CommandDataSM,
		protocol.CommandBroadcastSM,
		protocol.CommandQueryBroadcastSM,
		protocol.CommandCancelBroadcastSM:
		return true
	default:
		return false
	}
}

func isOneWayCommand(command protocol.CommandID) bool {
	return command == protocol.CommandOutbind || command == protocol.CommandAlertNotification
}

func isLifecycleRequest(command protocol.CommandID) bool {
	return isBindRequest(command) || command == protocol.CommandUnbind
}

func (s *Session) queueGenericNACK(request codec.Header, status protocol.CommandStatus) error {
	return s.queueResponse(request, protocol.CommandGenericNACK, status, protocol.EmptyBody{})
}

func (s *Session) queueResponse(request codec.Header, responseID protocol.CommandID, status protocol.CommandStatus, body any) error {
	header := codec.ResponseHeader(request, responseID, status)
	frame, frameBuffer, err := s.encodeTXPDU(header, body)
	if err != nil {
		return err
	}
	item := txItem{kind: txResponse, header: header, frame: frame, sequence: request.SequenceNumber, responseTo: request.CommandID, status: status, buffer: frameBuffer}
	if responseID == protocol.CommandGenericNACK {
		item.responseTo = 0
	}
	select {
	case s.tx <- item:
		return nil
	case <-s.done:
		s.releaseTXBuffer(frameBuffer)
		return s.requestCloseError()
	}
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

func ownDecodedPDU(pdu codec.DecodedPDU) codec.DecodedPDU {
	switch body := pdu.Body.(type) {
	case protocol.BindResponse:
		body.SystemID = cloneBytes(body.SystemID)
		body.Optional = cloneOptional(body.Optional)
		pdu.Body = body
	case protocol.SubmitSMResp:
		body.MessageID = cloneBytes(body.MessageID)
		body.Optional = cloneOptional(body.Optional)
		pdu.Body = body
	case protocol.DeliverSMResp:
		body.MessageID = cloneBytes(body.MessageID)
		body.Optional = cloneOptional(body.Optional)
		pdu.Body = body
	case protocol.DataSMResp:
		body.MessageID = cloneBytes(body.MessageID)
		body.Optional = cloneOptional(body.Optional)
		pdu.Body = body
	case protocol.SubmitMultiResp:
		body.MessageID = cloneBytes(body.MessageID)
		body.Optional = cloneOptional(body.Optional)
		if len(body.Unsuccessful) > 0 {
			entries := make([]protocol.UnsuccessfulSME, len(body.Unsuccessful))
			copy(entries, body.Unsuccessful)
			for i := range entries {
				entries[i].DestinationAddr = cloneBytes(entries[i].DestinationAddr)
			}
			body.Unsuccessful = entries
		}
		pdu.Body = body
	case protocol.QuerySMResp:
		body.MessageID = cloneBytes(body.MessageID)
		body.FinalDate = cloneBytes(body.FinalDate)
		body.Optional = cloneOptional(body.Optional)
		pdu.Body = body
	case protocol.BroadcastSMResp:
		body.MessageID = cloneBytes(body.MessageID)
		body.Optional = cloneOptional(body.Optional)
		pdu.Body = body
	case protocol.QueryBroadcastSMResp:
		body.MessageID = cloneBytes(body.MessageID)
		body.Optional = cloneOptional(body.Optional)
		pdu.Body = body
	case protocol.OptionalResponse:
		body.Optional = cloneOptional(body.Optional)
		pdu.Body = body
	case codec.RawBody:
		pdu.Body = codec.RawBody(cloneBytes(body))
	}
	return pdu
}

func bindRequestVersion(body any) (protocol.InterfaceVersion, bool) {
	switch typed := body.(type) {
	case protocol.BindRequest:
		return typed.InterfaceVersion, true
	case *protocol.BindRequest:
		if typed != nil {
			return typed.InterfaceVersion, true
		}
	}
	return 0, false
}

// bindNegotiationProfile limits negotiated capabilities to what this local
// endpoint actually advertised in its bind request. Reserved and pre-3.4 bind
// versions deliberately produce no modern TLV/SMPP 5.0 capability assumption.
func bindNegotiationProfile(local protocol.Profile, advertised protocol.InterfaceVersion) (protocol.Profile, bool) {
	switch advertised {
	case protocol.InterfaceVersion34:
		return protocol.SMPP34Profile(), true
	case protocol.InterfaceVersion50:
		if local.IsSMPP50() {
			return protocol.SMPP50Profile(), true
		}
	}
	return protocol.Profile{}, false
}

// ensureSCInterfaceVersion follows the SMPP compatibility guidance that an MC
// supporting SMPP 3.4 or later should advertise its interface version in a
// successful bind response. An explicitly supplied tag is preserved so an
// application can intentionally advertise a narrower capability set.
func ensureSCInterfaceVersion(body any, version protocol.InterfaceVersion) any {
	appendIfMissing := func(response protocol.BindResponse) protocol.BindResponse {
		for _, parameter := range response.Optional {
			if parameter.Tag == protocol.TLVTagSCInterfaceVersion {
				return response
			}
		}
		response.Optional = append(response.Optional, protocol.OptionalParameter{
			Tag: protocol.TLVTagSCInterfaceVersion, Value: []byte{byte(version)},
		})
		return response
	}

	switch typed := body.(type) {
	case protocol.BindResponse:
		return appendIfMissing(typed)
	case *protocol.BindResponse:
		if typed == nil {
			return body
		}
		response := appendIfMissing(*typed)
		return response
	default:
		return body
	}
}

func requiresSMPP50(command protocol.CommandID) bool {
	switch command {
	case protocol.CommandBroadcastSM, protocol.CommandBroadcastSMResp,
		protocol.CommandQueryBroadcastSM, protocol.CommandQueryBroadcastSMResp,
		protocol.CommandCancelBroadcastSM, protocol.CommandCancelBroadcastSMResp:
		return true
	default:
		return false
	}
}

func (s *Session) observeCongestion(pdu codec.DecodedPDU) {
	if !s.PeerCapabilities().SupportsSMPP50 {
		return
	}
	optional := responseOptionalParameters(pdu.Body)
	state, present, err := protocol.CongestionStateFromOptional(optional)
	if err != nil || !present {
		return
	}
	now := time.Now()
	s.metrics.noteCongestion(state)
	s.emitEvent(Event{Kind: EventCongestionState, At: now, Command: pdu.Header.CommandID, Sequence: pdu.Header.SequenceNumber, Congestion: state})
	if s.config.FlowController != nil {
		s.config.FlowController.OnCongestion(CongestionEvent{
			State: state, Command: pdu.Header.CommandID, Sequence: pdu.Header.SequenceNumber, At: now,
		})
	}
}

func responseOptionalParameters(body any) []protocol.OptionalParameter {
	switch v := body.(type) {
	case protocol.BindResponse:
		return v.Optional
	case protocol.SubmitSMResp:
		return v.Optional
	case protocol.DeliverSMResp:
		return v.Optional
	case protocol.DataSMResp:
		return v.Optional
	case protocol.SubmitMultiResp:
		return v.Optional
	case protocol.QuerySMResp:
		return v.Optional
	case protocol.BroadcastSMResp:
		return v.Optional
	case protocol.QueryBroadcastSMResp:
		return v.Optional
	case protocol.OptionalResponse:
		return v.Optional
	default:
		return nil
	}
}

func cloneBytes(src []byte) []byte {
	if len(src) == 0 {
		return nil
	}
	dst := make([]byte, len(src))
	copy(dst, src)
	return dst
}

func cloneOptional(src []protocol.OptionalParameter) []protocol.OptionalParameter {
	if len(src) == 0 {
		return nil
	}
	dst := make([]protocol.OptionalParameter, len(src))
	for i := range src {
		dst[i] = protocol.OptionalParameter{Tag: src[i].Tag, Value: cloneBytes(src[i].Value)}
	}
	return dst
}
