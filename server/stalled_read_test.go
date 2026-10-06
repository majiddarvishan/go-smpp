package server_test

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/server"
	"github.com/majiddarvishan/go-smpp/session"
)

// Task 6.3, over a real TCP connection and a real server. The session package
// tests (session/stalled_read_test.go) pin the mechanism on net.Pipe; these
// check the whole path: the peer's socket really is closed and the server
// really forgets the session.

func rawBindFrame(t *testing.T) []byte {
	t.Helper()
	registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	frame, err := codec.EncodePDU(nil, codec.Header{CommandID: protocol.CommandBindTransceiver, SequenceNumber: 1}, protocol.BindRequest{
		SystemID: []byte("esme"), Password: []byte("pw"), InterfaceVersion: protocol.InterfaceVersion34,
	}, registry)
	if err != nil {
		t.Fatal(err)
	}
	return frame
}

func acceptAll() server.Authenticator {
	return server.AuthenticatorFunc(func(context.Context, *session.Session, session.BindMode, protocol.BindRequest) (server.BindResult, error) {
		return server.BindResult{Status: protocol.StatusOK, SystemID: []byte("smsc")}, nil
	})
}

func listenForStallTest(t *testing.T, cfg session.Config) *server.Server {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	srv, err := server.Listen(ctx, server.Config{Address: "127.0.0.1:0", SessionConfig: cfg, Authenticator: acceptAll()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	startServer(t, ctx, srv)
	return srv
}

// closedByServer reports whether the server closes conn within the limit,
// draining anything it sends first.
func closedByServer(conn net.Conn, within time.Duration) bool {
	_ = conn.SetReadDeadline(time.Now().Add(within))
	_, err := io.Copy(io.Discard, conn)
	return err == nil // io.Copy returns nil on EOF; a deadline error means still open
}

func TestServerClosesAClientThatSendsHalfABindAndStalls(t *testing.T) {
	cfg := quietSessionConfig()
	cfg.SessionInitTimeout = 300 * time.Millisecond
	srv := listenForStallTest(t, cfg)

	conn, err := net.DialTimeout("tcp", srv.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	frame := rawBindFrame(t)
	if _, err := conn.Write(frame[:len(frame)/2]); err != nil {
		t.Fatal(err)
	}
	waitSessions(t, srv, 1, time.Second) // it is held while the PDU is unfinished...
	if !closedByServer(conn, 3*time.Second) {
		t.Fatal("the server never closed a connection stalled half way through its bind")
	}
	waitSessions(t, srv, 0, time.Second) // ...and then forgotten
}

func TestServerClosesABoundClientThatStallsMidPDU(t *testing.T) {
	cfg := quietSessionConfig()
	cfg.EnquireLinkInterval = 100 * time.Millisecond
	cfg.EnquireLinkTimeout = 100 * time.Millisecond
	cfg.InactivityTimeout = 30 * time.Second // so only the enquire_link path can end this
	srv := listenForStallTest(t, cfg)

	conn, err := net.DialTimeout("tcp", srv.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write(rawBindFrame(t)); err != nil {
		t.Fatal(err)
	}
	// read the bind_resp: header first, then the rest of its declared length
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		t.Fatalf("no bind_resp: %v", err)
	}
	if _, err := io.ReadFull(conn, make([]byte, binary.BigEndian.Uint32(head)-4)); err != nil {
		t.Fatalf("short bind_resp: %v", err)
	}

	// Now bound. Start a PDU that declares 1 MiB and never finish it.
	unfinished := make([]byte, 12)
	binary.BigEndian.PutUint32(unfinished, 1<<20)
	if _, err := conn.Write(unfinished); err != nil {
		t.Fatal(err)
	}
	if !closedByServer(conn, 3*time.Second) {
		t.Fatal("the server never closed a bound client stalled mid-PDU")
	}
	waitSessions(t, srv, 0, time.Second)
}
