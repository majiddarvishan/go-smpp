package client

import (
	"context"
	"crypto/tls"
	"net"

	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/session"
	"github.com/majiddarvishan/go-smpp/transport"
)

// Config configures a non-reconnecting ESME client. Automatic reconnect/rebind
// is intentionally added in its dedicated phase; the active Client API is
// already safe for concurrent request calls.
type Config struct {
	Address       string
	Network       string
	Dialer        *net.Dialer
	TLSConfig     *tls.Config
	SessionConfig session.Config
}

// Client is a thin concurrent-safe ESME facade over the shared Session core.
type Client struct {
	session *session.Session
}

func Dial(ctx context.Context, config Config) (*Client, error) {
	var (
		conn net.Conn
		err  error
	)
	if config.TLSConfig != nil {
		conn, err = transport.DialTLS(ctx, config.Network, config.Address, config.Dialer, config.TLSConfig)
	} else {
		conn, err = transport.DialTCP(ctx, config.Network, config.Address, config.Dialer)
	}
	if err != nil {
		return nil, err
	}
	client, err := New(conn, config.SessionConfig)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return client, nil
}

// New attaches the ESME client API to a caller-supplied TCP-compatible stream.
func New(conn net.Conn, config session.Config) (*Client, error) {
	config.Role = session.RoleESME
	sess, err := session.New(conn, config)
	if err != nil {
		return nil, err
	}
	return &Client{session: sess}, nil
}

func (c *Client) Session() *session.Session { return c.session }
func (c *Client) Close() error             { return c.session.Close() }

func (c *Client) BindTransmitter(ctx context.Context, request protocol.BindRequest) (protocol.BindResponse, error) {
	return c.session.BindTransmitter(ctx, request)
}

func (c *Client) BindReceiver(ctx context.Context, request protocol.BindRequest) (protocol.BindResponse, error) {
	return c.session.BindReceiver(ctx, request)
}

func (c *Client) BindTransceiver(ctx context.Context, request protocol.BindRequest) (protocol.BindResponse, error) {
	return c.session.BindTransceiver(ctx, request)
}

func (c *Client) SubmitSM(ctx context.Context, request protocol.SubmitSM) (protocol.SubmitSMResp, error) {
	return c.session.SubmitSM(ctx, request)
}

func (c *Client) EnquireLink(ctx context.Context) error { return c.session.EnquireLink(ctx) }
func (c *Client) Unbind(ctx context.Context) error      { return c.session.Unbind(ctx) }
