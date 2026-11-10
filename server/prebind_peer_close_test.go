package server_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/majiddarvishan/go-smpp/client"
	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/server"
	"github.com/majiddarvishan/go-smpp/session"
)

// A client may open a TCP connection and close it before sending any SMPP PDU.
// The listener must release that unbound session's slot and accept a later
// legitimate bind, even with MaxSessions=1. This does not infer why a peer
// made the pre-bind connection.
func TestServerPreBindPeerCloseDoesNotBlockNextBind(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv, err := server.Listen(ctx, server.Config{
		Address:       "127.0.0.1:0",
		MaxSessions:   1,
		SessionConfig: quietSessionConfig(),
		Authenticator: server.AuthenticatorFunc(func(_ context.Context, _ *session.Session, _ session.BindMode, _ protocol.BindRequest) (server.BindResult, error) {
			return server.BindResult{Status: protocol.StatusOK, SystemID: []byte("smsc")}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	_ = startServer(t, ctx, srv)

	for i := 0; i < 5; i++ {
		conn, err := net.DialTimeout("tcp", srv.Addr().String(), time.Second)
		if err != nil {
			t.Fatalf("pre-bind connection %d: %v", i, err)
		}
		waitSessions(t, srv, 1, time.Second)
		if err := conn.Close(); err != nil {
			t.Fatalf("close pre-bind connection %d: %v", i, err)
		}
		waitSessions(t, srv, 0, time.Second)
	}

	cli, err := client.Dial(ctx, client.Config{
		Address:       srv.Addr().String(),
		SessionConfig: quietSessionConfig(),
	})
	if err != nil {
		t.Fatalf("dial after pre-bind disconnects: %v", err)
	}
	defer cli.Close()
	resp, err := cli.BindTransceiver(ctx, protocol.BindRequest{
		SystemID: []byte("peer"), InterfaceVersion: protocol.InterfaceVersion34,
	})
	if err != nil {
		t.Fatalf("bind after pre-bind disconnects: %v", err)
	}
	if string(resp.SystemID) != "smsc" {
		t.Fatalf("BindResp system_id=%q, want smsc", resp.SystemID)
	}
	waitSessions(t, srv, 1, time.Second)
}
