package server

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"sync"

	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/session"
	"github.com/majiddarvishan/go-smpp/transport"
)

var (
	ErrAlreadyServing = errors.New("smpp server: Serve already active")
	ErrUnknownSession = errors.New("smpp server: session is not owned by this server")
)

// BindResult controls the SMPP response returned after an authentication hook.
// StatusOK accepts the bind. For an unsuccessful status, SystemID/Optional are
// ignored because unsuccessful bind responses have no body in SMPP 3.4.
type BindResult struct {
	Status   protocol.CommandStatus
	SystemID []byte
	Optional []protocol.OptionalParameter
}

// Authenticator validates an inbound bind request. Implementations may inspect
// the requested mode and the remote session address. Password bytes are handed
// only to this hook and are never logged by the server/session default logger.
type Authenticator interface {
	Authenticate(context.Context, *session.Session, session.BindMode, protocol.BindRequest) (BindResult, error)
}

type AuthenticatorFunc func(context.Context, *session.Session, session.BindMode, protocol.BindRequest) (BindResult, error)

func (f AuthenticatorFunc) Authenticate(ctx context.Context, s *session.Session, mode session.BindMode, request protocol.BindRequest) (BindResult, error) {
	return f(ctx, s, mode, request)
}

// SubmitResult controls submit_sm_resp for an accepted inbound submit_sm.
type SubmitResult struct {
	Status    protocol.CommandStatus
	MessageID []byte
}

// SubmitHandler processes inbound submit_sm after the SMPP state machine has
// already verified that the peer is allowed to issue it.
type SubmitHandler interface {
	SubmitSM(context.Context, *session.Session, protocol.SubmitSM) (SubmitResult, error)
}

type SubmitHandlerFunc func(context.Context, *session.Session, protocol.SubmitSM) (SubmitResult, error)

func (f SubmitHandlerFunc) SubmitSM(ctx context.Context, s *session.Session, request protocol.SubmitSM) (SubmitResult, error) {
	return f(ctx, s, request)
}

// Config configures SMSC/server listener and per-session behavior.
//
// A caller may either create a listener itself and pass this Config to
// NewWithConfig, or use Listen/ListenAndServe to create plain TCP or TLS-over-TCP
// listeners through the transport package.
type Config struct {
	Network      string
	Address      string
	ListenConfig *net.ListenConfig
	TLSConfig    *tls.Config

	SessionConfig session.Config
	Authenticator Authenticator
	SubmitHandler SubmitHandler
	Fallback      session.Handler

	// MaxSessions limits simultaneously active accepted sessions. A value <= 0
	// means unlimited. Connections accepted while the limit is full are closed
	// immediately without affecting the listener or existing sessions.
	MaxSessions int
}

// Server accepts TCP/TLS connections and attaches the shared SMSC session core
// to each connection. All exported methods are safe for concurrent use.
type Server struct {
	listener net.Listener
	config   Config

	mu        sync.Mutex
	sessions  map[*session.Session]struct{}
	serving   bool
	closed    bool
	closeOnce sync.Once
}

// New preserves the original low-level API for callers that already own a
// listener and want to provide a session.Handler directly.
func New(listener net.Listener, config session.Config) *Server {
	return NewWithConfig(listener, Config{SessionConfig: config, Fallback: config.Handler})
}

// NewWithConfig attaches SMSC behavior to an already-created TCP-compatible
// listener. TLS configuration belongs to listener creation and is not applied a
// second time here.
func NewWithConfig(listener net.Listener, config Config) *Server {
	config.SessionConfig.Role = session.RoleSMSC
	fallback := config.Fallback
	if fallback == nil {
		fallback = config.SessionConfig.Handler
	}
	if config.Authenticator != nil || config.SubmitHandler != nil || fallback != nil {
		config.SessionConfig.Handler = &dispatchHandler{
			authenticator: config.Authenticator,
			submit:        config.SubmitHandler,
			fallback:      fallback,
		}
	}
	return &Server{listener: listener, config: config, sessions: make(map[*session.Session]struct{})}
}

// Listen creates a plain TCP or TLS-over-TCP listener from Config.
func Listen(ctx context.Context, config Config) (*Server, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var (
		listener net.Listener
		err      error
	)
	if config.TLSConfig != nil {
		listener, err = transport.ListenTLS(ctx, config.Network, config.Address, config.ListenConfig, config.TLSConfig)
	} else {
		listener, err = transport.ListenTCP(ctx, config.Network, config.Address, config.ListenConfig)
	}
	if err != nil {
		return nil, err
	}
	return NewWithConfig(listener, config), nil
}

// ListenAndServe is a convenience wrapper around Listen followed by Serve.
func ListenAndServe(ctx context.Context, config Config) error {
	server, err := Listen(ctx, config)
	if err != nil {
		return err
	}
	defer server.Close()
	return server.Serve(ctx)
}

func (s *Server) Addr() net.Addr {
	if s == nil || s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

// Serve runs the accept loop. It is safe for Close and Sessions to run
// concurrently; only one Serve call may be active.
func (s *Server) Serve(ctx context.Context) error {
	if s == nil || s.listener == nil {
		return net.ErrClosed
	}
	s.mu.Lock()
	if s.serving {
		s.mu.Unlock()
		return ErrAlreadyServing
	}
	if s.closed {
		s.mu.Unlock()
		return net.ErrClosed
	}
	s.serving = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.serving = false
		s.mu.Unlock()
	}()

	if ctx == nil {
		ctx = context.Background()
	}
	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = s.Close()
		case <-stop:
		}
	}()
	defer close(stop)

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed || ctx.Err() != nil {
				return nil
			}
			return err
		}

		s.mu.Lock()
		atLimit := s.config.MaxSessions > 0 && len(s.sessions) >= s.config.MaxSessions
		closed := s.closed
		s.mu.Unlock()
		if closed || atLimit {
			_ = conn.Close()
			if closed {
				return nil
			}
			continue
		}

		sess, err := session.New(conn, s.config.SessionConfig)
		if err != nil {
			_ = conn.Close()
			continue
		}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			_ = sess.Close()
			return nil
		}
		// Serve has one accept loop, but preserve the limit check under the same
		// lock used for insertion so future accept parallelism cannot oversubscribe.
		if s.config.MaxSessions > 0 && len(s.sessions) >= s.config.MaxSessions {
			s.mu.Unlock()
			_ = sess.Close()
			continue
		}
		s.sessions[sess] = struct{}{}
		s.mu.Unlock()
		go s.watch(sess)
	}
}

func (s *Server) watch(sess *session.Session) {
	<-sess.Done()
	sess.Wait()
	s.mu.Lock()
	delete(s.sessions, sess)
	s.mu.Unlock()
}

// Sessions returns a snapshot of currently active sessions.
func (s *Server) Sessions() []*session.Session {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	result := make([]*session.Session, 0, len(s.sessions))
	for sess := range s.sessions {
		result = append(result, sess)
	}
	s.mu.Unlock()
	return result
}

// DeliverSM sends a server-originated deliver_sm over a session currently owned
// by this server. Session-level windowing, response timeout, correlation and
// sequence allocation apply exactly as they do for client-originated requests.
func (s *Server) DeliverSM(ctx context.Context, sess *session.Session, request protocol.DeliverSM) (protocol.DeliverSMResp, error) {
	if sess == nil || !s.owns(sess) {
		return protocol.DeliverSMResp{}, ErrUnknownSession
	}
	return sess.DeliverSM(ctx, request)
}

// DataSM sends a server-originated data_sm over a bound session.
func (s *Server) DataSM(ctx context.Context, sess *session.Session, request protocol.DataSM) (protocol.DataSMResp, error) {
	if sess == nil || !s.owns(sess) {
		return protocol.DataSMResp{}, ErrUnknownSession
	}
	return sess.DataSM(ctx, request)
}

// AlertNotification sends the one-way SMSC alert_notification primitive.
func (s *Server) AlertNotification(ctx context.Context, sess *session.Session, notification protocol.AlertNotification) error {
	if sess == nil || !s.owns(sess) {
		return ErrUnknownSession
	}
	return sess.AlertNotification(ctx, notification)
}

// Outbind asks an OPEN ESME connection to originate bind_receiver. The shared
// session state machine moves both peers through the Outbound state.
func (s *Server) Outbind(ctx context.Context, sess *session.Session, request protocol.Outbind) error {
	if sess == nil || !s.owns(sess) {
		return ErrUnknownSession
	}
	return sess.Outbind(ctx, request)
}

func (s *Server) owns(sess *session.Session) bool {
	s.mu.Lock()
	_, ok := s.sessions[sess]
	s.mu.Unlock()
	return ok
}

// Close is idempotent and safe for concurrent use. It closes the listener and
// all currently active sessions but does not wait for their goroutines; callers
// may use the individual Session.Wait method when required.
func (s *Server) Close() error {
	if s == nil {
		return nil
	}
	var closeErr error
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		listener := s.listener
		sessions := make([]*session.Session, 0, len(s.sessions))
		for sess := range s.sessions {
			sessions = append(sessions, sess)
		}
		s.mu.Unlock()
		if listener != nil {
			closeErr = listener.Close()
		}
		for _, sess := range sessions {
			_ = sess.Close()
		}
	})
	return closeErr
}

type dispatchHandler struct {
	authenticator Authenticator
	submit        SubmitHandler
	fallback      session.Handler
}

func (h *dispatchHandler) Handle(ctx context.Context, sess *session.Session, pdu session.InboundPDU) (session.Response, error) {
	switch pdu.Header.CommandID {
	case protocol.CommandBindTransmitter, protocol.CommandBindReceiver, protocol.CommandBindTransceiver:
		request, ok := pdu.Body.(protocol.BindRequest)
		if !ok {
			return session.Response{Status: protocol.StatusSystemError, Body: protocol.EmptyBody{}}, nil
		}
		if h.authenticator == nil {
			if h.fallback != nil {
				return h.fallback.Handle(ctx, sess, pdu)
			}
			return session.Response{Status: protocol.StatusBindFailed, Body: protocol.EmptyBody{}}, nil
		}
		result, err := h.authenticator.Authenticate(ctx, sess, bindMode(pdu.Header.CommandID), request)
		if err != nil {
			if result.Status.OK() {
				result.Status = protocol.StatusSystemError
			}
		}
		if !result.Status.OK() {
			return session.Response{Status: result.Status, Body: protocol.EmptyBody{}}, nil
		}
		return session.Response{Status: protocol.StatusOK, Body: protocol.BindResponse{SystemID: result.SystemID, Optional: result.Optional}}, nil

	case protocol.CommandSubmitSM:
		request, ok := pdu.Body.(protocol.SubmitSM)
		if !ok {
			return session.Response{Status: protocol.StatusSystemError, Body: protocol.EmptyBody{}}, nil
		}
		if h.submit == nil {
			if h.fallback != nil {
				return h.fallback.Handle(ctx, sess, pdu)
			}
			return session.Response{Status: protocol.StatusSystemError, Body: protocol.EmptyBody{}}, nil
		}
		result, err := h.submit.SubmitSM(ctx, sess, request)
		if err != nil && result.Status.OK() {
			result.Status = protocol.StatusSystemError
		}
		if !result.Status.OK() {
			return session.Response{Status: result.Status, Body: protocol.EmptyBody{}}, nil
		}
		return session.Response{Status: protocol.StatusOK, Body: protocol.SubmitSMResp{MessageID: result.MessageID}}, nil
	default:
		if h.fallback != nil {
			return h.fallback.Handle(ctx, sess, pdu)
		}
		return session.Response{Status: protocol.StatusSystemError, Body: protocol.EmptyBody{}}, nil
	}
}

func bindMode(command protocol.CommandID) session.BindMode {
	switch command {
	case protocol.CommandBindTransmitter:
		return session.BindTX
	case protocol.CommandBindReceiver:
		return session.BindRX
	case protocol.CommandBindTransceiver:
		return session.BindTRX
	default:
		return session.BindNone
	}
}
