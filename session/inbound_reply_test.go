package session

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/protocol"
)

func boundDeliverPair(t *testing.T, handler Handler, maxDeferred int) (*Session, *Session) {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	serverHandler := HandlerFunc(func(_ context.Context, _ *Session, pdu InboundPDU) (Response, error) {
		if pdu.Header.CommandID == protocol.CommandBindTransceiver {
			return Response{Status: protocol.StatusOK, Body: protocol.BindResponse{SystemID: []byte("smsc")}}, nil
		}
		return Response{Status: protocol.StatusSystemError, Body: protocol.EmptyBody{}}, nil
	})
	server, err := New(serverConn, Config{Role: RoleSMSC, Handler: serverHandler})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	client, err := New(clientConn, Config{Role: RoleESME, Handler: handler, MaxDeferredResponses: maxDeferred})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = client.BindTransceiver(ctx, protocol.BindRequest{SystemID: []byte("esme"), InterfaceVersion: protocol.InterfaceVersion34})
	if err != nil {
		t.Fatal(err)
	}
	return client, server
}

func TestInboundDeliverResponseSuppressDoesNotBlockRX(t *testing.T) {
	client, server := boundDeliverPair(t, HandlerFunc(func(_ context.Context, _ *Session, pdu InboundPDU) (Response, error) {
		if pdu.Header.CommandID == protocol.CommandDeliverSM {
			return Response{Action: ResponseSuppress}, nil
		}
		return Response{Status: protocol.StatusOK, Body: protocol.EmptyBody{}}, nil
	}), 1)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := server.DeliverSM(ctx, protocol.DeliverSM{DestinationAddr: []byte("1000")})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("suppressed response error=%v, want context deadline", err)
	}
	if got := client.PendingDeferredResponses(); got != 0 {
		t.Fatalf("suppress created %d deferred entries", got)
	}
	liveCtx, liveCancel := context.WithTimeout(context.Background(), time.Second)
	defer liveCancel()
	if err := client.EnquireLink(liveCtx); err != nil {
		t.Fatalf("receive loop blocked by suppressed deliver_sm: %v", err)
	}
}

func TestInboundDeliverResponseDeferredExactlyOnce(t *testing.T) {
	headers := make(chan codec.Header, 1)
	client, server := boundDeliverPair(t, HandlerFunc(func(_ context.Context, _ *Session, pdu InboundPDU) (Response, error) {
		if pdu.Header.CommandID == protocol.CommandDeliverSM {
			headers <- pdu.Header // header is a value, not a borrowed body
			return Response{Action: ResponseDeferred}, nil
		}
		return Response{Status: protocol.StatusOK, Body: protocol.EmptyBody{}}, nil
	}), 2)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := server.DeliverSM(ctx, protocol.DeliverSM{DestinationAddr: []byte("1000")})
		result <- err
	}()
	var header codec.Header
	select {
	case header = <-headers:
	case <-ctx.Done():
		t.Fatal("no deliver_sm arrived")
	}
	for client.PendingDeferredResponses() != 1 {
		select {
		case <-ctx.Done():
			t.Fatal("deferred response slot was not registered")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if err := client.RespondDeferred(header, Response{Status: protocol.StatusOK, Body: protocol.DeliverSMResp{}}); err != nil {
		t.Fatalf("send delayed response: %v", err)
	}
	if err := <-result; err != nil {
		t.Fatalf("server did not receive deferred ACK: %v", err)
	}
	if got := client.PendingDeferredResponses(); got != 0 {
		t.Fatalf("pending replies after ACK: %d", got)
	}
	if err := client.RespondDeferred(header, Response{Status: protocol.StatusOK, Body: protocol.DeliverSMResp{}}); !errors.Is(err, ErrDeferredResponseUnknown) {
		t.Fatalf("duplicate response error=%v", err)
	}
}

func TestInboundDeferredCapacityAndAbandon(t *testing.T) {
	conn, peer := net.Pipe()
	defer peer.Close()
	s, err := New(conn, Config{Role: RoleESME, MaxDeferredResponses: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	one := codec.Header{CommandID: protocol.CommandDeliverSM, SequenceNumber: 123}
	two := codec.Header{CommandID: protocol.CommandDeliverSM, SequenceNumber: 124}
	if err := s.registerDeferredReply(one); err != nil {
		t.Fatal(err)
	}
	if err := s.registerDeferredReply(two); !errors.Is(err, ErrDeferredResponseLimit) {
		t.Fatalf("second deferred slot error=%v", err)
	}
	if !s.AbandonDeferredResponse(one) {
		t.Fatal("failed to abandon reserved response")
	}
	if s.AbandonDeferredResponse(one) {
		t.Fatal("abandoned the same response twice")
	}
	if err := s.registerDeferredReply(two); err != nil {
		t.Fatalf("capacity was not released: %v", err)
	}
	if got := s.PendingDeferredResponses(); got != 1 {
		t.Fatalf("pending after abandon/re-register: %d", got)
	}
	if err := s.RespondDeferred(codec.Header{CommandID: protocol.CommandSubmitSM, SequenceNumber: 124}, Response{}); !errors.Is(err, ErrInvalidResponseAction) {
		t.Fatalf("wrong request type error=%v", err)
	}
}
