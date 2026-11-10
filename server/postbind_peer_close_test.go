package server_test

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/majiddarvishan/go-smpp/client"
	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/server"
	"github.com/majiddarvishan/go-smpp/session"
)

// A peer may disconnect after transmitting a valid Bind but before the server
// writes BindResp, or after a completed Bind. Either way, a bounded listener
// must reclaim the original session and allow a later peer to bind. This test
// deliberately makes no claim about the external peer's reason for closing.
func TestServerPostBindPeerDisconnectReclaimsSessionSlot(t *testing.T) {
	cases := []struct {
		name               string
		closeBeforeResponse bool
		reset              bool
	}{
		{name: "peer_fin_while_bind_response_pending", closeBeforeResponse: true},
		{name: "peer_rst_while_bind_response_pending", closeBeforeResponse: true, reset: true},
		{name: "peer_close_after_successful_bind"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			authEntered := make(chan struct{}, 1)
			releaseAuth := make(chan struct{})
			var releaseOnce sync.Once
			unblockAuth := func() { releaseOnce.Do(func() { close(releaseAuth) }) }
			defer unblockAuth()

			config := quietSessionConfig()
			config.SessionInitTimeout = 5 * time.Second
			srv, err := server.Listen(ctx, server.Config{
				Address:       "127.0.0.1:0",
				MaxSessions:   1,
				SessionConfig: config,
				Authenticator: server.AuthenticatorFunc(func(ctx context.Context, _ *session.Session, mode session.BindMode, request protocol.BindRequest) (server.BindResult, error) {
					if mode != session.BindTRX {
						return server.BindResult{Status: protocol.StatusBindFailed}, nil
					}
					if tc.closeBeforeResponse && string(request.SystemID) == "closing" {
						select {
						case authEntered <- struct{}{}:
						default:
						}
						select {
						case <-releaseAuth:
						case <-ctx.Done():
							return server.BindResult{Status: protocol.StatusSystemError}, ctx.Err()
						}
					}
					return server.BindResult{Status: protocol.StatusOK, SystemID: []byte("smsc")}, nil
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			defer srv.Close()
			_ = startServer(t, ctx, srv)

			if tc.closeBeforeResponse {
				registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
				if err != nil {
					t.Fatal(err)
				}
				bind, err := codec.EncodePDU(nil, codec.Header{
					CommandID:      protocol.CommandBindTransceiver,
					SequenceNumber: 0xe0000042,
				}, protocol.BindRequest{
					SystemID: []byte("closing"), InterfaceVersion: protocol.InterfaceVersion34,
				}, registry)
				if err != nil {
					t.Fatal(err)
				}
				peer, err := net.DialTimeout("tcp", srv.Addr().String(), 2*time.Second)
				if err != nil {
					t.Fatal(err)
				}
				defer peer.Close()
				if err := peer.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
					t.Fatal(err)
				}
				n, err := peer.Write(bind)
				if err != nil || n != len(bind) {
					t.Fatalf("send bind: written=%d of %d, error=%v", n, len(bind), err)
				}
				select {
				case <-authEntered:
				case <-time.After(3 * time.Second):
					t.Fatal("authenticator did not receive the pending Bind")
				}
				waitSessions(t, srv, 1, 3*time.Second)
				if tc.reset {
					tcp, ok := peer.(*net.TCPConn)
					if !ok {
						t.Fatalf("peer connection type %T is not TCP", peer)
					}
					if err := tcp.SetLinger(0); err != nil {
						t.Fatal(err)
					}
				}
				if err := peer.Close(); err != nil {
					t.Fatal(err)
				}
				// A closed peer need not receive BindResp, so the only invariant
				// is that the server releases this slot after terminal I/O.
				unblockAuth()
			} else {
				peer, err := client.Dial(ctx, client.Config{
					Address: srv.Addr().String(), SessionConfig: config,
				})
				if err != nil {
					t.Fatal(err)
				}
				_, err = peer.BindTransceiver(ctx, protocol.BindRequest{
					SystemID: []byte("closing"), InterfaceVersion: protocol.InterfaceVersion34,
				})
				if err != nil {
					_ = peer.Close()
					t.Fatalf("initial completed Bind: %v", err)
				}
				waitSessions(t, srv, 1, 3*time.Second)
				if err := peer.Close(); err != nil {
					t.Fatal(err)
				}
			}

			waitSessions(t, srv, 0, 3*time.Second)
			replacement, err := client.Dial(ctx, client.Config{
				Address: srv.Addr().String(), SessionConfig: config,
			})
			if err != nil {
				t.Fatalf("replacement dial: %v", err)
			}
			defer replacement.Close()
			response, err := replacement.BindTransceiver(ctx, protocol.BindRequest{
				SystemID: []byte("replacement"), InterfaceVersion: protocol.InterfaceVersion34,
			})
			if err != nil {
				t.Fatalf("replacement Bind after peer close: %v", err)
			}
			if string(response.SystemID) != "smsc" {
				t.Fatalf("replacement BindResp system_id=%q, want smsc", response.SystemID)
			}
			waitSessions(t, srv, 1, 3*time.Second)
			if err := replacement.Close(); err != nil {
				t.Fatal(err)
			}
			waitSessions(t, srv, 0, 3*time.Second)
		})
	}
}
