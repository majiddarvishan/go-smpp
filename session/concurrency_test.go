package session

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/transport"
)

func TestSessionHighConcurrencyOutOfOrderCorrelation(t *testing.T) {
	const count = 128
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
		bindResp, err := codec.EncodePDU(nil, codec.ResponseHeader(bind.Header, protocol.CommandBindTransceiverResp, protocol.StatusOK), protocol.BindResponse{SystemID: []byte("smsc")}, registry)
		if err != nil {
			peerDone <- err
			return
		}
		if err := transport.WriteFull(peerConn, bindResp); err != nil {
			peerDone <- err
			return
		}

		requests := make([]codec.DecodedPDU, 0, count)
		for i := 0; i < count; i++ {
			pdu, err := readOnePDU(peerConn, registry)
			if err != nil {
				peerDone <- err
				return
			}
			if pdu.Header.CommandID != protocol.CommandSubmitSM {
				peerDone <- fmt.Errorf("unexpected command 0x%08x", uint32(pdu.Header.CommandID))
				return
			}
			requests = append(requests, pdu)
		}

		for i := len(requests) - 1; i >= 0; i-- {
			request := requests[i]
			body := request.Body.(protocol.SubmitSM)
			response, err := codec.EncodePDU(nil, codec.ResponseHeader(request.Header, protocol.CommandSubmitSMResp, protocol.StatusOK), protocol.SubmitSMResp{MessageID: append([]byte(nil), body.DestinationAddr...)}, registry)
			if err != nil {
				peerDone <- err
				return
			}
			if err := transport.WriteFull(peerConn, response); err != nil {
				peerDone <- err
				return
			}
		}
		peerDone <- nil
	}()

	sess, err := New(clientConn, Config{Role: RoleESME, MaxPending: count, TXQueueSize: count})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := sess.BindTransceiver(ctx, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion34}); err != nil {
		t.Fatal(err)
	}

	type result struct {
		want string
		got  string
		err  error
	}
	results := make(chan result, count)
	for i := 0; i < count; i++ {
		want := fmt.Sprintf("%03d", i)
		go func() {
			response, err := sess.SubmitSM(ctx, protocol.SubmitSM{DestinationAddr: []byte(want)})
			results <- result{want: want, got: string(response.MessageID), err: err}
		}()
	}
	for i := 0; i < count; i++ {
		result := <-results
		if result.err != nil {
			t.Fatalf("submit %q: %v", result.want, result.err)
		}
		if result.got != result.want {
			t.Fatalf("correlation mismatch: want=%q got=%q", result.want, result.got)
		}
	}
	if err := <-peerDone; err != nil {
		t.Fatal(err)
	}
	if sess.Pending() != 0 {
		t.Fatalf("pending=%d after all responses", sess.Pending())
	}
}

func TestInboundDeliverWhileSubmitIsOutstanding(t *testing.T) {
	clientConn, peerConn := net.Pipe()
	registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	deliverSeen := make(chan protocol.SequenceNumber, 1)
	clientHandler := HandlerFunc(func(_ context.Context, _ *Session, pdu codec.DecodedPDU) (Response, error) {
		if pdu.Header.CommandID != protocol.CommandDeliverSM {
			return Response{Status: protocol.StatusSystemError, Body: protocol.EmptyBody{}}, nil
		}
		deliverSeen <- pdu.Header.SequenceNumber
		return Response{Status: protocol.StatusOK, Body: protocol.DeliverSMResp{}}, nil
	})

	peerDone := make(chan error, 1)
	go func() {
		defer peerConn.Close()
		bind, err := readOnePDU(peerConn, registry)
		if err != nil {
			peerDone <- err
			return
		}
		bindResp, err := codec.EncodePDU(nil, codec.ResponseHeader(bind.Header, protocol.CommandBindTransceiverResp, protocol.StatusOK), protocol.BindResponse{SystemID: []byte("smsc")}, registry)
		if err != nil {
			peerDone <- err
			return
		}
		if err := transport.WriteFull(peerConn, bindResp); err != nil {
			peerDone <- err
			return
		}

		submit, err := readOnePDU(peerConn, registry)
		if err != nil {
			peerDone <- err
			return
		}
		if submit.Header.CommandID != protocol.CommandSubmitSM {
			peerDone <- fmt.Errorf("expected submit_sm, got 0x%08x", uint32(submit.Header.CommandID))
			return
		}

		const inboundSequence protocol.SequenceNumber = 0xFFFFFFFE
		deliver, err := codec.EncodePDU(nil, codec.Header{CommandID: protocol.CommandDeliverSM, SequenceNumber: inboundSequence}, protocol.DeliverSM{DestinationAddr: []byte("esme"), ShortMessage: []byte("delivery")}, registry)
		if err != nil {
			peerDone <- err
			return
		}
		if err := transport.WriteFull(peerConn, deliver); err != nil {
			peerDone <- err
			return
		}
		deliverResp, err := readOnePDU(peerConn, registry)
		if err != nil {
			peerDone <- err
			return
		}
		if deliverResp.Header.CommandID != protocol.CommandDeliverSMResp || deliverResp.Header.SequenceNumber != inboundSequence {
			peerDone <- fmt.Errorf("deliver response mismatch: command=0x%08x sequence=0x%08x", uint32(deliverResp.Header.CommandID), uint32(deliverResp.Header.SequenceNumber))
			return
		}

		submitResp, err := codec.EncodePDU(nil, codec.ResponseHeader(submit.Header, protocol.CommandSubmitSMResp, protocol.StatusOK), protocol.SubmitSMResp{MessageID: []byte("held-submit")}, registry)
		if err != nil {
			peerDone <- err
			return
		}
		if err := transport.WriteFull(peerConn, submitResp); err != nil {
			peerDone <- err
			return
		}
		peerDone <- nil
	}()

	sess, err := New(clientConn, Config{Role: RoleESME, Handler: clientHandler})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := sess.BindTransceiver(ctx, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion34}); err != nil {
		t.Fatal(err)
	}

	submitDone := make(chan error, 1)
	go func() {
		response, err := sess.SubmitSM(ctx, protocol.SubmitSM{DestinationAddr: []byte("123")})
		if err == nil && string(response.MessageID) != "held-submit" {
			err = fmt.Errorf("unexpected message id %q", response.MessageID)
		}
		submitDone <- err
	}()

	select {
	case sequence := <-deliverSeen:
		if sequence != 0xFFFFFFFE {
			t.Fatalf("deliver sequence=0x%08x", uint32(sequence))
		}
	case <-ctx.Done():
		t.Fatal("deliver_sm was not handled while submit_sm was outstanding")
	}
	if err := <-submitDone; err != nil {
		t.Fatal(err)
	}
	if err := <-peerDone; err != nil {
		t.Fatal(err)
	}
}

func TestInvalidStandardOperationRejectedByState(t *testing.T) {
	serverConn, peerConn := net.Pipe()
	registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	var handlerCalls atomic.Int32
	handler := HandlerFunc(func(_ context.Context, _ *Session, pdu codec.DecodedPDU) (Response, error) {
		handlerCalls.Add(1)
		if pdu.Header.CommandID == protocol.CommandBindReceiver {
			return Response{Status: protocol.StatusOK, Body: protocol.BindResponse{SystemID: []byte("smsc")}}, nil
		}
		return Response{Status: protocol.StatusOK, Body: protocol.SubmitSMResp{MessageID: []byte("should-not-run")}}, nil
	})
	sess, err := New(serverConn, Config{Role: RoleSMSC, Handler: handler})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	bind, err := codec.EncodePDU(nil, codec.Header{CommandID: protocol.CommandBindReceiver, SequenceNumber: 1}, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion34}, registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := transport.WriteFull(peerConn, bind); err != nil {
		t.Fatal(err)
	}
	if _, err := readOnePDU(peerConn, registry); err != nil {
		t.Fatal(err)
	}
	waitState(t, sess, protocol.StateBoundRX)

	submit, err := codec.EncodePDU(nil, codec.Header{CommandID: protocol.CommandSubmitSM, SequenceNumber: 2}, protocol.SubmitSM{DestinationAddr: []byte("123")}, registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := transport.WriteFull(peerConn, submit); err != nil {
		t.Fatal(err)
	}
	response, err := readOnePDU(peerConn, registry)
	if err != nil {
		t.Fatal(err)
	}
	if response.Header.CommandID != protocol.CommandSubmitSMResp || response.Header.CommandStatus != protocol.StatusInvalidBindState {
		t.Fatalf("unexpected invalid-state response: command=0x%08x status=0x%08x", uint32(response.Header.CommandID), uint32(response.Header.CommandStatus))
	}
	if handlerCalls.Load() != 1 {
		t.Fatalf("handler calls=%d; invalid submit_sm reached handler", handlerCalls.Load())
	}
}

func TestFatalReceiveClosesBlockedTransmitAndFailsPending(t *testing.T) {
	serverConn, peerConn := net.Pipe()
	registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	handler := HandlerFunc(func(_ context.Context, _ *Session, pdu codec.DecodedPDU) (Response, error) {
		if pdu.Header.CommandID == protocol.CommandBindTransceiver {
			return Response{Status: protocol.StatusOK, Body: protocol.BindResponse{SystemID: []byte("smsc")}}, nil
		}
		return Response{Status: protocol.StatusSystemError, Body: protocol.EmptyBody{}}, nil
	})
	sess, err := New(serverConn, Config{Role: RoleSMSC, Handler: handler})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	bind, err := codec.EncodePDU(nil, codec.Header{CommandID: protocol.CommandBindTransceiver, SequenceNumber: 1}, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion34}, registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := transport.WriteFull(peerConn, bind); err != nil {
		t.Fatal(err)
	}
	if _, err := readOnePDU(peerConn, registry); err != nil {
		t.Fatal(err)
	}
	waitState(t, sess, protocol.StateBoundTRX)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	deliverDone := make(chan error, 1)
	go func() {
		_, err := sess.DeliverSM(ctx, protocol.DeliverSM{DestinationAddr: []byte("esme"), ShortMessage: []byte("will-block")})
		deliverDone <- err
	}()
	deadline := time.Now().Add(time.Second)
	for sess.Pending() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if sess.Pending() != 1 {
		t.Fatalf("pending=%d want 1", sess.Pending())
	}

	bad := make([]byte, codec.HeaderSize)
	bad[3] = 7 // command_length=7, structurally fatal
	if _, err := peerConn.Write(bad); err != nil {
		t.Fatalf("write malformed frame: %v", err)
	}
	select {
	case err := <-deliverDone:
		var loss *LossError
		if !errors.As(err, &loss) {
			t.Fatalf("deliver error=%T %v, want LossError", err, err)
		}
	case <-ctx.Done():
		t.Fatal("blocked transmit was not interrupted by fatal receive close")
	}
	select {
	case <-sess.Done():
	case <-time.After(time.Second):
		t.Fatal("session did not close after fatal receive")
	}
}
