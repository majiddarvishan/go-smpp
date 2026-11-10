package server_test

import (
	"bytes"
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/server"
	"github.com/majiddarvishan/go-smpp/session"
)

// Regression for the reported ESME interop run: a rejected bind_transceiver
// is a complete 16-octet SMPP error PDU, not a truncated success response.
func TestServerBindPasswordRejectionWireFormat(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv, err := server.Listen(ctx, server.Config{
		Address:       "127.0.0.1:0",
		SessionConfig: quietSessionConfig(),
		Authenticator: server.AuthenticatorFunc(func(_ context.Context, _ *session.Session, _ session.BindMode, request protocol.BindRequest) (server.BindResult, error) {
			if string(request.Password) != "valid-test-password" {
				return server.BindResult{Status: protocol.StatusInvalidPassword}, nil
			}
			return server.BindResult{Status: protocol.StatusOK, SystemID: []byte("smsc")}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	_ = startServer(t, ctx, srv)

	registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		password string
		status   protocol.CommandStatus
		body     []byte
	}{
		{"invalid_password", "wrong-test-password", protocol.StatusInvalidPassword, nil},
		{"valid_password", "valid-test-password", protocol.StatusOK, []byte{'s', 'm', 's', 'c', 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn, err := net.DialTimeout("tcp", srv.Addr().String(), 2*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
				t.Fatal(err)
			}
			request, err := codec.EncodePDU(nil, codec.Header{
				CommandID: protocol.CommandBindTransceiver, SequenceNumber: 0xadc00008,
			}, protocol.BindRequest{
				SystemID: []byte("peer"), Password: []byte(tc.password),
				InterfaceVersion: protocol.InterfaceVersion34,
			}, registry)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Write(request); err != nil {
				t.Fatal(err)
			}
			headerBytes := make([]byte, codec.HeaderSize)
			if _, err := io.ReadFull(conn, headerBytes); err != nil {
				t.Fatalf("read BindResp header: %v", err)
			}
			header, err := codec.DecodeHeader(headerBytes)
			if err != nil {
				t.Fatal(err)
			}
			wantLen := codec.HeaderSize + len(tc.body)
			if header.CommandID != protocol.CommandBindTransceiverResp ||
				header.CommandStatus != tc.status ||
				header.SequenceNumber != 0xadc00008 ||
				header.CommandLength != uint32(wantLen) {
				t.Fatalf("unexpected BindResp wire header: %+v, want status=%v length=%d", header, tc.status, wantLen)
			}
			frame := append([]byte(nil), headerBytes...)
			if len(tc.body) > 0 {
				body := make([]byte, len(tc.body))
				if _, err := io.ReadFull(conn, body); err != nil {
					t.Fatalf("read BindResp body: %v", err)
				}
				if !bytes.Equal(body, tc.body) {
					t.Fatalf("unexpected BindResp body %x, want %x", body, tc.body)
				}
				frame = append(frame, body...)
			}
			if _, err := codec.DecodePDU(frame, registry); err != nil {
				t.Fatalf("DecodePDU rejected server BindResp: %v", err)
			}
		})
	}
}
