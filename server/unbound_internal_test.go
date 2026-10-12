package server

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestHostKeyIsTheIPAlone(t *testing.T) {
	for _, c := range []struct {
		name string
		addr net.Addr
		want string
	}{
		{"ipv4 with port", &net.TCPAddr{IP: net.ParseIP("10.1.2.3"), Port: 5000}, "10.1.2.3"},
		{"another port, same key", &net.TCPAddr{IP: net.ParseIP("10.1.2.3"), Port: 6000}, "10.1.2.3"},
		{"ipv4-mapped ipv6 equals its ipv4 form", &net.TCPAddr{IP: net.ParseIP("::ffff:10.1.2.3"), Port: 1}, "10.1.2.3"},
		{"ipv6", &net.TCPAddr{IP: net.ParseIP("2001:db8::1"), Port: 1}, "2001:db8::1"},
		{"ipv6 zone dropped", &net.TCPAddr{IP: net.ParseIP("fe80::1"), Port: 1, Zone: "eth0"}, "fe80::1"},
		{"non-tcp address", fakeAddr("192.0.2.7:99"), "192.0.2.7"},
		{"nil", nil, ""},
	} {
		if got := hostKey(c.addr); got != c.want {
			t.Errorf("%s: hostKey = %q, want %q", c.name, got, c.want)
		}
	}
}

type fakeAddr string

func (a fakeAddr) Network() string { return "fake" }
func (a fakeAddr) String() string  { return string(a) }

// The per-IP index must not outlive its sessions, or an address that connected
// once would leave an entry behind forever.
func TestPerIPIndexIsEmptiedWhenSessionsClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv, err := Listen(ctx, Config{Address: "127.0.0.1:0", MaxUnboundPerIP: 5})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	go func() { _ = srv.Serve(ctx) }()

	var conns []net.Conn
	for i := 0; i < 3; i++ {
		c, err := net.Dial("tcp", srv.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
	}
	waitFor(t, func() bool { return len(srv.Sessions()) == 3 })
	srv.mu.Lock()
	indexed := len(srv.byHost["127.0.0.1"])
	srv.mu.Unlock()
	if indexed != 3 {
		t.Fatalf("index holds %d sessions for 127.0.0.1, want 3", indexed)
	}
	for _, c := range conns {
		_ = c.Close()
	}
	waitFor(t, func() bool { return len(srv.Sessions()) == 0 })
	waitFor(t, func() bool {
		srv.mu.Lock()
		defer srv.mu.Unlock()
		return len(srv.byHost) == 0
	})
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition not reached in 3s")
}
