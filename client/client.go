package client

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/session"
	"github.com/majiddarvishan/go-smpp/transport"
)

var ErrUnavailable = errors.New("smpp client: no active session")

// ReconnectPolicy controls transport reconnect/rebind after session loss.
// Reconnect never replays an application request from the lost session.
type ReconnectPolicy struct {
	Enabled        bool
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	Multiplier     float64
	BindTimeout    time.Duration
}

// Config configures an ESME client.
type Config struct {
	Address       string
	Network       string
	Dialer        *net.Dialer
	TLSConfig     *tls.Config
	SessionConfig session.Config
	Reconnect     ReconnectPolicy
}

// UnavailableError reports that a new request was attempted while no current
// SMPP session was available. Cause preserves the most recent session/dial/bind
// failure when one exists.
type UnavailableError struct {
	Cause        error
	Reconnecting bool
}

func (e *UnavailableError) Error() string {
	if e == nil || e.Cause == nil {
		return ErrUnavailable.Error()
	}
	return ErrUnavailable.Error() + ": " + e.Cause.Error()
}

func (e *UnavailableError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *UnavailableError) Is(target error) bool { return target == ErrUnavailable }

type bindProfile struct {
	mode    session.BindMode
	request protocol.BindRequest
}

// Client is a concurrent-safe ESME facade over replaceable Session instances.
// A request always binds to exactly one session snapshot and is never silently
// retried on a replacement connection.
type Client struct {
	mu       sync.RWMutex
	config   Config
	current  *session.Session
	last     *session.Session
	profile  *bindProfile
	lastErr  error
	lastLoss error
	dialable bool
	closed   bool
	notify   chan struct{}

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	reconnects atomic.Uint64
}

func Dial(ctx context.Context, config Config) (*Client, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	conn, err := dialTransport(ctx, config)
	if err != nil {
		return nil, err
	}
	client, err := newClient(conn, config, true)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return client, nil
}

// New attaches the ESME client API to a caller-supplied TCP-compatible stream.
// A caller-supplied stream cannot be redialed automatically.
func New(conn net.Conn, config session.Config) (*Client, error) {
	return newClient(conn, Config{SessionConfig: config}, false)
}

func newClient(conn net.Conn, config Config, dialable bool) (*Client, error) {
	config.SessionConfig.Role = session.RoleESME
	sess, err := session.New(conn, config.SessionConfig)
	if err != nil {
		return nil, err
	}
	config.Reconnect = normalizeReconnect(config.Reconnect)
	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{
		config: config, current: sess, last: sess, dialable: dialable,
		notify: make(chan struct{}), ctx: ctx, cancel: cancel,
	}
	c.signalLocked()
	c.wg.Add(1)
	go c.lifecycle(sess)
	return c, nil
}

func normalizeReconnect(policy ReconnectPolicy) ReconnectPolicy {
	if policy.InitialBackoff <= 0 {
		policy.InitialBackoff = 100 * time.Millisecond
	}
	if policy.MaxBackoff <= 0 {
		policy.MaxBackoff = 5 * time.Second
	}
	if policy.MaxBackoff < policy.InitialBackoff {
		policy.MaxBackoff = policy.InitialBackoff
	}
	if policy.Multiplier < 1 {
		policy.Multiplier = 2
	}
	if policy.BindTimeout <= 0 {
		policy.BindTimeout = session.DefaultSessionInitTimeout
	}
	return policy
}

func dialTransport(ctx context.Context, config Config) (net.Conn, error) {
	if config.TLSConfig != nil {
		return transport.DialTLS(ctx, config.Network, config.Address, config.Dialer, config.TLSConfig)
	}
	return transport.DialTCP(ctx, config.Network, config.Address, config.Dialer)
}

// Session returns the current session snapshot. If the client is temporarily
// disconnected or closed, it returns the most recently owned session so callers
// can inspect its terminal error/state. Do not cache this value across reconnects.
func (c *Client) Session() *session.Session {
	c.mu.RLock()
	sess := c.current
	if sess == nil {
		sess = c.last
	}
	c.mu.RUnlock()
	return sess
}

func (c *Client) ReconnectCount() uint64 { return c.reconnects.Load() }

// LastLoss returns the most recent terminal error from a replaced/lost SMPP
// session. It remains available after a successful reconnect so applications
// can distinguish transport loss from fatal protocol corruption.
func (c *Client) LastLoss() error {
	c.mu.RLock()
	err := c.lastLoss
	c.mu.RUnlock()
	return err
}

// WaitConnected waits until a current session exists or ctx/client closure wins.
func (c *Client) WaitConnected(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		c.mu.RLock()
		if c.current != nil && !c.closed {
			c.mu.RUnlock()
			return nil
		}
		if c.closed {
			c.mu.RUnlock()
			return ErrUnavailable
		}
		notify := c.notify
		c.mu.RUnlock()
		select {
		case <-notify:
		case <-ctx.Done():
			return ctx.Err()
		case <-c.ctx.Done():
			return ErrUnavailable
		}
	}
}

func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.cancel()
	sess := c.current
	c.signalLocked()
	c.mu.Unlock()
	if sess != nil {
		return sess.Close()
	}
	return nil
}

func (c *Client) Wait() { c.wg.Wait() }

func (c *Client) BindTransmitter(ctx context.Context, request protocol.BindRequest) (protocol.BindResponse, error) {
	return c.bind(ctx, session.BindTX, request)
}

func (c *Client) BindReceiver(ctx context.Context, request protocol.BindRequest) (protocol.BindResponse, error) {
	return c.bind(ctx, session.BindRX, request)
}

func (c *Client) BindTransceiver(ctx context.Context, request protocol.BindRequest) (protocol.BindResponse, error) {
	return c.bind(ctx, session.BindTRX, request)
}

func (c *Client) bind(ctx context.Context, mode session.BindMode, request protocol.BindRequest) (protocol.BindResponse, error) {
	sess, err := c.activeSession()
	if err != nil {
		return protocol.BindResponse{}, err
	}
	var response protocol.BindResponse
	switch mode {
	case session.BindTX:
		response, err = sess.BindTransmitter(ctx, request)
	case session.BindRX:
		response, err = sess.BindReceiver(ctx, request)
	default:
		response, err = sess.BindTransceiver(ctx, request)
	}
	if err == nil {
		profile := &bindProfile{mode: mode, request: cloneBindRequest(request)}
		c.mu.Lock()
		c.profile = profile
		c.mu.Unlock()
	}
	return response, err
}

func (c *Client) SubmitSM(ctx context.Context, request protocol.SubmitSM) (protocol.SubmitSMResp, error) {
	sess, err := c.activeSession()
	if err != nil {
		return protocol.SubmitSMResp{}, err
	}
	return sess.SubmitSM(ctx, request)
}

func (c *Client) DataSM(ctx context.Context, request protocol.DataSM) (protocol.DataSMResp, error) {
	sess, err := c.activeSession()
	if err != nil {
		return protocol.DataSMResp{}, err
	}
	return sess.DataSM(ctx, request)
}

func (c *Client) SubmitMulti(ctx context.Context, request protocol.SubmitMulti) (protocol.SubmitMultiResp, error) {
	sess, err := c.activeSession()
	if err != nil {
		return protocol.SubmitMultiResp{}, err
	}
	return sess.SubmitMulti(ctx, request)
}

func (c *Client) QuerySM(ctx context.Context, request protocol.QuerySM) (protocol.QuerySMResp, error) {
	sess, err := c.activeSession()
	if err != nil {
		return protocol.QuerySMResp{}, err
	}
	return sess.QuerySM(ctx, request)
}

func (c *Client) CancelSM(ctx context.Context, request protocol.CancelSM) error {
	sess, err := c.activeSession()
	if err != nil {
		return err
	}
	return sess.CancelSM(ctx, request)
}

func (c *Client) ReplaceSM(ctx context.Context, request protocol.ReplaceSM) error {
	sess, err := c.activeSession()
	if err != nil {
		return err
	}
	return sess.ReplaceSM(ctx, request)
}

func (c *Client) EnquireLink(ctx context.Context) error {
	sess, err := c.activeSession()
	if err != nil {
		return err
	}
	return sess.EnquireLink(ctx)
}

func (c *Client) Unbind(ctx context.Context) error {
	sess, err := c.activeSession()
	if err != nil {
		return err
	}
	return sess.Unbind(ctx)
}

func (c *Client) activeSession() (*session.Session, error) {
	c.mu.RLock()
	sess := c.current
	cause := c.lastErr
	reconnecting := !c.closed && c.config.Reconnect.Enabled && c.dialable
	closed := c.closed
	c.mu.RUnlock()
	if sess != nil && !closed {
		return sess, nil
	}
	return nil, &UnavailableError{Cause: cause, Reconnecting: reconnecting}
}

func (c *Client) lifecycle(initial *session.Session) {
	defer c.wg.Done()
	sess := initial
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-sess.Done():
		}
		sess.Wait()
		cause := sess.Err()
		c.mu.Lock()
		if c.current == sess {
			c.current = nil
			c.last = sess
			c.lastErr = cause
			c.lastLoss = cause
			c.signalLocked()
		}
		closed := c.closed
		enabled := c.config.Reconnect.Enabled && c.dialable
		c.mu.Unlock()
		if closed || !enabled {
			return
		}

		backoff := c.config.Reconnect.InitialBackoff
		for {
			if !waitContext(c.ctx, backoff) {
				return
			}
			conn, err := dialTransport(c.ctx, c.config)
			if err != nil {
				c.recordReconnectError(err)
				backoff = nextBackoff(backoff, c.config.Reconnect)
				continue
			}
			config := c.config.SessionConfig
			config.Role = session.RoleESME
			candidate, err := session.New(conn, config)
			if err != nil {
				_ = conn.Close()
				c.recordReconnectError(err)
				backoff = nextBackoff(backoff, c.config.Reconnect)
				continue
			}
			profile := c.bindProfileSnapshot()
			if profile != nil {
				bindCtx, cancel := context.WithTimeout(c.ctx, c.config.Reconnect.BindTimeout)
				err = bindSession(bindCtx, candidate, profile)
				cancel()
				if err != nil {
					_ = candidate.Close()
					candidate.Wait()
					c.recordReconnectError(err)
					backoff = nextBackoff(backoff, c.config.Reconnect)
					continue
				}
			}
			c.mu.Lock()
			if c.closed {
				c.mu.Unlock()
				_ = candidate.Close()
				candidate.Wait()
				return
			}
			c.current = candidate
			c.last = candidate
			c.lastErr = nil
			c.reconnects.Add(1)
			c.signalLocked()
			c.mu.Unlock()
			sess = candidate
			break
		}
	}
}

func (c *Client) bindProfileSnapshot() *bindProfile {
	c.mu.RLock()
	profile := c.profile
	if profile != nil {
		copyProfile := *profile
		copyProfile.request = cloneBindRequest(profile.request)
		profile = &copyProfile
	}
	c.mu.RUnlock()
	return profile
}

func (c *Client) recordReconnectError(err error) {
	c.mu.Lock()
	c.lastErr = err
	c.signalLocked()
	c.mu.Unlock()
}

func (c *Client) signalLocked() {
	close(c.notify)
	c.notify = make(chan struct{})
}

func bindSession(ctx context.Context, sess *session.Session, profile *bindProfile) error {
	switch profile.mode {
	case session.BindTX:
		_, err := sess.BindTransmitter(ctx, profile.request)
		return err
	case session.BindRX:
		_, err := sess.BindReceiver(ctx, profile.request)
		return err
	default:
		_, err := sess.BindTransceiver(ctx, profile.request)
		return err
	}
}

func cloneBindRequest(request protocol.BindRequest) protocol.BindRequest {
	request.SystemID = append([]byte(nil), request.SystemID...)
	request.Password = append([]byte(nil), request.Password...)
	request.SystemType = append([]byte(nil), request.SystemType...)
	request.AddressRange = append([]byte(nil), request.AddressRange...)
	return request
}

func waitContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func nextBackoff(current time.Duration, policy ReconnectPolicy) time.Duration {
	next := time.Duration(float64(current) * policy.Multiplier)
	if next <= current {
		next = current + time.Millisecond
	}
	if next > policy.MaxBackoff {
		return policy.MaxBackoff
	}
	return next
}
