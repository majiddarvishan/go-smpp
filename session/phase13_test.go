package session

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/transport"
)

func TestOutbindIsOneWayAndEnablesBindReceiver(t *testing.T) {
	sessionConn, peerConn := net.Pipe()
	registry := mustRegistryForPhase13(t)
	outbindSeen := make(chan protocol.Outbind, 1)
	handler := HandlerFunc(func(_ context.Context, _ *Session, pdu codec.DecodedPDU) (Response, error) {
		if pdu.Header.CommandID == protocol.CommandOutbind {
			outbindSeen <- pdu.Body.(protocol.Outbind)
		}
		return Response{Status: protocol.StatusOK}, nil
	})
	sess, err := New(sessionConn, Config{Role: RoleESME, Handler: handler, SessionInitTimeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	defer peerConn.Close()

	outbind, err := codec.EncodePDU(nil, codec.Header{CommandID: protocol.CommandOutbind, SequenceNumber: 0xf0000001}, protocol.Outbind{SystemID: []byte("smsc"), Password: []byte("pw")}, registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := transport.WriteFull(peerConn, outbind); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-outbindSeen:
		if string(got.SystemID) != "smsc" {
			t.Fatalf("outbind=%+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("outbind handler not called")
	}
	waitState(t, sess, protocol.StateOutbound)

	if err := peerConn.SetReadDeadline(time.Now().Add(40 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 16)
	_, err = peerConn.Read(buf)
	if err == nil {
		t.Fatal("outbind unexpectedly produced a response PDU")
	}
	var ne net.Error
	if !errors.As(err, &ne) || !ne.Timeout() {
		t.Fatalf("want read timeout proving no response, got %v", err)
	}
	_ = peerConn.SetReadDeadline(time.Time{})

	peerDone := make(chan error, 1)
	go func() {
		bind, err := readOnePDU(peerConn, registry)
		if err != nil {
			peerDone <- err
			return
		}
		if bind.Header.CommandID != protocol.CommandBindReceiver {
			peerDone <- errors.New("expected bind_receiver after outbind")
			return
		}
		frame, err := codec.EncodePDU(nil, codec.ResponseHeader(bind.Header, protocol.CommandBindReceiverResp, protocol.StatusOK), protocol.BindResponse{SystemID: []byte("smsc")}, registry)
		if err == nil {
			err = transport.WriteFull(peerConn, frame)
		}
		peerDone <- err
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := sess.BindReceiver(ctx, protocol.BindRequest{SystemID: []byte("esme"), InterfaceVersion: protocol.InterfaceVersion34}); err != nil {
		t.Fatal(err)
	}
	if err := <-peerDone; err != nil {
		t.Fatal(err)
	}
	if sess.State() != protocol.StateBoundRX {
		t.Fatalf("state=%s want Bound_RX", sess.State())
	}
}

func TestAlertNotificationOneWayDoesNotCreatePendingRequest(t *testing.T) {
	esmeConn, smscConn := net.Pipe()
	alertSeen := make(chan protocol.AlertNotification, 1)
	esmeHandler := HandlerFunc(func(_ context.Context, _ *Session, pdu codec.DecodedPDU) (Response, error) {
		if pdu.Header.CommandID == protocol.CommandAlertNotification {
			alertSeen <- pdu.Body.(protocol.AlertNotification)
		}
		return Response{Status: protocol.StatusOK}, nil
	})
	smscHandler := HandlerFunc(func(_ context.Context, _ *Session, pdu codec.DecodedPDU) (Response, error) {
		if pdu.Header.CommandID == protocol.CommandBindReceiver {
			return Response{Status: protocol.StatusOK, Body: protocol.BindResponse{SystemID: []byte("smsc")}}, nil
		}
		return Response{Status: protocol.StatusSystemError, Body: protocol.EmptyBody{}}, nil
	})
	esme, err := New(esmeConn, Config{Role: RoleESME, Handler: esmeHandler})
	if err != nil {
		t.Fatal(err)
	}
	defer esme.Close()
	smsc, err := New(smscConn, Config{Role: RoleSMSC, Handler: smscHandler})
	if err != nil {
		t.Fatal(err)
	}
	defer smsc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := esme.BindReceiver(ctx, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion34}); err != nil {
		t.Fatal(err)
	}
	waitState(t, smsc, protocol.StateBoundRX)
	if err := smsc.AlertNotification(ctx, protocol.AlertNotification{SourceAddr: []byte("123"), ESMEAddr: []byte("456")}); err != nil {
		t.Fatal(err)
	}
	if smsc.Pending() != 0 {
		t.Fatalf("one-way alert created %d pending requests", smsc.Pending())
	}
	select {
	case got := <-alertSeen:
		if string(got.SourceAddr) != "123" {
			t.Fatalf("alert=%+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("alert not delivered")
	}
}

func TestRequestRejectsOneWayCommands(t *testing.T) {
	local, peer := net.Pipe()
	defer peer.Close()
	sess, err := New(local, Config{Role: RoleSMSC, SessionInitTimeout: -1, EnquireLinkInterval: -1, InactivityTimeout: -1})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := sess.Request(ctx, protocol.CommandOutbind, protocol.Outbind{}); !errors.Is(err, ErrUnexpectedPDU) {
		t.Fatalf("outbind request err=%v", err)
	}
	if _, err := sess.Request(ctx, protocol.CommandAlertNotification, protocol.AlertNotification{}); !errors.Is(err, ErrUnexpectedPDU) {
		t.Fatalf("alert request err=%v", err)
	}
}

func mustRegistryForPhase13(t *testing.T) *codec.Registry {
	t.Helper()
	registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func TestSessionSMPP34ExtendedRequestMethods(t *testing.T) {
	esmeConn, smscConn := net.Pipe()
	smscHandler := HandlerFunc(func(_ context.Context, _ *Session, pdu codec.DecodedPDU) (Response, error) {
		switch pdu.Header.CommandID {
		case protocol.CommandBindTransmitter:
			return Response{Status: protocol.StatusOK, Body: protocol.BindResponse{SystemID: []byte("smsc")}}, nil
		case protocol.CommandDataSM:
			return Response{Status: protocol.StatusOK, Body: protocol.DataSMResp{MessageID: []byte("d1"), Optional: []protocol.OptionalParameter{{Tag: protocol.TLVTagDPFResult, Value: []byte{1}}}}}, nil
		case protocol.CommandSubmitMulti:
			return Response{Status: protocol.StatusOK, Body: protocol.SubmitMultiResp{MessageID: []byte("m1"), Unsuccessful: []protocol.UnsuccessfulSME{{DestinationAddr: []byte("999"), ErrorStatusCode: protocol.StatusInvalidDestinationAddress}}}}, nil
		case protocol.CommandQuerySM:
			return Response{Status: protocol.StatusOK, Body: protocol.QuerySMResp{MessageID: []byte("m1"), MessageState: protocol.MessageStateDelivered}}, nil
		case protocol.CommandCancelSM, protocol.CommandReplaceSM:
			return Response{Status: protocol.StatusOK, Body: protocol.EmptyBody{}}, nil
		default:
			return Response{Status: protocol.StatusSystemError, Body: protocol.EmptyBody{}}, nil
		}
	})
	smsc, err := New(smscConn, Config{Role: RoleSMSC, Handler: smscHandler})
	if err != nil {
		t.Fatal(err)
	}
	defer smsc.Close()
	esme, err := New(esmeConn, Config{Role: RoleESME})
	if err != nil {
		t.Fatal(err)
	}
	defer esme.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := esme.BindTransmitter(ctx, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion34}); err != nil {
		t.Fatal(err)
	}
	if got, err := esme.DataSM(ctx, protocol.DataSM{DestinationAddr: []byte("1")}); err != nil || string(got.MessageID) != "d1" || len(got.Optional) != 1 {
		t.Fatalf("data_sm resp=%+v err=%v", got, err)
	}
	multi, err := esme.SubmitMulti(ctx, protocol.SubmitMulti{Destinations: []protocol.SubmitMultiDestination{{Flag: protocol.DestinationSME, Address: []byte("1")}}})
	if err != nil || string(multi.MessageID) != "m1" || len(multi.Unsuccessful) != 1 || string(multi.Unsuccessful[0].DestinationAddr) != "999" {
		t.Fatalf("submit_multi resp=%+v err=%v", multi, err)
	}
	query, err := esme.QuerySM(ctx, protocol.QuerySM{MessageID: []byte("m1")})
	if err != nil || query.MessageState != protocol.MessageStateDelivered {
		t.Fatalf("query resp=%+v err=%v", query, err)
	}
	if err := esme.CancelSM(ctx, protocol.CancelSM{MessageID: []byte("m1")}); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if err := esme.ReplaceSM(ctx, protocol.ReplaceSM{MessageID: []byte("m1"), ShortMessage: []byte("new")}); err != nil {
		t.Fatalf("replace: %v", err)
	}
}
