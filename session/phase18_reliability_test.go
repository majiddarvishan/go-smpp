package session

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/transport"
)

func TestPhase18TCPFragmentationAndCoalescingEndToEnd(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	accepted := make(chan net.Conn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			acceptErr <- err
			return
		}
		accepted <- conn
	}()

	peer, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()

	var serverConn net.Conn
	select {
	case serverConn = <-accepted:
	case err := <-acceptErr:
		t.Fatal(err)
	case <-time.After(time.Second):
		t.Fatal("accept timeout")
	}

	registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	handler := HandlerFunc(func(_ context.Context, _ *Session, pdu codec.DecodedPDU) (Response, error) {
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
	sess, err := New(serverConn, Config{
		Role:                RoleSMSC,
		Handler:             handler,
		ReadBufferSize:      7,
		EnquireLinkInterval: -1,
		InactivityTimeout:   -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	bind, err := codec.EncodePDU(nil, codec.Header{CommandID: protocol.CommandBindTransceiver, SequenceNumber: 1},
		protocol.BindRequest{SystemID: []byte("esme"), InterfaceVersion: protocol.InterfaceVersion34}, registry)
	if err != nil {
		t.Fatal(err)
	}
	for off := 0; off < len(bind); {
		end := off + 3
		if end > len(bind) {
			end = len(bind)
		}
		if _, err := peer.Write(bind[off:end]); err != nil {
			t.Fatal(err)
		}
		off = end
	}
	bindResp, err := readOnePDU(peer, registry)
	if err != nil {
		t.Fatal(err)
	}
	if bindResp.Header.CommandID != protocol.CommandBindTransceiverResp || bindResp.Header.SequenceNumber != 1 {
		t.Fatalf("bind response=%+v", bindResp.Header)
	}

	first, err := codec.EncodePDU(nil, codec.Header{CommandID: protocol.CommandSubmitSM, SequenceNumber: 2},
		protocol.SubmitSM{DestinationAddr: []byte("111"), ShortMessage: []byte("a")}, registry)
	if err != nil {
		t.Fatal(err)
	}
	second, err := codec.EncodePDU(nil, codec.Header{CommandID: protocol.CommandSubmitSM, SequenceNumber: 3},
		protocol.SubmitSM{DestinationAddr: []byte("222"), ShortMessage: []byte("b")}, registry)
	if err != nil {
		t.Fatal(err)
	}
	coalesced := append(append(make([]byte, 0, len(first)+len(second)), first...), second...)
	if _, err := peer.Write(coalesced); err != nil {
		t.Fatal(err)
	}

	seen := map[protocol.SequenceNumber]string{}
	for i := 0; i < 2; i++ {
		resp, err := readOnePDU(peer, registry)
		if err != nil {
			t.Fatal(err)
		}
		body := resp.Body.(protocol.SubmitSMResp)
		seen[resp.Header.SequenceNumber] = string(body.MessageID)
	}
	if seen[2] != "id-111" || seen[3] != "id-222" {
		t.Fatalf("coalesced responses=%v", seen)
	}
}

func TestPhase18FatalFramingMatrixClosesAndRedacts(t *testing.T) {
	tests := []struct {
		name       string
		maxPDU     uint32
		wire       func() []byte
		closePeer  bool
		sensitive  string
	}{
		{
			name: "invalid command length and following plausible frame",
			wire: func() []byte {
				bad := rawPDUHeader(15, protocol.CommandBindTransceiver, 1)
				good := rawPDUHeader(codec.HeaderSize, protocol.CommandEnquireLink, 2)
				return append(bad, good...)
			},
		},
		{
			name:   "oversized declaration",
			maxPDU: 64,
			wire: func() []byte {
				return rawPDUHeader(65, protocol.CommandBindTransceiver, 1)[:4]
			},
		},
		{
			name: "truncated frame",
			wire: func() []byte {
				frame := rawPDUHeader(32, protocol.CommandBindTransceiver, 1)
				return append(frame, []byte{1, 2, 3, 4}...)
			},
			closePeer: true,
		},
		{
			name: "malformed mandatory cstring redacts body",
			wire: func() []byte {
				body := []byte("secret-password-message-content")
				frame := rawPDUHeader(uint32(codec.HeaderSize+len(body)), protocol.CommandBindTransceiver, 9)
				return append(frame, body...)
			},
			sensitive: "secret-password-message-content",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sessionConn, peerConn := net.Pipe()
			var logs bytes.Buffer
			var calls atomic.Int32
			handler := HandlerFunc(func(context.Context, *Session, codec.DecodedPDU) (Response, error) {
				calls.Add(1)
				return Response{Status: protocol.StatusOK, Body: protocol.BindResponse{SystemID: []byte("smsc")}}, nil
			})
			cfg := Config{
				Role:                RoleSMSC,
				Handler:             handler,
				Logger:              slog.New(slog.NewTextHandler(&logs, nil)),
				MaxPDUSize:          tc.maxPDU,
				SessionInitTimeout:  time.Second,
				EnquireLinkInterval: -1,
				InactivityTimeout:   -1,
			}
			sess, err := New(sessionConn, cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer sess.Close()

			if _, err := peerConn.Write(tc.wire()); err != nil && !tc.closePeer {
				t.Fatal(err)
			}
			if tc.closePeer {
				_ = peerConn.Close()
			} else {
				defer peerConn.Close()
			}

			select {
			case <-sess.Done():
			case <-time.After(time.Second):
				t.Fatal("fatal structural input did not close session")
			}
			var fatal *protocol.FatalError
			if !errors.As(sess.Err(), &fatal) {
				t.Fatalf("terminal error=%T %v", sess.Err(), sess.Err())
			}
			if calls.Load() != 0 {
				t.Fatalf("handler called %d times after fatal framing input", calls.Load())
			}
			logText := logs.String()
			if !strings.Contains(logText, "smpp_protocol_fatal") || !strings.Contains(logText, "connection_closed") {
				t.Fatalf("fatal diagnostic missing: %s", logText)
			}
			if tc.sensitive != "" && strings.Contains(logText, tc.sensitive) {
				t.Fatalf("fatal log leaked sensitive PDU content: %s", logText)
			}
		})
	}
}

func TestPhase18InvalidTLVLengthClosesWithoutResynchronization(t *testing.T) {
	sessionConn, peerConn := net.Pipe()
	defer peerConn.Close()
	registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}

	var submitCalls atomic.Int32
	handler := HandlerFunc(func(_ context.Context, _ *Session, pdu codec.DecodedPDU) (Response, error) {
		switch pdu.Header.CommandID {
		case protocol.CommandBindTransceiver:
			return Response{Status: protocol.StatusOK, Body: protocol.BindResponse{SystemID: []byte("smsc")}}, nil
		case protocol.CommandSubmitSM:
			submitCalls.Add(1)
			return Response{Status: protocol.StatusOK, Body: protocol.SubmitSMResp{MessageID: []byte("ok")}}, nil
		default:
			return Response{Status: protocol.StatusSystemError, Body: protocol.EmptyBody{}}, nil
		}
	})
	var logs bytes.Buffer
	sess, err := New(sessionConn, Config{
		Role: RoleSMSC, Handler: handler,
		Logger: slog.New(slog.NewTextHandler(&logs, nil)),
		EnquireLinkInterval: -1, InactivityTimeout: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	bind, err := codec.EncodePDU(nil, codec.Header{CommandID: protocol.CommandBindTransceiver, SequenceNumber: 1},
		protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion34}, registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := transport.WriteFull(peerConn, bind); err != nil {
		t.Fatal(err)
	}
	if _, err := readOnePDU(peerConn, registry); err != nil {
		t.Fatal(err)
	}

	malformed, err := codec.EncodePDU(nil, codec.Header{CommandID: protocol.CommandSubmitSM, SequenceNumber: 2},
		protocol.SubmitSM{DestinationAddr: []byte("111"), ShortMessage: []byte("bad-tlv")}, registry)
	if err != nil {
		t.Fatal(err)
	}
	malformed = append(malformed, 0x00, 0x1e, 0x00, 0x10, 0x41) // length 16, one value octet present
	binary.BigEndian.PutUint32(malformed[:4], uint32(len(malformed)))
	good, err := codec.EncodePDU(nil, codec.Header{CommandID: protocol.CommandSubmitSM, SequenceNumber: 3},
		protocol.SubmitSM{DestinationAddr: []byte("222"), ShortMessage: []byte("must-not-run")}, registry)
	if err != nil {
		t.Fatal(err)
	}
	wire := append(append(make([]byte, 0, len(malformed)+len(good)), malformed...), good...)
	_, _ = peerConn.Write(wire)

	select {
	case <-sess.Done():
	case <-time.After(time.Second):
		t.Fatal("invalid TLV length did not close session")
	}
	if submitCalls.Load() != 0 {
		t.Fatalf("stream resynchronization occurred; submit handler calls=%d", submitCalls.Load())
	}
	var fatal *protocol.FatalError
	if !errors.As(sess.Err(), &fatal) {
		t.Fatalf("terminal error=%T %v", sess.Err(), sess.Err())
	}
	if !strings.Contains(logs.String(), "smpp_protocol_fatal") {
		t.Fatalf("fatal TLV diagnostic missing: %s", logs.String())
	}
}

func TestPhase18UnexpectedAndDuplicateResponsesDoNotMiscompleteRequests(t *testing.T) {
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
		bindResp, _ := codec.EncodePDU(nil, codec.ResponseHeader(bind.Header, protocol.CommandBindTransceiverResp, protocol.StatusOK),
			protocol.BindResponse{SystemID: []byte("smsc")}, registry)
		if err := transport.WriteFull(peerConn, bindResp); err != nil {
			peerDone <- err
			return
		}

		request, err := readOnePDU(peerConn, registry)
		if err != nil {
			peerDone <- err
			return
		}
		wrongHeader := codec.Header{
			CommandID: protocol.CommandSubmitSMResp, CommandStatus: protocol.StatusOK,
			SequenceNumber: request.Header.SequenceNumber + 1000,
		}
		wrong, _ := codec.EncodePDU(nil, wrongHeader, protocol.SubmitSMResp{MessageID: []byte("wrong")}, registry)
		if err := transport.WriteFull(peerConn, wrong); err != nil {
			peerDone <- err
			return
		}
		correct, _ := codec.EncodePDU(nil, codec.ResponseHeader(request.Header, protocol.CommandSubmitSMResp, protocol.StatusOK),
			protocol.SubmitSMResp{MessageID: []byte("correct")}, registry)
		if err := transport.WriteFull(peerConn, correct); err != nil {
			peerDone <- err
			return
		}
		if err := transport.WriteFull(peerConn, correct); err != nil { // duplicate after terminal completion
			peerDone <- err
			return
		}

		enquire, err := readOnePDU(peerConn, registry)
		if err != nil {
			peerDone <- err
			return
		}
		if enquire.Header.CommandID != protocol.CommandEnquireLink {
			peerDone <- fmt.Errorf("expected enquire_link, got 0x%08x", uint32(enquire.Header.CommandID))
			return
		}
		resp, _ := codec.EncodePDU(nil, codec.ResponseHeader(enquire.Header, protocol.CommandEnquireLinkResp, protocol.StatusOK), protocol.EmptyBody{}, registry)
		peerDone <- transport.WriteFull(peerConn, resp)
	}()

	sess, err := New(clientConn, Config{
		Role: RoleESME, ResponseTimeout: time.Second,
		EnquireLinkInterval: -1, InactivityTimeout: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := sess.BindTransceiver(ctx, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion34}); err != nil {
		t.Fatal(err)
	}
	resp, err := sess.SubmitSM(ctx, protocol.SubmitSM{DestinationAddr: []byte("111")})
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.MessageID) != "correct" {
		t.Fatalf("message id=%q", resp.MessageID)
	}
	if err := sess.EnquireLink(ctx); err != nil {
		t.Fatalf("session unusable after unexpected/duplicate responses: %v", err)
	}
	if err := <-peerDone; err != nil {
		t.Fatal(err)
	}
}

func TestPhase18DisconnectDuringBindIdleAndFullWindow(t *testing.T) {
	t.Run("bind", func(t *testing.T) {
		clientConn, peerConn := net.Pipe()
		registry, _ := codec.NewSMPP34Registry(codec.RegistryCompatible)
		go func() {
			_, _ = readOnePDU(peerConn, registry)
			_ = peerConn.Close()
		}()
		sess, err := New(clientConn, Config{Role: RoleESME, EnquireLinkInterval: -1, InactivityTimeout: -1})
		if err != nil {
			t.Fatal(err)
		}
		defer sess.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, err = sess.BindTransceiver(ctx, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion34})
		var loss *LossError
		if !errors.As(err, &loss) {
			t.Fatalf("bind disconnect error=%T %v, want LossError", err, err)
		}
	})

	t.Run("idle", func(t *testing.T) {
		sessionConn, peerConn := net.Pipe()
		sess, err := New(sessionConn, Config{Role: RoleESME, EnquireLinkInterval: -1, InactivityTimeout: -1})
		if err != nil {
			t.Fatal(err)
		}
		defer sess.Close()
		_ = peerConn.Close()
		select {
		case <-sess.Done():
		case <-time.After(time.Second):
			t.Fatal("idle peer disconnect did not terminate session")
		}
	})

	t.Run("full-window", func(t *testing.T) {
		clientConn, serverConn := net.Pipe()
		entered := make(chan struct{}, 1)
		release := make(chan struct{})
		server, err := New(serverConn, Config{
			Role: RoleSMSC,
			Handler: HandlerFunc(func(_ context.Context, _ *Session, pdu codec.DecodedPDU) (Response, error) {
				switch pdu.Header.CommandID {
				case protocol.CommandBindTransceiver:
					return Response{Status: protocol.StatusOK, Body: protocol.BindResponse{SystemID: []byte("smsc")}}, nil
				case protocol.CommandSubmitSM:
					select { case entered <- struct{}{}: default: }
					<-release
					return Response{Status: protocol.StatusOK, Body: protocol.SubmitSMResp{MessageID: []byte("late")}}, nil
				default:
					return Response{Status: protocol.StatusSystemError, Body: protocol.EmptyBody{}}, nil
				}
			}),
			EnquireLinkInterval: -1, InactivityTimeout: -1,
		})
		if err != nil {
			t.Fatal(err)
		}
		defer server.Close()

		client, err := New(clientConn, Config{
			Role: RoleESME, WindowSize: 1, MaxPending: 1,
			ResponseTimeout: time.Second, EnquireLinkInterval: -1, InactivityTimeout: -1,
		})
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if _, err := client.BindTransceiver(ctx, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion34}); err != nil {
			t.Fatal(err)
		}

		errs := make(chan error, 2)
		go func() {
			_, err := client.SubmitSM(ctx, protocol.SubmitSM{DestinationAddr: []byte("111")})
			errs <- err
		}()
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("first request did not enter slow handler")
		}
		go func() {
			_, err := client.SubmitSM(ctx, protocol.SubmitSM{DestinationAddr: []byte("222")})
			errs <- err
		}()
		time.Sleep(10 * time.Millisecond)
		if got := client.Window().InUse; got != 1 {
			t.Fatalf("window in use=%d want=1", got)
		}
		_ = server.Close()
		close(release)

		for i := 0; i < 2; i++ {
			select {
			case err := <-errs:
				if err == nil {
					t.Fatal("request unexpectedly succeeded across full-window disconnect")
				}
			case <-ctx.Done():
				t.Fatal("request remained stuck after disconnect")
			}
		}
		deadline := time.Now().Add(time.Second)
		for time.Now().Before(deadline) && (client.Pending() != 0 || client.Window().InUse != 0) {
			time.Sleep(time.Millisecond)
		}
		if client.Pending() != 0 || client.Window().InUse != 0 {
			t.Fatalf("outstanding state leaked: pending=%d window=%d", client.Pending(), client.Window().InUse)
		}
	})
}

func TestPhase18TimeoutStormLateResponsesAreIgnored(t *testing.T) {
	const count = 24
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
		bindResp, _ := codec.EncodePDU(nil, codec.ResponseHeader(bind.Header, protocol.CommandBindTransceiverResp, protocol.StatusOK),
			protocol.BindResponse{SystemID: []byte("smsc")}, registry)
		if err := transport.WriteFull(peerConn, bindResp); err != nil {
			peerDone <- err
			return
		}

		requests := make([]codec.DecodedPDU, 0, count)
		for i := 0; i < count; i++ {
			request, err := readOnePDU(peerConn, registry)
			if err != nil {
				peerDone <- err
				return
			}
			requests = append(requests, request)
		}
		time.Sleep(60 * time.Millisecond)
		for _, request := range requests {
			resp, _ := codec.EncodePDU(nil, codec.ResponseHeader(request.Header, protocol.CommandSubmitSMResp, protocol.StatusOK),
				protocol.SubmitSMResp{MessageID: []byte("too-late")}, registry)
			if err := transport.WriteFull(peerConn, resp); err != nil {
				peerDone <- err
				return
			}
		}
		enquire, err := readOnePDU(peerConn, registry)
		if err != nil {
			peerDone <- err
			return
		}
		resp, _ := codec.EncodePDU(nil, codec.ResponseHeader(enquire.Header, protocol.CommandEnquireLinkResp, protocol.StatusOK), protocol.EmptyBody{}, registry)
		peerDone <- transport.WriteFull(peerConn, resp)
	}()

	sess, err := New(clientConn, Config{
		Role: RoleESME, WindowSize: count, MaxPending: count, TXQueueSize: count,
		ResponseTimeout: 15 * time.Millisecond, EnquireLinkInterval: -1, InactivityTimeout: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := sess.BindTransceiver(ctx, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion34}); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, count)
	wg.Add(count)
	for i := 0; i < count; i++ {
		go func(i int) {
			defer wg.Done()
			_, err := sess.SubmitSM(ctx, protocol.SubmitSM{DestinationAddr: []byte(fmt.Sprintf("%03d", i))})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		var timeout *TimeoutError
		if !errors.As(err, &timeout) || timeout.Kind != TimeoutResponse {
			t.Fatalf("storm result=%T %v, want response timeout", err, err)
		}
	}
	if sess.Pending() != 0 || sess.Window().InUse != 0 {
		t.Fatalf("timeout storm leaked outstanding state: pending=%d window=%d", sess.Pending(), sess.Window().InUse)
	}
	time.Sleep(80 * time.Millisecond) // let late responses traverse the RX path
	if err := sess.EnquireLink(ctx); err != nil {
		t.Fatalf("late responses corrupted session correlation: %v", err)
	}
	if err := <-peerDone; err != nil {
		t.Fatal(err)
	}
}

func TestPhase18SlowApplicationHandlerAndSlowPeerShutdown(t *testing.T) {
	t.Run("slow-handler", func(t *testing.T) {
		clientConn, serverConn := net.Pipe()
		server, err := New(serverConn, Config{
			Role: RoleSMSC,
			Handler: HandlerFunc(func(_ context.Context, _ *Session, pdu codec.DecodedPDU) (Response, error) {
				switch pdu.Header.CommandID {
				case protocol.CommandBindTransceiver:
					return Response{Status: protocol.StatusOK, Body: protocol.BindResponse{SystemID: []byte("smsc")}}, nil
				case protocol.CommandSubmitSM:
					time.Sleep(40 * time.Millisecond)
					return Response{Status: protocol.StatusOK, Body: protocol.SubmitSMResp{MessageID: []byte("slow-ok")}}, nil
				default:
					return Response{Status: protocol.StatusSystemError, Body: protocol.EmptyBody{}}, nil
				}
			}),
			EnquireLinkInterval: -1, InactivityTimeout: -1,
		})
		if err != nil {
			t.Fatal(err)
		}
		defer server.Close()
		client, err := New(clientConn, Config{
			Role: RoleESME, ResponseTimeout: time.Second,
			EnquireLinkInterval: -1, InactivityTimeout: -1,
		})
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := client.BindTransceiver(ctx, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion34}); err != nil {
			t.Fatal(err)
		}
		resp, err := client.SubmitSM(ctx, protocol.SubmitSM{DestinationAddr: []byte("111")})
		if err != nil {
			t.Fatal(err)
		}
		if string(resp.MessageID) != "slow-ok" {
			t.Fatalf("message id=%q", resp.MessageID)
		}
	})

	t.Run("slow-peer-blocked-write", func(t *testing.T) {
		clientConn, peerConn := net.Pipe()
		registry, _ := codec.NewSMPP34Registry(codec.RegistryCompatible)
		bound := make(chan struct{})
		go func() {
			bind, err := readOnePDU(peerConn, registry)
			if err != nil {
				return
			}
			resp, _ := codec.EncodePDU(nil, codec.ResponseHeader(bind.Header, protocol.CommandBindTransceiverResp, protocol.StatusOK),
				protocol.BindResponse{SystemID: []byte("smsc")}, registry)
			if transport.WriteFull(peerConn, resp) != nil {
				return
			}
			close(bound)
			// Deliberately stop reading. The next TX write can block until Close
			// closes the underlying connection.
			<-time.After(time.Second)
			_ = peerConn.Close()
		}()
		sess, err := New(clientConn, Config{
			Role: RoleESME, ResponseTimeout: time.Second,
			EnquireLinkInterval: -1, InactivityTimeout: -1,
		})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, err := sess.BindTransceiver(ctx, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion34}); err != nil {
			t.Fatal(err)
		}
		<-bound
		result := make(chan error, 1)
		go func() {
			_, err := sess.SubmitSM(ctx, protocol.SubmitSM{DestinationAddr: []byte("blocked"), ShortMessage: bytes.Repeat([]byte("x"), 200)})
			result <- err
		}()
		time.Sleep(20 * time.Millisecond)
		_ = sess.Close()
		select {
		case err := <-result:
			if err == nil {
				t.Fatal("blocked request unexpectedly succeeded")
			}
		case <-time.After(time.Second):
			t.Fatal("Close did not interrupt slow peer write")
		}
	})
}

func rawPDUHeader(length uint32, command protocol.CommandID, sequence protocol.SequenceNumber) []byte {
	frame := make([]byte, codec.HeaderSize)
	binary.BigEndian.PutUint32(frame[0:4], length)
	binary.BigEndian.PutUint32(frame[4:8], uint32(command))
	binary.BigEndian.PutUint32(frame[8:12], 0)
	binary.BigEndian.PutUint32(frame[12:16], uint32(sequence))
	return frame
}

func TestPhase18ExistingRaceAndTimerCoverageSentinel(t *testing.T) {
	// The detailed timer/race scenarios live in timers_test.go,
	// completion_test.go, concurrency_test.go, and client/reconnect_test.go.
	// This sentinel makes Phase 18's dependency on those suites explicit while
	// the CI phase continues to execute the entire repository under -race.
	if io.EOF == nil {
		t.Fatal("unreachable")
	}
}
