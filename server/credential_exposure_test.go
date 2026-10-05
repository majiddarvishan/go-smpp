package server_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/majiddarvishan/go-smpp/client"
	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/server"
	"github.com/majiddarvishan/go-smpp/session"
)

// SEC3. Bind and outbind PDUs carry the password in clear text. The library's
// own outputs - structured logs, observer events and the errors it returns -
// must never contain it. The one deliberate exception, raw packet tracing, is
// opt-in, documented on PacketTracer, and pinned by the positive control below
// so that this test provably CAN see a leak when there is one.

const (
	// 8 octets: the longest password SMPP 3.4 allows.
	leakMarker = "Zq7!pwXk"
	// Sent over-long and unterminated to drive the malformed-PDU (fatal) path.
	longLeakMarker = "LONGSECRETPASSWORD"
)

type captured struct {
	mu     sync.Mutex
	logs   bytes.Buffer
	events []session.Event
	errs   []string
}

func (c *captured) event(e session.Event) { c.mu.Lock(); c.events = append(c.events, e); c.mu.Unlock() }
func (c *captured) err(err error) {
	if err != nil {
		c.mu.Lock()
		c.errs = append(c.errs, fmt.Sprintf("%v | %+v | %#v", err, err, err))
		c.mu.Unlock()
	}
}

// text is everything observable, rendered every way a careless formatter could.
func (c *captured) text() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var b strings.Builder
	b.Write(c.logs.Bytes())
	for _, e := range c.events {
		fmt.Fprintf(&b, "%v %+v %#v\n", e, e, e)
	}
	for _, e := range c.errs {
		b.WriteString(e + "\n")
	}
	return b.String()
}

type lockedWriter struct {
	c *captured
}

func (w lockedWriter) Write(p []byte) (int, error) {
	w.c.mu.Lock()
	defer w.c.mu.Unlock()
	return w.c.logs.Write(p)
}

func debugLogger(c *captured) *slog.Logger {
	return slog.New(slog.NewTextHandler(lockedWriter{c}, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func leakCheckConfig(c *captured) session.Config {
	cfg := quietSessionConfig()
	cfg.Logger = debugLogger(c)
	cfg.Observer = session.ObserverFunc(c.event)
	return cfg
}

func assertNoLeak(t *testing.T, c *captured, secrets ...string) {
	t.Helper()
	out := c.text()
	for _, s := range secrets {
		// Not just the whole password: any 5-octet run of it. A decoder that
		// echoes the field it choked on is bounded by the field's maximum
		// length, so it would show a prefix, never the full value.
		for i := 0; i+5 <= len(s); i++ {
			if strings.Contains(out, s[i:i+5]) {
				t.Fatalf("part of password %q (%q) appears in the library's log/event/error output:\n%s", s, s[i:i+5], out)
			}
		}
		// also as the decimal byte list a %v of a []byte would print
		var dec []string
		for _, b := range []byte(s) {
			dec = append(dec, fmt.Sprint(b))
		}
		if strings.Contains(out, "["+strings.Join(dec, " ")) {
			t.Fatalf("password %q appears as a byte list in output:\n%s", s, out)
		}
	}
	if len(out) == 0 {
		t.Fatal("captured nothing at all; the test cannot prove anything")
	}
}

func TestPasswordNeverReachesLogsEventsOrErrors(t *testing.T) {
	c := &captured{}
	// Also capture slog's default logger, so code that logs without going
	// through Config.Logger (this package has none today) cannot slip past.
	prev := slog.Default()
	slog.SetDefault(debugLogger(c))
	defer slog.SetDefault(prev)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv, err := server.Listen(ctx, server.Config{
		Address:       "127.0.0.1:0",
		SessionConfig: leakCheckConfig(c),
		Authenticator: server.AuthenticatorFunc(func(_ context.Context, _ *session.Session, _ session.BindMode, r protocol.BindRequest) (server.BindResult, error) {
			if string(r.Password) != leakMarker {
				return server.BindResult{Status: protocol.StatusInvalidPassword}, nil
			}
			return server.BindResult{Status: protocol.StatusOK, SystemID: []byte("smsc")}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	startServer(t, ctx, srv)
	addr := srv.Addr().String()

	bind := func(password string) error {
		cli, err := client.Dial(ctx, client.Config{Address: addr, SessionConfig: leakCheckConfig(c)})
		if err != nil {
			return err
		}
		defer cli.Close()
		_, err = cli.BindTransceiver(ctx, protocol.BindRequest{SystemID: []byte("esme"), Password: []byte(password), InterfaceVersion: protocol.InterfaceVersion34})
		return err
	}

	c.err(bind(leakMarker))          // accepted
	c.err(bind("Wr0ng!pw"))          // rejected: exercises the failure path
	c.err(bind(leakMarker + "\x00")) // client-side encode error path (embedded NUL)
	c.err(bind(longLeakMarker))      // client-side encode error path (too long)

	// A hostile peer sends a malformed bind: password longer than the
	// field allows and never NUL-terminated. This is the path that logs a
	// fatal protocol error on the server.
	raw, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	body := append([]byte("esme\x00"), []byte(longLeakMarker)...) // no terminator within 9 octets
	frame := make([]byte, codec.HeaderSize+len(body))
	if err := codec.EncodeHeader(frame, codec.Header{CommandLength: uint32(len(frame)), CommandID: protocol.CommandBindTransceiver, SequenceNumber: 1}); err != nil {
		t.Fatal(err)
	}
	copy(frame[codec.HeaderSize:], body)
	if _, err := raw.Write(frame); err != nil {
		t.Fatal(err)
	}
	_ = raw.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, _ = raw.Read(make([]byte, 64)) // wait for the server to drop us
	raw.Close()
	waitSessions(t, srv, 0, 2*time.Second)

	assertNoLeak(t, c, leakMarker, "Wr0ng!pw", longLeakMarker)
	if !strings.Contains(c.text(), "smpp_protocol_fatal") {
		t.Fatalf("the malformed-bind scenario never reached the fatal-error log path, so it proved nothing:\n%s", c.text())
	}
}

// Positive control. Raw packet tracing is the documented, opt-in way to see
// credentials; if this stops finding the password, the test above could be
// passing for the wrong reason and the PacketTracer warning may be stale.
func TestRawPacketTraceDoesExposeThePasswordWhenEnabled(t *testing.T) {
	var mu sync.Mutex
	var raws [][]byte
	traceCfg := quietSessionConfig()
	traceCfg.TraceRawPDU = true
	traceCfg.PacketTracer = session.PacketTracerFunc(func(p session.PacketTrace) {
		mu.Lock()
		raws = append(raws, p.RawPDU)
		mu.Unlock()
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv, err := server.Listen(ctx, server.Config{Address: "127.0.0.1:0", SessionConfig: quietSessionConfig(),
		Authenticator: server.AuthenticatorFunc(func(context.Context, *session.Session, session.BindMode, protocol.BindRequest) (server.BindResult, error) {
			return server.BindResult{Status: protocol.StatusOK, SystemID: []byte("smsc")}, nil
		})})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	startServer(t, ctx, srv)

	cli, err := client.Dial(ctx, client.Config{Address: srv.Addr().String(), SessionConfig: traceCfg})
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	if _, err := cli.BindTransceiver(ctx, protocol.BindRequest{SystemID: []byte("esme"), Password: []byte(leakMarker), InterfaceVersion: protocol.InterfaceVersion34}); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, r := range raws {
		if bytes.Contains(r, []byte(leakMarker)) {
			return
		}
	}
	t.Fatalf("TraceRawPDU captured %d packets and none contained the password; the documented exposure no longer matches reality", len(raws))
}

// With raw tracing off (the default) a tracer must receive no payload at all.
func TestPacketTraceCarriesNoPayloadByDefault(t *testing.T) {
	var mu sync.Mutex
	var got []session.PacketTrace
	cfg := quietSessionConfig()
	cfg.PacketTracer = session.PacketTracerFunc(func(p session.PacketTrace) { mu.Lock(); got = append(got, p); mu.Unlock() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv, err := server.Listen(ctx, server.Config{Address: "127.0.0.1:0", SessionConfig: cfg,
		Authenticator: server.AuthenticatorFunc(func(context.Context, *session.Session, session.BindMode, protocol.BindRequest) (server.BindResult, error) {
			return server.BindResult{Status: protocol.StatusOK, SystemID: []byte("smsc")}, nil
		})})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	startServer(t, ctx, srv)
	cli, err := client.Dial(ctx, client.Config{Address: srv.Addr().String(), SessionConfig: quietSessionConfig()})
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	if _, err := cli.BindTransceiver(ctx, protocol.BindRequest{SystemID: []byte("esme"), Password: []byte(leakMarker), InterfaceVersion: protocol.InterfaceVersion34}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) == 0 {
		t.Fatal("tracer saw no packets; nothing was proved")
	}
	for _, p := range got {
		if p.RawPDU != nil {
			t.Fatalf("RawPDU populated (%d bytes) without Config.TraceRawPDU", len(p.RawPDU))
		}
	}
}
