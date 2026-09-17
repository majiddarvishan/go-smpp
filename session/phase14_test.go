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

func TestSMPP50NegotiationBroadcastAndCongestionFeedback(t *testing.T) {
	esmeConn, smscConn := net.Pipe()
	congestion := make(chan CongestionEvent, 1)

	smscHandler := HandlerFunc(func(_ context.Context, _ *Session, pdu codec.DecodedPDU) (Response, error) {
		switch pdu.Header.CommandID {
		case protocol.CommandBindTransmitter:
			// The session core should add sc_interface_version from its local
			// SMPP 5.0 profile when the handler does not provide one.
			return Response{Status: protocol.StatusOK, Body: protocol.BindResponse{SystemID: []byte("smsc")}}, nil
		case protocol.CommandBroadcastSM:
			request := pdu.Body.(protocol.BroadcastSM)
			if len(request.Optional) == 0 || request.Optional[0].Tag != protocol.TLVTagBroadcastAreaIdentifier {
				t.Errorf("broadcast request TLVs=%+v", request.Optional)
			}
			return Response{Status: protocol.StatusOK, Body: protocol.BroadcastSMResp{
				MessageID: []byte("broadcast-1"),
				Optional:  []protocol.OptionalParameter{{Tag: protocol.TLVTagCongestionState, Value: []byte{87}}},
			}}, nil
		case protocol.CommandQueryBroadcastSM:
			return Response{Status: protocol.StatusOK, Body: protocol.QueryBroadcastSMResp{MessageID: []byte("broadcast-1"), Optional: []protocol.OptionalParameter{{Tag: protocol.TLVTagMessageState, Value: []byte{byte(protocol.MessageStateDelivered)}}}}}, nil
		case protocol.CommandCancelBroadcastSM:
			return Response{Status: protocol.StatusOK, Body: protocol.EmptyBody{}}, nil
		default:
			return Response{Status: protocol.StatusSystemError, Body: protocol.EmptyBody{}}, nil
		}
	})

	smsc, err := New(smscConn, Config{Role: RoleSMSC, Profile: protocol.SMPP50Profile(), Handler: smscHandler})
	if err != nil {
		t.Fatal(err)
	}
	defer smsc.Close()
	esme, err := New(esmeConn, Config{
		Role: RoleESME, Profile: protocol.SMPP50Profile(),
		FlowController: FlowControllerFunc(func(event CongestionEvent) { congestion <- event }),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer esme.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := esme.BindTransmitter(ctx, protocol.BindRequest{SystemID: []byte("esme"), InterfaceVersion: protocol.InterfaceVersion50}); err != nil {
		t.Fatal(err)
	}
	if caps := esme.PeerCapabilities(); !caps.SupportsSMPP50 || !caps.SupportsTLV || caps.NegotiatedVersion != protocol.InterfaceVersion50 {
		t.Fatalf("ESME caps=%+v", caps)
	}
	if caps := smsc.PeerCapabilities(); !caps.SupportsSMPP50 || caps.PeerVersion != protocol.InterfaceVersion50 {
		t.Fatalf("SMSC caps=%+v", caps)
	}

	resp, err := esme.BroadcastSM(ctx, protocol.BroadcastSM{
		SourceAddrTON: 1, SourceAddrNPI: 1, SourceAddr: []byte("123"),
		Optional: []protocol.OptionalParameter{
			{Tag: protocol.TLVTagBroadcastAreaIdentifier, Value: []byte{0, 'A'}},
			{Tag: protocol.TLVTagBroadcastContentType, Value: []byte{0, 0, 1}},
		},
	})
	if err != nil || string(resp.MessageID) != "broadcast-1" {
		t.Fatalf("broadcast resp=%+v err=%v", resp, err)
	}
	select {
	case event := <-congestion:
		if event.State != 87 || event.Command != protocol.CommandBroadcastSMResp {
			t.Fatalf("congestion event=%+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("congestion feedback not delivered")
	}
	metrics := esme.Metrics()
	if metrics.CongestionSamples != 1 || metrics.LastCongestionState != 87 {
		t.Fatalf("congestion metrics=%+v", metrics)
	}

	query, err := esme.QueryBroadcastSM(ctx, protocol.QueryBroadcastSM{MessageID: []byte("broadcast-1"), SourceAddr: []byte("123")})
	if err != nil || string(query.MessageID) != "broadcast-1" {
		t.Fatalf("query=%+v err=%v", query, err)
	}
	if err := esme.CancelBroadcastSM(ctx, protocol.CancelBroadcastSM{MessageID: []byte("broadcast-1"), SourceAddr: []byte("123")}); err != nil {
		t.Fatal(err)
	}
}

func TestSMPP50DowngradeBlocksV5OnlyCommands(t *testing.T) {
	esmeConn, smscConn := net.Pipe()
	smscHandler := HandlerFunc(func(_ context.Context, _ *Session, pdu codec.DecodedPDU) (Response, error) {
		if pdu.Header.CommandID == protocol.CommandBindTransmitter {
			return Response{Status: protocol.StatusOK, Body: protocol.BindResponse{SystemID: []byte("smsc"), Optional: []protocol.OptionalParameter{{Tag: protocol.TLVTagSCInterfaceVersion, Value: []byte{byte(protocol.InterfaceVersion34)}}}}}, nil
		}
		return Response{Status: protocol.StatusSystemError, Body: protocol.EmptyBody{}}, nil
	})
	smsc, err := New(smscConn, Config{Role: RoleSMSC, Profile: protocol.SMPP50Profile(), Handler: smscHandler})
	if err != nil {
		t.Fatal(err)
	}
	defer smsc.Close()
	esme, err := New(esmeConn, Config{Role: RoleESME, Profile: protocol.SMPP50Profile()})
	if err != nil {
		t.Fatal(err)
	}
	defer esme.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := esme.BindTransmitter(ctx, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion50}); err != nil {
		t.Fatal(err)
	}
	caps := esme.PeerCapabilities()
	if caps.SupportsSMPP50 || !caps.SupportsTLV || caps.NegotiatedVersion != protocol.InterfaceVersion34 {
		t.Fatalf("caps=%+v", caps)
	}
	if _, err := esme.BroadcastSM(ctx, protocol.BroadcastSM{}); !errors.Is(err, ErrUnsupportedCapability) {
		t.Fatalf("broadcast downgrade err=%v", err)
	}
}

func TestSMPP50MissingSCInterfaceVersionDisablesTLVAssumption(t *testing.T) {
	esmeConn, peerConn := net.Pipe()
	registry, err := codec.NewSMPP50Registry(codec.RegistryCompatible)
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
		frame, err := codec.EncodePDU(nil,
			codec.ResponseHeader(bind.Header, protocol.CommandBindTransmitterResp, protocol.StatusOK),
			protocol.BindResponse{SystemID: []byte("raw-peer")}, registry)
		if err != nil {
			peerDone <- err
			return
		}
		err = transport.WriteFull(peerConn, frame)
		peerDone <- err
	}()

	esme, err := New(esmeConn, Config{Role: RoleESME, Profile: protocol.SMPP50Profile()})
	if err != nil {
		t.Fatal(err)
	}
	defer esme.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := esme.BindTransmitter(ctx, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion50}); err != nil {
		t.Fatal(err)
	}
	if err := <-peerDone; err != nil {
		t.Fatal(err)
	}
	caps := esme.PeerCapabilities()
	if caps.VersionAdvertised || caps.SupportsTLV || caps.SupportsSMPP50 || caps.NegotiatedVersion != 0 {
		t.Fatalf("caps=%+v", caps)
	}
}

func TestSMPP50BindAdvertisementCannotExceedLocalProfile(t *testing.T) {
	local, peer := net.Pipe()
	defer peer.Close()
	sess, err := New(local, Config{Role: RoleESME, Profile: protocol.SMPP34Profile()})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = sess.BindTransmitter(ctx, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion50})
	if !errors.Is(err, ErrUnsupportedCapability) {
		t.Fatalf("bind with v5 advertisement on v3.4 profile err=%v", err)
	}
}

func TestSMPP50LocalBindAdvertisementCapsNegotiation(t *testing.T) {
	esmeConn, peerConn := net.Pipe()
	registry, err := codec.NewSMPP50Registry(codec.RegistryCompatible)
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
		frame, err := codec.EncodePDU(nil,
			codec.ResponseHeader(bind.Header, protocol.CommandBindTransmitterResp, protocol.StatusOK),
			protocol.BindResponse{SystemID: []byte("smsc"), Optional: []protocol.OptionalParameter{{Tag: protocol.TLVTagSCInterfaceVersion, Value: []byte{byte(protocol.InterfaceVersion50)}}}},
			registry)
		if err != nil {
			peerDone <- err
			return
		}
		err = transport.WriteFull(peerConn, frame)
		peerDone <- err
	}()

	esme, err := New(esmeConn, Config{Role: RoleESME, Profile: protocol.SMPP50Profile()})
	if err != nil {
		t.Fatal(err)
	}
	defer esme.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := esme.BindTransmitter(ctx, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion34}); err != nil {
		t.Fatal(err)
	}
	if err := <-peerDone; err != nil {
		t.Fatal(err)
	}
	caps := esme.PeerCapabilities()
	if caps.NegotiatedVersion != protocol.InterfaceVersion34 || !caps.SupportsTLV || caps.SupportsSMPP50 {
		t.Fatalf("caps=%+v", caps)
	}
}

func TestSMPP50BroadcastStateMatrix(t *testing.T) {
	for _, command := range []protocol.CommandID{protocol.CommandBroadcastSM, protocol.CommandQueryBroadcastSM, protocol.CommandCancelBroadcastSM} {
		if !CanIssue(protocol.StateBoundTX, RoleESME, command) || !CanIssue(protocol.StateBoundTRX, RoleESME, command) {
			t.Fatalf("ESME should issue 0x%08x in TX/TRX", uint32(command))
		}
		if CanIssue(protocol.StateBoundRX, RoleESME, command) || CanIssue(protocol.StateOpen, RoleESME, command) {
			t.Fatalf("ESME must not issue 0x%08x in RX/Open", uint32(command))
		}
	}
}

func TestSMPP50CongestionFeedbackSurvivesErrorResponse(t *testing.T) {
	esmeConn, smscConn := net.Pipe()
	congestion := make(chan CongestionEvent, 1)
	smsc, err := New(smscConn, Config{
		Role: RoleSMSC, Profile: protocol.SMPP50Profile(),
		Handler: HandlerFunc(func(_ context.Context, _ *Session, pdu codec.DecodedPDU) (Response, error) {
			switch pdu.Header.CommandID {
			case protocol.CommandBindTransmitter:
				return Response{Status: protocol.StatusOK, Body: protocol.BindResponse{SystemID: []byte("smsc")}}, nil
			case protocol.CommandSubmitSM:
				return Response{Status: protocol.StatusThrottled, Body: protocol.OptionalResponse{Optional: []protocol.OptionalParameter{{Tag: protocol.TLVTagCongestionState, Value: []byte{96}}}}}, nil
			default:
				return Response{Status: protocol.StatusSystemError, Body: protocol.EmptyBody{}}, nil
			}
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer smsc.Close()
	esme, err := New(esmeConn, Config{
		Role: RoleESME, Profile: protocol.SMPP50Profile(),
		FlowController: FlowControllerFunc(func(event CongestionEvent) { congestion <- event }),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer esme.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := esme.BindTransmitter(ctx, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion50}); err != nil {
		t.Fatal(err)
	}
	if _, err := esme.SubmitSM(ctx, protocol.SubmitSM{DestinationAddr: []byte("123")}); err == nil {
		t.Fatal("expected throttled submit error")
	}
	select {
	case event := <-congestion:
		if event.State != 96 || event.Command != protocol.CommandSubmitSMResp {
			t.Fatalf("congestion event=%+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("congestion feedback not delivered from error response")
	}
}

func TestSMPP50ServerDoesNotSendTLVToLegacyBind(t *testing.T) {
	sessionConn, peerConn := net.Pipe()
	registry, err := codec.NewSMPP50Registry(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := New(sessionConn, Config{
		Role: RoleSMSC, Profile: protocol.SMPP50Profile(),
		Handler: HandlerFunc(func(_ context.Context, _ *Session, pdu codec.DecodedPDU) (Response, error) {
			if pdu.Header.CommandID == protocol.CommandBindTransmitter {
				return Response{Status: protocol.StatusOK, Body: protocol.BindResponse{SystemID: []byte("smsc")}}, nil
			}
			return Response{Status: protocol.StatusSystemError, Body: protocol.EmptyBody{}}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	defer peerConn.Close()

	frame, err := codec.EncodePDU(nil, codec.Header{CommandID: protocol.CommandBindTransmitter, SequenceNumber: 1}, protocol.BindRequest{InterfaceVersion: 0x33}, registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := transport.WriteFull(peerConn, frame); err != nil {
		t.Fatal(err)
	}
	response, err := readOnePDU(peerConn, registry)
	if err != nil {
		t.Fatal(err)
	}
	body, ok := response.Body.(protocol.BindResponse)
	if !ok {
		t.Fatalf("bind response body=%T", response.Body)
	}
	if len(body.Optional) != 0 {
		t.Fatalf("legacy bind received TLVs: %+v", body.Optional)
	}
}
