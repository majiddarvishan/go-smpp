package client

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/session"
	"github.com/majiddarvishan/go-smpp/transport"
)

func TestClientReconnectRebindWithoutHiddenResubmit(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}

	firstSeen := make(chan struct{})
	readySecond := make(chan struct{})
	peerErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			peerErr <- err
			return
		}
		if err := peerBindTRX(conn, registry); err != nil {
			peerErr <- err
			return
		}
		first, err := readClientPDU(conn, registry)
		if err != nil {
			peerErr <- err
			return
		}
		if first.Header.CommandID != protocol.CommandSubmitSM {
			peerErr <- errors.New("expected first submit_sm")
			return
		}
		close(firstSeen)
		_ = conn.Close() // delivery outcome is deliberately ambiguous

		conn, err = listener.Accept()
		if err != nil {
			peerErr <- err
			return
		}
		defer conn.Close()
		if err := peerBindTRX(conn, registry); err != nil {
			peerErr <- err
			return
		}

		// Prove the lost submit is not replayed automatically after rebind.
		_ = conn.SetReadDeadline(time.Now().Add(80 * time.Millisecond))
		if _, err := readClientPDU(conn, registry); err == nil {
			peerErr <- errors.New("ambiguous submit_sm was replayed after reconnect")
			return
		} else if ne, ok := err.(net.Error); !ok || !ne.Timeout() {
			peerErr <- err
			return
		}
		_ = conn.SetReadDeadline(time.Time{})
		close(readySecond)

		second, err := readClientPDU(conn, registry)
		if err != nil {
			peerErr <- err
			return
		}
		if second.Header.CommandID != protocol.CommandSubmitSM {
			peerErr <- errors.New("expected second submit_sm")
			return
		}
		body := second.Body.(protocol.SubmitSM)
		if string(body.DestinationAddr) != "222" {
			peerErr <- errors.New("unexpected second destination")
			return
		}
		frame, err := codec.EncodePDU(nil, codec.ResponseHeader(second.Header, protocol.CommandSubmitSMResp, protocol.StatusOK), protocol.SubmitSMResp{MessageID: []byte("second")}, registry)
		if err != nil {
			peerErr <- err
			return
		}
		peerErr <- transport.WriteFull(conn, frame)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := Dial(ctx, Config{
		Address: listener.Addr().String(),
		SessionConfig: session.Config{
			WindowSize: 1, ResponseTimeout: 500 * time.Millisecond,
			SessionInitTimeout: time.Second, EnquireLinkInterval: -1, InactivityTimeout: -1,
		},
		Reconnect: ReconnectPolicy{Enabled: true, InitialBackoff: 10 * time.Millisecond, MaxBackoff: 20 * time.Millisecond, BindTimeout: time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(); c.Wait() }()
	bindReq := protocol.BindRequest{SystemID: []byte("esme"), Password: []byte("pw"), InterfaceVersion: protocol.InterfaceVersion34}
	if _, err := c.BindTransceiver(ctx, bindReq); err != nil {
		t.Fatal(err)
	}

	firstResult := make(chan error, 1)
	go func() {
		_, err := c.SubmitSM(ctx, protocol.SubmitSM{DestinationAddr: []byte("111")})
		firstResult <- err
	}()
	select {
	case <-firstSeen:
	case <-ctx.Done():
		t.Fatal("peer did not see first submit")
	}
	var loss *session.LossError
	if err := <-firstResult; !errors.As(err, &loss) {
		t.Fatalf("first request error=%T %v, want session loss", err, err)
	}

	select {
	case <-readySecond:
	case <-ctx.Done():
		t.Fatal("client did not reconnect/rebind")
	}
	if err := c.WaitConnected(ctx); err != nil {
		t.Fatal(err)
	}
	if c.ReconnectCount() != 1 {
		t.Fatalf("reconnect count=%d", c.ReconnectCount())
	}
	if got := c.Session().Window().InUse; got != 0 {
		t.Fatalf("window leaked across reconnect: %d", got)
	}

	resp, err := c.SubmitSM(ctx, protocol.SubmitSM{DestinationAddr: []byte("222")})
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.MessageID) != "second" {
		t.Fatalf("message id=%q", resp.MessageID)
	}
	if err := <-peerErr; err != nil {
		t.Fatal(err)
	}
}

func TestClientReconnectPreservesFatalProtocolLossReason(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	rebound := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		if peerBindTRX(conn, registry) != nil {
			return
		}
		bad := make([]byte, codec.HeaderSize)
		binary.BigEndian.PutUint32(bad[:4], 7)
		_, _ = conn.Write(bad)
		_ = conn.Close()
		conn, err = listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		if peerBindTRX(conn, registry) != nil {
			return
		}
		close(rebound)
		<-time.After(100 * time.Millisecond)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := Dial(ctx, Config{
		Address:       listener.Addr().String(),
		SessionConfig: session.Config{SessionInitTimeout: time.Second, EnquireLinkInterval: -1, InactivityTimeout: -1},
		Reconnect:     ReconnectPolicy{Enabled: true, InitialBackoff: 10 * time.Millisecond, MaxBackoff: 20 * time.Millisecond, BindTimeout: time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close(); c.Wait() }()
	if _, err := c.BindTransceiver(ctx, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion34}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-rebound:
	case <-ctx.Done():
		t.Fatal("fatal-protocol connection was not replaced")
	}
	if err := c.WaitConnected(ctx); err != nil {
		t.Fatal(err)
	}
	var fatal *protocol.FatalError
	if !errors.As(c.LastLoss(), &fatal) {
		t.Fatalf("last loss=%T %v, want fatal protocol error", c.LastLoss(), c.LastLoss())
	}
}

func TestClientCloseStopsReconnectBackoff(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	closedFirst := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		if peerBindTRX(conn, registry) != nil {
			return
		}
		_ = conn.Close()
		close(closedFirst)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c, err := Dial(ctx, Config{
		Address:       listener.Addr().String(),
		SessionConfig: session.Config{SessionInitTimeout: time.Second, EnquireLinkInterval: -1, InactivityTimeout: -1},
		Reconnect:     ReconnectPolicy{Enabled: true, InitialBackoff: 300 * time.Millisecond, MaxBackoff: 300 * time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.BindTransceiver(ctx, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion34}); err != nil {
		t.Fatal(err)
	}
	<-closedFirst
	deadline := time.Now().Add(time.Second)
	for c.LastLoss() == nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	c.Wait()
	if c.ReconnectCount() != 0 {
		t.Fatalf("reconnect count after Close=%d", c.ReconnectCount())
	}
}

func peerBindTRX(conn net.Conn, registry *codec.Registry) error {
	bind, err := readClientPDU(conn, registry)
	if err != nil {
		return err
	}
	if bind.Header.CommandID != protocol.CommandBindTransceiver {
		return errors.New("expected bind_transceiver")
	}
	frame, err := codec.EncodePDU(nil, codec.ResponseHeader(bind.Header, protocol.CommandBindTransceiverResp, protocol.StatusOK), protocol.BindResponse{SystemID: []byte("smsc")}, registry)
	if err != nil {
		return err
	}
	return transport.WriteFull(conn, frame)
}

func readClientPDU(conn net.Conn, registry *codec.Registry) (codec.DecodedPDU, error) {
	prefix := make([]byte, 4)
	if _, err := io.ReadFull(conn, prefix); err != nil {
		return codec.DecodedPDU{}, err
	}
	length := binary.BigEndian.Uint32(prefix)
	if length < codec.HeaderSize {
		return codec.DecodedPDU{}, errors.New("invalid frame")
	}
	frame := make([]byte, int(length))
	copy(frame, prefix)
	if _, err := io.ReadFull(conn, frame[4:]); err != nil {
		return codec.DecodedPDU{}, err
	}
	return codec.DecodePDU(frame, registry)
}
