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
			if string(request.Password) != "good1234" {
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
		{"invalid_password", "bad1234", protocol.StatusInvalidPassword, nil},
		{"valid_password", "good1234", protocol.StatusOK, []byte{'s', 'm', 's', 'c', 0}},
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
			if header.CommandID != protocol.CommandBindTransceiverResp ||
				header.CommandStatus != tc.status ||
				header.SequenceNumber != 0xadc00008 {
				t.Fatalf("unexpected BindResp wire header: %+v, want status=%v", header, tc.status)
			}
			// Failure is header-only. Successful BindResp may also include an
			// sc_interface_version TLV after the required system_id C-string.
			if tc.status != protocol.StatusOK && header.CommandLength != codec.HeaderSize {
				t.Fatalf("failed BindResp length=%d, want header-only %d", header.CommandLength, codec.HeaderSize)
			}
			if tc.status == protocol.StatusOK && header.CommandLength < uint32(codec.HeaderSize+len(tc.body)) {
				t.Fatalf("successful BindResp length=%d, missing system_id", header.CommandLength)
			}
			if header.CommandLength > 1024 {
				t.Fatalf("oversized BindResp length=%d", header.CommandLength)
			}
			frame := append([]byte(nil), headerBytes...)
			bodyLength := int(header.CommandLength) - codec.HeaderSize
			if bodyLength > 0 {
				body := make([]byte, bodyLength)
				if _, err := io.ReadFull(conn, body); err != nil {
					t.Fatalf("read BindResp body: %v", err)
				}
				if !bytes.HasPrefix(body, tc.body) {
					t.Fatalf("BindResp body %x missing expected prefix %x", body, tc.body)
				}
				frame = append(frame, body...)
			}
			decoded, err := codec.DecodePDU(frame, registry)
			if err != nil {
				t.Fatalf("DecodePDU rejected server BindResp: %v", err)
			}
			if tc.status == protocol.StatusOK {
				bind, ok := decoded.Body.(protocol.BindResponse)
				if !ok || !bytes.Equal(bind.SystemID, []byte("smsc")) {
					t.Fatalf("unexpected successful BindResp body: %#v", decoded.Body)
				}
			}
		})
	}
}
