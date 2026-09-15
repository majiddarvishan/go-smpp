package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"

	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/transport"
)

const (
	DefaultMaxPending     = 1024
	DefaultTXQueueSize    = 1024
	DefaultReadBufferSize = 64 << 10
)

// Response is returned by an inbound request Handler. The response command ID
// and sequence number are derived from the request so handlers cannot
// accidentally break correlation.
type Response struct {
	Status protocol.CommandStatus
	Body   any
}

// Handler processes an inbound request PDU. The DecodedPDU may contain borrowed
// views into the receive frame and must not be retained after Handle returns
// unless the application copies the data it needs.
type Handler interface {
	Handle(context.Context, *Session, codec.DecodedPDU) (Response, error)
}

type HandlerFunc func(context.Context, *Session, codec.DecodedPDU) (Response, error)

func (f HandlerFunc) Handle(ctx context.Context, s *Session, pdu codec.DecodedPDU) (Response, error) {
	return f(ctx, s, pdu)
}

// Config configures one active SMPP session. The registry is immutable while a
// session is active. MaxPending and TXQueueSize are defensive bounds, not the
// Phase-8 outstanding-window policy.
type Config struct {
	Role           Role
	Registry       *codec.Registry
	MaxPDUSize     uint32
	MaxPending     int
	TXQueueSize    int
	ReadBufferSize int
	Handler        Handler
	Logger         *slog.Logger
}

type txKind uint8

const (
	txRequest txKind = iota + 1
	txResponse
)

type txItem struct {
	kind       txKind
	frame      []byte
	sequence   protocol.SequenceNumber
	requestID  protocol.CommandID
	responseTo protocol.CommandID
	status     protocol.CommandStatus
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

	pending *pendingTable
	seq     sequenceGenerator

	closeOnce sync.Once
	wg        sync.WaitGroup
	errMu     sync.RWMutex
	closeErr  error
}

func New(conn net.Conn, config Config) (*Session, error) {
	if conn == nil || !config.Role.valid() {
		return nil, ErrInvalidConfig
	}
	if config.Registry == nil {
		registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
		if err != nil {
			return nil, err
		}
		config.Registry = registry
	}
	if config.MaxPending <= 0 {
		config.MaxPending = DefaultMaxPending
	}
	if config.TXQueueSize <= 0 {
		config.TXQueueSize = DefaultTXQueueSize
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
	s := &Session{
		conn: conn, registry: config.Registry, framer: framer, machine: machine,
		config: config, logger: logger, ctx: ctx, cancel: cancel,
		done: make(chan struct{}), tx: make(chan txItem, config.TXQueueSize),
		pending: newPendingTable(config.MaxPending),
	}
	s.wg.Add(2)
	go s.rxLoop()
	go s.txLoop()
	return s, nil
}

func (s *Session) Role() Role                   { return s.machine.Role() }
func (s *Session) State() protocol.SessionState { return s.machine.State() }
func (s *Session) BindMode() BindMode           { return s.machine.BindMode() }
func (s *Session) Done() <-chan struct{}         { return s.done }
func (s *Session) Pending() int                  { return s.pending.len() }
func (s *Session) LocalAddr() net.Addr           { return s.conn.LocalAddr() }
func (s *Session) RemoteAddr() net.Addr          { return s.conn.RemoteAddr() }

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
// remains asynchronous and may carry many other requests concurrently.
func (s *Session) Request(ctx context.Context, command protocol.CommandID, body any) (codec.DecodedPDU, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if command.IsResponse() || command == protocol.CommandOutbind {
		return codec.DecodedPDU{}, fmt.Errorf("%w: command 0x%08x does not have synchronous request semantics", ErrUnexpectedPDU, uint32(command))
	}
	select {
	case <-s.done:
		return codec.DecodedPDU{}, s.requestCloseError()
	default:
	}

	if err := s.machine.BeginOutbound(command); err != nil {
		return codec.DecodedPDU{}, err
	}
	reserved := true
	defer func() {
		if reserved {
			s.machine.CancelOutbound(command)
		}
	}()

	request := &pendingRequest{requestID: command, expectedID: command.ResponseID(), done: make(chan requestResult, 1)}
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

	header := codec.Header{CommandID: command, CommandStatus: protocol.StatusOK, SequenceNumber: sequence}
	frame, err := codec.EncodePDU(nil, header, body, s.registry)
	if err != nil {
		_, _ = s.pending.completeError(sequence, err)
		return codec.DecodedPDU{}, err
	}

	item := txItem{kind: txRequest, frame: frame, sequence: sequence, requestID: command}
	select {
	case s.tx <- item:
		reserved = false
	case <-ctx.Done():
		if _, won := s.pending.completeError(sequence, ctx.Err()); won {
			return codec.DecodedPDU{}, ctx.Err()
		}
		reserved = false
		return s.waitCompleted(request)
	case <-s.done:
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
	for {
		select {
		case item := <-s.tx:
			if item.kind == txRequest && !s.pending.exists(item.sequence) {
				continue
			}
			if err := transport.WriteFull(s.conn, item.frame); err != nil {
				s.terminate(err)
				return
			}
			if item.kind == txRequest {
				s.pending.markDispatched(item.sequence)
				continue
			}
			if item.responseTo != 0 {
				s.machine.CompleteInbound(item.responseTo, item.status)
				if item.responseTo == protocol.CommandUnbind && item.status.OK() {
					s.terminate(ErrSessionClosed)
					return
				}
			}
		case <-s.done:
			return
		}
	}
}

func (s *Session) processFrame(frame []byte) error {
	pdu, err := codec.DecodePDU(frame, s.registry)
	if err != nil {
		var fatal *protocol.FatalError
		if errors.As(err, &fatal) {
			return err
		}
		header, headerErr := codec.DecodeHeader(frame)
		if headerErr != nil {
			return headerErr
		}
		// Framing is still trustworthy. Reject the semantic/body error without
		// poisoning or resynchronizing the TCP stream.
		_ = s.queueGenericNACK(header, protocol.StatusInvalidMessageLength)
		return nil
	}
	if pdu.Header.CommandID.IsResponse() {
		s.handleResponse(pdu)
		return nil
	}
	return s.handleRequest(pdu)
}

func (s *Session) handleResponse(pdu codec.DecodedPDU) {
	request, ok := s.pending.takeResponse(pdu.Header.SequenceNumber, pdu.Header.CommandID)
	if !ok {
		return // late, unmatched, duplicate, or response from another sequence space
	}
	s.machine.CompleteOutbound(request.requestID, pdu.Header.CommandStatus)
	owned := ownDecodedPDU(pdu)
	request.done <- requestResult{pdu: owned}
	if request.requestID == protocol.CommandUnbind && pdu.Header.CommandStatus.OK() {
		s.terminate(ErrSessionClosed)
	}
}

func (s *Session) handleRequest(pdu codec.DecodedPDU) error {
	if !pdu.Header.SequenceNumber.ValidInbound() {
		return s.queueGenericNACK(pdu.Header, protocol.StatusInvalidMessageLength)
	}
	_, registered := s.registry.Command(pdu.Header.CommandID)
	if !registered {
		return s.queueGenericNACK(pdu.Header, protocol.StatusInvalidCommandID)
	}

	if err := s.beginInbound(pdu.Header.CommandID); err != nil {
		return s.queueResponse(pdu.Header, pdu.Header.CommandID.ResponseID(), protocol.StatusInvalidBindState, protocol.EmptyBody{})
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
		protocol.CommandDataSM:
		return true
	default:
		return false
	}
}

func isLifecycleRequest(command protocol.CommandID) bool {
	return isBindRequest(command) || command == protocol.CommandUnbind
}

func (s *Session) queueGenericNACK(request codec.Header, status protocol.CommandStatus) error {
	return s.queueResponse(request, protocol.CommandGenericNACK, status, protocol.EmptyBody{})
}

func (s *Session) queueResponse(request codec.Header, responseID protocol.CommandID, status protocol.CommandStatus, body any) error {
	header := codec.ResponseHeader(request, responseID, status)
	frame, err := codec.EncodePDU(nil, header, body, s.registry)
	if err != nil {
		return err
	}
	item := txItem{kind: txResponse, frame: frame, sequence: request.SequenceNumber, responseTo: request.CommandID, status: status}
	if responseID == protocol.CommandGenericNACK {
		item.responseTo = 0
	}
	select {
	case s.tx <- item:
		return nil
	case <-s.done:
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
		pdu.Body = body
	case protocol.DeliverSMResp:
		body.MessageID = cloneBytes(body.MessageID)
		pdu.Body = body
	case codec.RawBody:
		pdu.Body = codec.RawBody(cloneBytes(body))
	}
	return pdu
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
