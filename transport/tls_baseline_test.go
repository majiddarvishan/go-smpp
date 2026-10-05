package transport

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

// SEC4. The package documentation recommends a TLS baseline (TLS 1.2 minimum,
// verified peer certificates, a ServerName, optionally mutual TLS) and says the
// package never overrides the caller's tls.Config. These tests pin each of those
// statements, so the documentation cannot drift from the behaviour.

// baselinePair returns a certificate usable as both server and client identity
// for 127.0.0.1, and a pool trusting it.
func baselinePair(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(7),
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, pool
}

// echoServer listens with ListenTLS and the given config, answering one
// "ping" with "pong". Its result (nil on success) arrives on the channel.
func echoServer(t *testing.T, ctx context.Context, cfg *tls.Config) (string, <-chan error) {
	t.Helper()
	l, err := ListenTLS(ctx, "tcp", "127.0.0.1:0", nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	done := make(chan error, 1)
	go func() {
		conn, err := l.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		buf := make([]byte, 4)
		if err := ReadFull(conn, buf); err != nil {
			done <- err
			return
		}
		done <- WriteFull(conn, []byte("pong"))
	}()
	return l.Addr().String(), done
}

// roundTrip dials and exchanges ping/pong, returning the first error on either
// the handshake or the exchange, plus the negotiated version when it worked.
func roundTrip(ctx context.Context, addr string, cfg *tls.Config) (uint16, error) {
	conn, err := DialTLS(ctx, "tcp", addr, nil, cfg)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if err := WriteFull(conn, []byte("ping")); err != nil {
		return 0, err
	}
	buf := make([]byte, 4)
	if err := ReadFull(conn, buf); err != nil {
		return 0, err
	}
	if string(buf) != "pong" {
		return 0, errors.New("unexpected reply " + string(buf))
	}
	return conn.(*tls.Conn).ConnectionState().Version, nil
}

func baselineCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestRecommendedTLSBaselineConnects(t *testing.T) {
	ctx := baselineCtx(t)
	cert, pool := baselinePair(t)
	addr, done := echoServer(t, ctx, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	version, err := roundTrip(ctx, addr, &tls.Config{RootCAs: pool, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatalf("the documented baseline does not connect: %v", err)
	}
	if version < tls.VersionTLS12 {
		t.Fatalf("negotiated version 0x%04x, below TLS 1.2", version)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// DialTLS, unlike crypto/tls.Dial, does not derive ServerName from the address.
// Documented, because the failure is otherwise a puzzle: the caller must set it.
func TestDialTLSDoesNotInferServerName(t *testing.T) {
	ctx := baselineCtx(t)
	cert, pool := baselinePair(t)
	addr, _ := echoServer(t, ctx, &tls.Config{Certificates: []tls.Certificate{cert}})
	_, err := roundTrip(ctx, addr, &tls.Config{RootCAs: pool}) // no ServerName
	if err == nil || !strings.Contains(err.Error(), "ServerName") {
		t.Fatalf("err = %v, want the 'ServerName or InsecureSkipVerify must be specified' error", err)
	}
}

// Peer certificates are verified unless the caller opts out.
func TestDialTLSVerifiesPeerCertificate(t *testing.T) {
	ctx := baselineCtx(t)
	cert, _ := baselinePair(t)
	addr, _ := echoServer(t, ctx, &tls.Config{Certificates: []tls.Certificate{cert}})
	_, err := roundTrip(ctx, addr, &tls.Config{RootCAs: x509.NewCertPool(), ServerName: "127.0.0.1"})
	var unknown x509.UnknownAuthorityError
	if !errors.As(err, &unknown) {
		t.Fatalf("err = %v (%T), want an x509 unknown-authority error", err, err)
	}
}

func TestRecommendedServerBaselineRejectsTLS11(t *testing.T) {
	ctx := baselineCtx(t)
	cert, pool := baselinePair(t)
	addr, _ := echoServer(t, ctx, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	_, err := roundTrip(ctx, addr, &tls.Config{RootCAs: pool, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS10, MaxVersion: tls.VersionTLS11})
	if err == nil {
		t.Fatal("a TLS 1.1 client connected to a server configured with MinVersion TLS 1.2")
	}
}

// The package must hand the caller's configuration to crypto/tls untouched.
// MinVersion TLS 1.3 is used here because a server's *default* floor is TLS
// 1.2 on current Go, so only a stricter caller choice can reveal a wrapper that
// dropped it.
func TestListenTLSHonoursCallerMinVersion(t *testing.T) {
	ctx := baselineCtx(t)
	cert, pool := baselinePair(t)
	addr, _ := echoServer(t, ctx, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13})
	_, err := roundTrip(ctx, addr, &tls.Config{RootCAs: pool, ServerName: "127.0.0.1", MaxVersion: tls.VersionTLS12})
	if err == nil {
		t.Fatal("a TLS 1.2-only client connected to a server configured with MinVersion TLS 1.3")
	}
}

// Mutual TLS is a pure pass-through of tls.Config; the docs recommend it for
// bilateral links, so it must actually work and actually refuse.
func TestMutualTLSIsHonouredWhenConfigured(t *testing.T) {
	cert, pool := baselinePair(t)
	serverCfg := func() *tls.Config {
		return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool}
	}

	ctx := baselineCtx(t)
	addr, done := echoServer(t, ctx, serverCfg())
	if _, err := roundTrip(ctx, addr, &tls.Config{RootCAs: pool, ServerName: "127.0.0.1", Certificates: []tls.Certificate{cert}}); err != nil {
		t.Fatalf("a client presenting a trusted certificate was refused: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	addr, done = echoServer(t, ctx, serverCfg())
	if _, err := roundTrip(ctx, addr, &tls.Config{RootCAs: pool, ServerName: "127.0.0.1"}); err == nil {
		t.Fatal("a client with no certificate was served by a server that requires one")
	}
	if err := <-done; err == nil {
		t.Fatal("the server completed an exchange with a client that presented no certificate")
	}
}

// The converse: a wrapper that forced something stricter than the caller asked
// for would break peers that legitimately need TLS 1.2. A server the caller
// capped at 1.2 must negotiate exactly 1.2.
func TestListenTLSHonoursCallerMaxVersion(t *testing.T) {
	ctx := baselineCtx(t)
	cert, pool := baselinePair(t)
	addr, done := echoServer(t, ctx, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12, MaxVersion: tls.VersionTLS12})
	version, err := roundTrip(ctx, addr, &tls.Config{RootCAs: pool, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatalf("a TLS 1.2-capped server refused a TLS 1.2+ client: %v", err)
	}
	if version != tls.VersionTLS12 {
		t.Fatalf("negotiated 0x%04x, want exactly TLS 1.2 (0x%04x)", version, tls.VersionTLS12)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
