package transport

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
)

var ErrNoProgress = errors.New("smpp transport: write made no progress")

// DialTCP establishes a plain TCP connection using the supplied dialer. A nil
// dialer uses net.Dialer defaults.
func DialTCP(ctx context.Context, network, address string, dialer *net.Dialer) (net.Conn, error) {
	if network == "" {
		network = "tcp"
	}
	if dialer == nil {
		dialer = &net.Dialer{}
	}
	return dialer.DialContext(ctx, network, address)
}

// ListenTCP creates a plain TCP listener using the supplied ListenConfig. A nil
// config uses net.ListenConfig defaults.
func ListenTCP(ctx context.Context, network, address string, config *net.ListenConfig) (net.Listener, error) {
	if network == "" {
		network = "tcp"
	}
	if config == nil {
		config = &net.ListenConfig{}
	}
	return config.Listen(ctx, network, address)
}

// TLSClient wraps an already established TCP-compatible stream as a TLS client.
// The caller controls certificate and cipher policy through tls.Config.
func TLSClient(conn net.Conn, config *tls.Config) *tls.Conn {
	return tls.Client(conn, config)
}

// TLSServer wraps an already accepted TCP-compatible stream as a TLS server.
func TLSServer(conn net.Conn, config *tls.Config) *tls.Conn {
	return tls.Server(conn, config)
}

// DialTLS establishes TCP first and then performs a TLS client handshake. The
// underlying TCP connection is closed if the handshake fails.
func DialTLS(ctx context.Context, network, address string, dialer *net.Dialer, config *tls.Config) (net.Conn, error) {
	conn, err := DialTCP(ctx, network, address, dialer)
	if err != nil {
		return nil, err
	}
	tlsConn := TLSClient(conn, config)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return tlsConn, nil
}

// ListenTLS creates a TCP listener and wraps accepted connections with TLS.
func ListenTLS(ctx context.Context, network, address string, listenConfig *net.ListenConfig, tlsConfig *tls.Config) (net.Listener, error) {
	listener, err := ListenTCP(ctx, network, address, listenConfig)
	if err != nil {
		return nil, err
	}
	return tls.NewListener(listener, tlsConfig), nil
}

// WriteFull serializes one already-encoded PDU to a stream, handling short
// writes. Returning from this function with nil means every octet was handed to
// the transport. Request response-timeout accounting may therefore start only
// after this function succeeds.
func WriteFull(conn net.Conn, p []byte) error {
	for len(p) > 0 {
		n, err := conn.Write(p)
		if n > 0 {
			p = p[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrNoProgress
		}
	}
	return nil
}

// ReadFull is exposed only as a small transport helper for tests/simulators and
// protocol integrations that need an exact byte count. Session framing itself
// remains stream-oriented and does not use one-read-per-PDU assumptions.
func ReadFull(conn net.Conn, p []byte) error {
	_, err := io.ReadFull(conn, p)
	return err
}
