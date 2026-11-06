package client

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/session"
	"github.com/majiddarvishan/go-smpp/transport"
)

func TestReconnectJitterStaysInsideBackoffCap(t *testing.T) {
	const base = 30 * time.Second
	if got := jitterReconnectDelay(base, 0); got != base {
		t.Fatalf("zero jitter changed backoff: %s", got)
	}
	for i := 0; i < 200; i++ {
		delay := jitterReconnectDelay(base, 0.20)
		if delay < 24*time.Second || delay > base {
			t.Fatalf("jitter delay %s outside [24s,30s]", delay)
		}
	}
	if got := normalizeReconnect(ReconnectPolicy{JitterFraction: 10}).JitterFraction; got != 1 {
		t.Fatalf("jitter ratio not capped: %f", got)
	}
}

func TestReconnectPermanentAuthClassification(t *testing.T) {
	for _, status := range []protocol.CommandStatus{
		protocol.StatusInvalidPassword,
		protocol.StatusInvalidSystemID,
		protocol.StatusInvalidSystemType,
	} {
		if !isPermanentBindRejection(&session.ResponseError{Status: status}) {
			t.Fatalf("status %x should stop optional auth retries", uint32(status))
		}
	}
	for _, status := range []protocol.CommandStatus{
		protocol.StatusSystemError,
		protocol.StatusBindFailed,
		protocol.StatusThrottled,
	} {
		if isPermanentBindRejection(&session.ResponseError{Status: status}) {
			t.Fatalf("status %x must remain retryable", uint32(status))
		}
	}
}

func TestReconnectStopsOnExplicitInvalidPassword(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	peerResult := make(chan error, 1)
	go func() {
		for i := 0; i < 2; i++ {
			conn, err := listener.Accept()
			if err != nil {
				peerResult <- err
				return
			}
			bind, err := readClientPDU(conn, registry)
			if err != nil {
				_ = conn.Close()
				peerResult <- err
				return
			}
			status := protocol.StatusOK
			body := any(protocol.BindResponse{SystemID: []byte("smsc")})
			if i == 1 {
				status = protocol.StatusInvalidPassword
				body = protocol.EmptyBody{}
			}
			frame, err := codec.EncodePDU(nil, codec.ResponseHeader(bind.Header, protocol.CommandBindTransceiverResp, status), body, registry)
			if err == nil {
				err = transport.WriteFull(conn, frame)
			}
			if err != nil {
				_ = conn.Close()
				peerResult <- err
				return
			}
			if i == 0 {
				time.Sleep(25 * time.Millisecond)
			}
			_ = conn.Close()
		}
		peerResult <- nil
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := Dial(ctx, Config{
		Address: listener.Addr().String(),
		Reconnect: ReconnectPolicy{
			Enabled: true, InitialBackoff: 5 * time.Millisecond,
			MaxBackoff: 10 * time.Millisecond, BindTimeout: time.Second,
			StopOnPermanentBindFailure: true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.BindTransceiver(ctx, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion34, SystemID: []byte("esme")}); err != nil {
		t.Fatalf("initial bind: %v", err)
	}
	for c.Metrics().ReconnectFailures == 0 {
		select {
		case <-ctx.Done():
			t.Fatal("did not observe rejected reconnect")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	waitErr := c.WaitConnected(ctx)
	var unavailable *UnavailableError
	if !errors.As(waitErr, &unavailable) || unavailable.Reconnecting {
		t.Fatalf("WaitConnected error=%v, want terminal UnavailableError", waitErr)
	}
	var reject *session.ResponseError
	if !errors.As(waitErr, &reject) || reject.Status != protocol.StatusInvalidPassword {
		t.Fatalf("terminal cause=%v, want InvalidPassword", waitErr)
	}
	if err := <-peerResult; err != nil {
		t.Fatalf("peer failure: %v", err)
	}
	if c.ReconnectCount() != 0 {
		t.Fatalf("unexpected successful reconnect count=%d", c.ReconnectCount())
	}
	_ = listener.(*net.TCPListener).SetDeadline(time.Now().Add(80 * time.Millisecond))
	if third, err := listener.Accept(); err == nil {
		_ = third.Close()
		t.Fatal("attempted additional reconnect after permanent credential rejection")
	} else if ne, ok := err.(net.Error); !ok || !ne.Timeout() {
		t.Fatalf("unexpected Accept error after permanent rejection: %v", err)
	}
}
