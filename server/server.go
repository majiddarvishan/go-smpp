package server

import (
	"context"
	"errors"
	"net"
	"sync"

	"github.com/majiddarvishan/go-smpp/session"
)

var ErrAlreadyServing = errors.New("smpp server: Serve already active")

// Server accepts already-configured TCP/TLS listener connections and attaches
// the shared SMSC session engine to each one. Authentication/session policy is
// supplied through SessionConfig.Handler; richer server policy remains in the
// dedicated SMSC/server phase.
type Server struct {
	listener net.Listener
	config   session.Config

	mu       sync.Mutex
	sessions map[*session.Session]struct{}
	serving  bool
	closed   bool
	closeOnce sync.Once
}

func New(listener net.Listener, config session.Config) *Server {
	config.Role = session.RoleSMSC
	return &Server{listener: listener, config: config, sessions: make(map[*session.Session]struct{})}
}

// Serve runs the accept loop. It is safe for Close and Sessions to run
// concurrently; only one Serve call may be active.
func (s *Server) Serve(ctx context.Context) error {
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
		sess, err := session.New(conn, s.config)
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
	s.mu.Lock()
	result := make([]*session.Session, 0, len(s.sessions))
	for sess := range s.sessions {
		result = append(result, sess)
	}
	s.mu.Unlock()
	return result
}

// Close is idempotent and safe for concurrent use. It closes the listener and
// all currently active sessions but does not wait for their goroutines; callers
// may use the individual Session.Wait method when required.
func (s *Server) Close() error {
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
