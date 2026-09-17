package session

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/transport"
)

func TestSessionBidirectionalTRX(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	serverHandler := HandlerFunc(func(_ context.Context, _ *Session, pdu codec.DecodedPDU) (Response, error) {
		switch pdu.Header.CommandID {
		case protocol.CommandBindTransceiver:
			return Response{Status: protocol.StatusOK, Body: protocol.BindResponse{SystemID: []byte("smsc")}}, nil
		case protocol.CommandSubmitSM:
			body := pdu.Body.(protocol.SubmitSM)
			return Response{Status: protocol.StatusOK, Body: protocol.SubmitSMResp{MessageID: append([]byte("id-"), body.DestinationAddr...)}}, nil
		default:
			return Response{Status: protocol.StatusSystemError, Body: protocol.EmptyBody{}}, nil
		}
	})
	clientHandler := HandlerFunc(func(_ context.Context, _ *Session, pdu codec.DecodedPDU) (Response, error) {
		if pdu.Header.CommandID != protocol.CommandDeliverSM {
			return Response{Status: protocol.StatusSystemError, Body: protocol.EmptyBody{}}, nil
		}
		return Response{Status: protocol.StatusOK, Body: protocol.DeliverSMResp{}}, nil
	})
	serverSession, err := New(serverConn, Config{Role: RoleSMSC, Handler: serverHandler})
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	clientSession, err := New(clientConn, Config{Role: RoleESME, Handler: clientHandler})
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := clientSession.BindTransceiver(ctx, protocol.BindRequest{SystemID: []byte("esme"), InterfaceVersion: protocol.InterfaceVersion34}); err != nil {
		t.Fatalf("bind: %v", err)
	}
	waitState(t, serverSession, protocol.StateBoundTRX)
	if clientSession.State() != protocol.StateBoundTRX {
		t.Fatalf("client state after bind: %s", clientSession.State())
	}

	var wg sync.WaitGroup
	wg.Add(2)
	var submitErr, deliverErr error
	go func() {
		defer wg.Done()
		resp, err := clientSession.SubmitSM(ctx, protocol.SubmitSM{DestinationAddr: []byte("12345")})
		submitErr = err
		if err == nil && string(resp.MessageID) != "id-12345" {
			submitErr = errors.New("unexpected submit message id")
		}
	}()
	go func() {
		defer wg.Done()
		_, deliverErr = serverSession.DeliverSM(ctx, protocol.DeliverSM{DestinationAddr: []byte("esme")})
	}()
	wg.Wait()
	if submitErr != nil {
		t.Fatalf("submit: %v", submitErr)
	}
	if deliverErr != nil {
		t.Fatalf("deliver: %v", deliverErr)
	}
	if err := clientSession.EnquireLink(ctx); err != nil {
		t.Fatalf("enquire_link: %v", err)
	}
	if err := clientSession.Unbind(ctx); err != nil {
		t.Fatalf("unbind: %v", err)
	}
	select {
	case <-clientSession.Done():
	case <-time.After(time.Second):
		t.Fatal("client did not close after unbind")
	}
}

func TestSessionOutOfOrderCorrelation(t *testing.T) {
	clientConn, peerConn := net.Pipe()
	registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	peerDone := make(chan error, 1)
	go func() {
		defer peerConn.Close()
		bind, err := readOnePDU(peerConn, registry)
		if err != nil {
			peerDone <- err
			return
		}
		frame, err := codec.EncodePDU(nil, codec.ResponseHeader(bind.Header, protocol.CommandBindTransceiverResp, protocol.StatusOK), protocol.BindResponse{SystemID: []byte("smsc")}, registry)
		if err != nil {
			peerDone <- err
			return
		}
		if err := transport.WriteFull(peerConn, frame); err != nil {
			peerDone <- err
			return
		}
		first, err := readOnePDU(peerConn, registry)
		if err != nil {
			peerDone <- err
			return
		}
		second, err := readOnePDU(peerConn, registry)
		if err != nil {
			peerDone <- err
			return
		}
		for _, request := range []codec.DecodedPDU{second, first} {
			body := request.Body.(protocol.SubmitSM)
			resp := protocol.SubmitSMResp{MessageID: append([]byte(nil), body.DestinationAddr...)}
			frame, err := codec.EncodePDU(nil, codec.ResponseHeader(request.Header, protocol.CommandSubmitSMResp, protocol.StatusOK), resp, registry)
			if err != nil {
				peerDone <- err
				return
			}
			if err := transport.WriteFull(peerConn, frame); err != nil {
				peerDone <- err
				return
			}
		}
		peerDone <- nil
	}()

	clientSession, err := New(clientConn, Config{Role: RoleESME})
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := clientSession.BindTransceiver(ctx, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion34}); err != nil {
		t.Fatal(err)
	}

	type result struct {
		want string
		got  protocol.SubmitSMResp
		err  error
	}
	results := make(chan result, 2)
	for _, destination := range []string{"111", "222"} {
		destination := destination
		go func() {
			resp, err := clientSession.SubmitSM(ctx, protocol.SubmitSM{DestinationAddr: []byte(destination)})
			results <- result{want: destination, got: resp, err: err}
		}()
	}
	for i := 0; i < 2; i++ {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		if string(result.got.MessageID) != result.want {
			t.Fatalf("correlation mismatch: want %q got %q", result.want, result.got.MessageID)
		}
	}
	if err := <-peerDone; err != nil {
		t.Fatal(err)
	}
}

func TestSessionAcceptsHighInboundSequenceAndEchoesIt(t *testing.T) {
	sessionConn, peerConn := net.Pipe()
	registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	handler := HandlerFunc(func(_ context.Context, _ *Session, pdu codec.DecodedPDU) (Response, error) {
		if pdu.Header.CommandID == protocol.CommandBindTransceiver {
			return Response{Status: protocol.StatusOK, Body: protocol.BindResponse{SystemID: []byte("smsc")}}, nil
		}
		if pdu.Header.CommandID == protocol.CommandSubmitSM {
			return Response{Status: protocol.StatusOK, Body: protocol.SubmitSMResp{MessageID: []byte("x")}}, nil
		}
		return Response{Status: protocol.StatusSystemError, Body: protocol.EmptyBody{}}, nil
	})
	sess, err := New(sessionConn, Config{Role: RoleSMSC, Handler: handler})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	bindHeader := codec.Header{CommandID: protocol.CommandBindTransceiver, SequenceNumber: protocol.SequenceInboundMax}
	frame, err := codec.EncodePDU(nil, bindHeader, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion34}, registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := transport.WriteFull(peerConn, frame); err != nil {
		t.Fatal(err)
	}
	resp, err := readOnePDU(peerConn, registry)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Header.SequenceNumber != protocol.SequenceInboundMax {
		t.Fatalf("response sequence=0x%08x", uint32(resp.Header.SequenceNumber))
	}
}

func TestSessionHandlesFragmentedTCPReads(t *testing.T) {
	sessionConn, peerConn := net.Pipe()
	registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	handler := HandlerFunc(func(_ context.Context, _ *Session, pdu codec.DecodedPDU) (Response, error) {
		if pdu.Header.CommandID != protocol.CommandBindTransceiver {
			return Response{Status: protocol.StatusSystemError, Body: protocol.EmptyBody{}}, nil
		}
		return Response{Status: protocol.StatusOK, Body: protocol.BindResponse{SystemID: []byte("smsc")}}, nil
	})
	sess, err := New(sessionConn, Config{Role: RoleSMSC, Handler: handler, ReadBufferSize: 5})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	frame, err := codec.EncodePDU(nil, codec.Header{CommandID: protocol.CommandBindTransceiver, SequenceNumber: 9}, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion34}, registry)
	if err != nil {
		t.Fatal(err)
	}
	writeDone := make(chan error, 1)
	go func() {
		for off := 0; off < len(frame); {
			end := off + 3
			if end > len(frame) {
				end = len(frame)
			}
			if _, err := peerConn.Write(frame[off:end]); err != nil {
				writeDone <- err
				return
			}
			off = end
		}
		writeDone <- nil
	}()
	resp, err := readOnePDU(peerConn, registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-writeDone; err != nil {
		t.Fatal(err)
	}
	if resp.Header.CommandID != protocol.CommandBindTransceiverResp || resp.Header.SequenceNumber != 9 {
		t.Fatalf("unexpected response: command=0x%08x sequence=%d", uint32(resp.Header.CommandID), resp.Header.SequenceNumber)
	}
	waitState(t, sess, protocol.StateBoundTRX)
}

func TestFatalFramingErrorLogsAndClosesConnection(t *testing.T) {
	sessionConn, peerConn := net.Pipe()
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	sess, err := New(sessionConn, Config{Role: RoleSMSC, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	bad := make([]byte, 16)
	binary.BigEndian.PutUint32(bad[:4], 7)
	_ = peerConn.SetWriteDeadline(time.Now().Add(time.Second))
	_, _ = peerConn.Write(bad)
	select {
	case <-sess.Done():
	case <-time.After(time.Second):
		t.Fatal("session remained open after fatal frame")
	}
	var fatal *protocol.FatalError
	if !errors.As(sess.Err(), &fatal) {
		t.Fatalf("terminal error=%T %v", sess.Err(), sess.Err())
	}
	if !strings.Contains(logs.String(), "smpp_protocol_fatal") || !strings.Contains(logs.String(), "connection_closed") {
		t.Fatalf("fatal diagnostic missing: %s", logs.String())
	}
	metrics := sess.Metrics()
	if metrics.FatalProtocolErrors != 1 || metrics.DecodeFailures != 1 {
		t.Fatalf("fatal metrics=%+v", metrics)
	}
	_ = peerConn.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
	if _, err := peerConn.Write([]byte{0, 0, 0, 16}); err == nil {
		t.Fatal("peer write unexpectedly succeeded after fatal close")
	}
}

func waitState(t *testing.T, sess *Session, want protocol.SessionState) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if sess.State() == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("state=%s want %s", sess.State(), want)
}

func readOnePDU(conn net.Conn, registry *codec.Registry) (codec.DecodedPDU, error) {
	prefix := make([]byte, 4)
	if _, err := io.ReadFull(conn, prefix); err != nil {
		return codec.DecodedPDU{}, err
	}
	length := binary.BigEndian.Uint32(prefix)
	if length < codec.HeaderSize {
		return codec.DecodedPDU{}, errors.New("invalid test frame")
	}
	frame := make([]byte, int(length))
	copy(frame[:4], prefix)
	if _, err := io.ReadFull(conn, frame[4:]); err != nil {
		return codec.DecodedPDU{}, err
	}
	return codec.DecodePDU(frame, registry)
}
