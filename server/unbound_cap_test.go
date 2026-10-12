package server_test

import (
	"context"
	"io"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/majiddarvishan/go-smpp/client"
	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/server"
)

// MaxUnboundPerIP (Phase 5 backlog item, SEC1 remainder). Connections that never
// bind are invisible to BindRateLimiter; the cap is what stops one address from
// holding every MaxSessions slot.

func capServer(t *testing.T, cfg server.Config) *server.Server {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	cfg.Address = "127.0.0.1:0"
	if cfg.SessionConfig.ResponseTimeout == 0 {
		cfg.SessionConfig = quietSessionConfig()
		cfg.SessionConfig.SessionInitTimeout = 30 * time.Second // never the thing that frees a slot in these tests
	}
	if cfg.Authenticator == nil {
		cfg.Authenticator = acceptAll()
	}
	srv, err := server.Listen(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	startServer(t, ctx, srv)
	return srv
}

// dialFrom connects from a specific loopback source address (127.0.0.0/8 is all
// local on Linux); the test is skipped where the OS will not allow it.
func dialFrom(t *testing.T, srv *server.Server, source string) net.Conn {
	t.Helper()
	d := net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP(source)}, Timeout: time.Second}
	conn, err := d.Dial("tcp", srv.Addr().String())
	if err != nil {
		t.Skipf("cannot connect from %s on this host: %v", source, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// refused waits generously for the server to hang up on a connection it should
// not accept: it returns as soon as EOF arrives, so a slow CI machine only costs
// time, never a false "held open".
func refused(conn net.Conn) bool {
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, err := io.Copy(io.Discard, conn)
	return err == nil // EOF: the server hung up
}

// held reports whether the server keeps conn open for a short while. A held
// connection can only fail this by being closed, never by being slow.
func held(conn net.Conn) bool {
	_ = conn.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	_, err := io.Copy(io.Discard, conn)
	return err != nil // a deadline error: still open
}

func TestUnboundCapRefusesTheConnectionOverTheLimit(t *testing.T) {
	srv := capServer(t, server.Config{MaxUnboundPerIP: 2})
	a := dialFrom(t, srv, "127.0.0.1")
	b := dialFrom(t, srv, "127.0.0.1")
	waitSessions(t, srv, 2, time.Second)
	if !held(a) || !held(b) {
		t.Fatal("a connection within the limit was closed")
	}
	c := dialFrom(t, srv, "127.0.0.1")
	if !refused(c) {
		t.Fatal("a third unbound connection from the same IP was held open despite MaxUnboundPerIP 2")
	}
	if got := srv.BindStats().RefusedUnbound; got != 1 {
		t.Fatalf("RefusedUnbound = %d, want 1", got)
	}
	if n := len(srv.Sessions()); n != 2 {
		t.Fatalf("%d sessions tracked, want the 2 within the limit", n)
	}
}

func TestUnboundCapFreesTheSlotWhenAnUnboundConnectionCloses(t *testing.T) {
	srv := capServer(t, server.Config{MaxUnboundPerIP: 1})
	a := dialFrom(t, srv, "127.0.0.1")
	waitSessions(t, srv, 1, time.Second)
	if !refused(dialFrom(t, srv, "127.0.0.1")) {
		t.Fatal("second connection not refused at limit 1")
	}
	_ = a.Close()
	waitSessions(t, srv, 0, 2*time.Second)
	d := dialFrom(t, srv, "127.0.0.1")
	waitSessions(t, srv, 1, time.Second)
	if !held(d) {
		t.Fatal("the slot was not freed after the unbound connection closed")
	}
}

// A bound session stops counting, so legitimate peers that have bound are
// unaffected, and a bound peer's own extra connections are only held to the cap
// while they are unbound.
func TestUnboundCapDoesNotCountBoundSessions(t *testing.T) {
	srv := capServer(t, server.Config{MaxUnboundPerIP: 1})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cli, err := client.Dial(ctx, client.Config{Address: srv.Addr().String(), SessionConfig: quietSessionConfig()})
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	if _, err := cli.BindTransceiver(ctx, protocol.BindRequest{SystemID: []byte("esme"), Password: []byte("pw"), InterfaceVersion: protocol.InterfaceVersion34}); err != nil {
		t.Fatal(err)
	}
	// The bound session no longer counts, so one unbound connection is still allowed...
	silent := dialFrom(t, srv, "127.0.0.1")
	waitSessions(t, srv, 2, time.Second)
	if !held(silent) {
		t.Fatal("the bound session was counted against the unbound cap")
	}
	// ...and the next unbound one is not.
	if !refused(dialFrom(t, srv, "127.0.0.1")) {
		t.Fatal("second unbound connection not refused")
	}
}

func TestUnboundCapIsPerIPAndExemptionsAreHonoured(t *testing.T) {
	srv := capServer(t, server.Config{MaxUnboundPerIP: 1, UnboundCapExempt: []netip.Prefix{netip.MustParsePrefix("127.0.0.3/32")}})
	_ = dialFrom(t, srv, "127.0.0.1")
	waitSessions(t, srv, 1, time.Second)
	if !refused(dialFrom(t, srv, "127.0.0.1")) {
		t.Fatal("the same IP was not capped")
	}
	if !held(dialFrom(t, srv, "127.0.0.2")) {
		t.Fatal("a different IP was refused: the cap is not per IP")
	}
	for i := 0; i < 3; i++ { // exempt: three unbound connections from 127.0.0.3
		if !held(dialFrom(t, srv, "127.0.0.3")) {
			t.Fatalf("exempt address refused on connection %d", i+1)
		}
	}
}

// legitimateSessionPresent reports whether the server still tracks a session from
// the legitimate peer's address (127.0.0.2).
func legitimateSessionPresent(srv *server.Server) bool {
	for _, s := range srv.Sessions() {
		if strings.HasPrefix(s.RemoteAddr().String(), "127.0.0.2:") {
			return true
		}
	}
	return false
}

func TestUnboundCapDefaultIsUnlimited(t *testing.T) {
	srv := capServer(t, server.Config{})
	for i := 0; i < 12; i++ {
		_ = dialFrom(t, srv, "127.0.0.1")
	}
	waitSessions(t, srv, 12, 2*time.Second)
	if got := srv.BindStats().RefusedUnbound; got != 0 {
		t.Fatalf("RefusedUnbound = %d with the cap off", got)
	}
}

// The Phase 5 exit criterion, as measured in the earlier probe: one address
// flooding silent connections must not keep a legitimate peer out of MaxSessions.
// Without the cap the attacker below fills all three slots and the legitimate
// peer is refused every time (it was 268 of 268 in the original probe).
func TestUnboundFloodFromOneAddressCannotExhaustMaxSessions(t *testing.T) {
	cfg := quietSessionConfig()
	cfg.SessionInitTimeout = 300 * time.Millisecond
	srv := capServer(t, server.Config{SessionConfig: cfg, MaxSessions: 3, MaxUnboundPerIP: 1})

	deadline := time.Now().Add(1500 * time.Millisecond)
	var served, refusedCount int
	for time.Now().Before(deadline) {
		// the attacker tops itself up as fast as it can
		if c, err := (&net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1")}, Timeout: time.Second}).Dial("tcp", srv.Addr().String()); err == nil {
			defer c.Close()
		}
		// a legitimate peer from another address tries to get a session
		conn := dialFrom(t, srv, "127.0.0.2")
		_ = conn.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
		if _, err := conn.Read(make([]byte, 1)); err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				served++
			} else {
				refusedCount++
			}
		}
		_ = conn.Close()
		// Let the server notice that this connection closed before the next try. A peer that
		// drops an unbound connection and reconnects before the server has seen the close is
		// still counted once (see D3), and this test is about the attacker, not about that.
		for i := 0; i < 400 && legitimateSessionPresent(srv); i++ {
			time.Sleep(5 * time.Millisecond)
		}
	}
	if served == 0 || refusedCount > 0 {
		t.Fatalf("legitimate peer: held open %d times, refused %d times; want never refused", served, refusedCount)
	}
	if srv.BindStats().RefusedUnbound == 0 {
		t.Fatal("the flood was never refused by the cap; the test did not exercise it")
	}
}
