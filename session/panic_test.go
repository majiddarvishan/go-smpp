package session

import (
	"bytes"
	"context"
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
)

// These tests pin the panic policy in panic.go: a panic in caller-supplied
// code or on a session goroutine is recovered, counted, logged and emitted,
// and it costs as little as possible: one PDU, one notification, or at most
// the one session it happened on. None of them may crash the test binary.

const panicTestTimeout = 2 * time.Second

// quiet keeps the recovered panics' log lines, stacks included, out of the
// test output.
var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func bindForPanicTest(t *testing.T, smscConfig, esmeConfig Config) (smsc, esme *Session) {
	t.Helper()
	smscConn, esmeConn := net.Pipe()
	smscConfig.Role, esmeConfig.Role = RoleSMSC, RoleESME
	for _, cfg := range []*Config{&smscConfig, &esmeConfig} {
		cfg.SessionInitTimeout = panicTestTimeout
		cfg.EnquireLinkInterval = -1
		cfg.InactivityTimeout = -1
		if cfg.Logger == nil {
			cfg.Logger = quiet
		}
	}
	smsc, err := New(smscConn, smscConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = smsc.Close(); smsc.Wait() })
	esme, err = New(esmeConn, esmeConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = esme.Close(); esme.Wait() })
	ctx, cancel := context.WithTimeout(context.Background(), panicTestTimeout)
	defer cancel()
	if _, err := esme.BindTransceiver(ctx, protocol.BindRequest{SystemID: []byte("esme"), InterfaceVersion: protocol.InterfaceVersion34}); err != nil {
		t.Fatalf("bind: %v", err)
	}
	return smsc, esme
}

// bindOK answers binds and hands everything else to next.
func bindOK(next func(InboundPDU) (Response, error)) Handler {
	return HandlerFunc(func(_ context.Context, _ *Session, pdu InboundPDU) (Response, error) {
		if isBindRequest(pdu.Header.CommandID) {
			return Response{Status: protocol.StatusOK, Body: protocol.BindResponse{SystemID: []byte("smsc")}}, nil
		}
		return next(pdu)
	})
}

func waitPanics(t *testing.T, s *Session, want uint64) {
	t.Helper()
	deadline := time.Now().Add(panicTestTimeout)
	for s.Metrics().PanicsRecovered < want {
		if time.Now().After(deadline) {
			t.Fatalf("PanicsRecovered = %d, want at least %d", s.Metrics().PanicsRecovered, want)
		}
		time.Sleep(time.Millisecond)
	}
}

func wantStatus(t *testing.T, err error, status protocol.CommandStatus) {
	t.Helper()
	var resp *ResponseError
	if !errors.As(err, &resp) || resp.Status != status {
		t.Fatalf("err = %v, want a response with status 0x%08x", err, uint32(status))
	}
}

type eventLog struct {
	mu     sync.Mutex
	events []Event
}

func (l *eventLog) OnEvent(e Event) {
	l.mu.Lock()
	l.events = append(l.events, e)
	l.mu.Unlock()
}

func (l *eventLog) panicReasons() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for _, e := range l.events {
		if e.Kind == EventPanicRecovered {
			out = append(out, e.Reason)
		}
	}
	return out
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func TestHandlerPanicFailsOnlyThatPDU(t *testing.T) {
	events := &eventLog{}
	logs := &syncBuffer{}
	smsc, esme := bindForPanicTest(t, Config{
		Observer: events,
		Logger:   slog.New(slog.NewTextHandler(logs, nil)),
		Handler: bindOK(func(InboundPDU) (Response, error) {
			panic("handler exploded")
		}),
	}, Config{})

	ctx, cancel := context.WithTimeout(context.Background(), panicTestTimeout)
	defer cancel()
	for i := 0; i < 2; i++ {
		_, err := esme.SubmitSM(ctx, protocol.SubmitSM{DestinationAddr: []byte("1"), ShortMessage: []byte("x")})
		wantStatus(t, err, protocol.StatusSystemError)
	}
	if err := esme.EnquireLink(ctx); err != nil {
		t.Fatalf("session unusable after a handler panic: %v", err)
	}
	waitPanics(t, smsc, 2)
	if smsc.State() != protocol.StateBoundTRX || esme.State() != protocol.StateBoundTRX {
		t.Fatalf("states after handler panics: smsc=%s esme=%s, want both still bound", smsc.State(), esme.State())
	}
	reasons := events.panicReasons()
	if len(reasons) != 2 || !strings.Contains(reasons[0], sourceHandler) || !strings.Contains(reasons[0], "handler exploded") {
		t.Fatalf("panic events = %q, want two naming %s and the panic value", reasons, sourceHandler)
	}
	if out := logs.String(); !strings.Contains(out, "smpp_panic_recovered") || !strings.Contains(out, "panic_test.go") {
		t.Fatalf("log does not carry the recovered panic and its stack:\n%s", out)
	}
}

func TestHandlerPanicOnBindRefusesThatBind(t *testing.T) {
	smscConn, esmeConn := net.Pipe()
	smsc, err := New(smscConn, Config{Role: RoleSMSC, Logger: quiet, Handler: HandlerFunc(func(context.Context, *Session, InboundPDU) (Response, error) {
		panic(errors.New("authenticator bug"))
	})})
	if err != nil {
		t.Fatal(err)
	}
	defer smsc.Close()
	esme, err := New(esmeConn, Config{Role: RoleESME, Logger: quiet})
	if err != nil {
		t.Fatal(err)
	}
	defer esme.Close()
	ctx, cancel := context.WithTimeout(context.Background(), panicTestTimeout)
	defer cancel()
	_, err = esme.BindTransceiver(ctx, protocol.BindRequest{SystemID: []byte("esme"), InterfaceVersion: protocol.InterfaceVersion34})
	wantStatus(t, err, protocol.StatusSystemError)
	waitPanics(t, smsc, 1)
	if smsc.State() != protocol.StateOpen {
		t.Fatalf("smsc state = %s after a panicking bind, want Open (bind refused, session kept)", smsc.State())
	}
}

func TestOneWayHandlerPanicIsRecovered(t *testing.T) {
	smscConn, esmeConn := net.Pipe()
	esme, err := New(esmeConn, Config{Role: RoleESME, Logger: quiet, Handler: HandlerFunc(func(context.Context, *Session, InboundPDU) (Response, error) {
		panic("outbind handler exploded")
	})})
	if err != nil {
		t.Fatal(err)
	}
	defer esme.Close()
	smsc, err := New(smscConn, Config{Role: RoleSMSC, Logger: quiet})
	if err != nil {
		t.Fatal(err)
	}
	defer smsc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), panicTestTimeout)
	defer cancel()
	if err := smsc.Outbind(ctx, protocol.Outbind{SystemID: []byte("smsc")}); err != nil {
		t.Fatal(err)
	}
	waitPanics(t, esme, 1)
	select {
	case <-esme.Done():
		t.Fatalf("esme session ended after a one-way handler panic: %v", esme.Err())
	default:
	}
}

// panickingLogHandler is an slog.Handler that panics on every record.
type panickingLogHandler struct{ slog.Handler }

func (panickingLogHandler) Enabled(context.Context, slog.Level) bool { return true }
func (panickingLogHandler) Handle(context.Context, slog.Record) error {
	panic("log handler exploded")
}

func TestNotificationCallbackPanicsDropOnlyTheNotification(t *testing.T) {
	boom := func() { panic("callback exploded") }
	smsc, esme := bindForPanicTest(t,
		Config{
			Handler: bindOK(func(pdu InboundPDU) (Response, error) {
				return Response{Status: protocol.StatusOK, Body: protocol.SubmitSMResp{MessageID: []byte("id")}}, nil
			}),
			// Logged to directly below: a panicking slog.Handler must cost
			// only that record.
			Logger: slog.New(panickingLogHandler{}),
		},
		Config{
			Observer:       ObserverFunc(func(Event) { boom() }),
			PacketTracer:   PacketTracerFunc(func(PacketTrace) { boom() }),
			WindowObserver: func(WindowSnapshot) { boom() },
		})

	ctx, cancel := context.WithTimeout(context.Background(), panicTestTimeout)
	defer cancel()
	for i := 0; i < 3; i++ {
		resp, err := esme.SubmitSM(ctx, protocol.SubmitSM{DestinationAddr: []byte("1"), ShortMessage: []byte("x")})
		if err != nil || string(resp.MessageID) != "id" {
			t.Fatalf("submit %d with panicking observers: resp=%+v err=%v", i, resp, err)
		}
	}
	// Each submit fires the window observer, packet tracer and observer more
	// than once; every one of those panics was recovered.
	waitPanics(t, esme, 9)
	if esme.State() != protocol.StateBoundTRX {
		t.Fatalf("esme state = %s, want still bound", esme.State())
	}

	smsc.logger.Error("probe")
	waitPanics(t, smsc, 1)
}

func TestFlowControllerPanicIsRecovered(t *testing.T) {
	smscConn, _ := net.Pipe()
	s, err := New(smscConn, Config{Role: RoleESME, Logger: quiet, FlowController: FlowControllerFunc(func(CongestionEvent) {
		panic("flow controller exploded")
	})})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.notifyCongestion(CongestionEvent{State: 90})
	if got := s.Metrics().PanicsRecovered; got != 1 {
		t.Fatalf("PanicsRecovered = %d, want 1", got)
	}
}

// panickingConn is a net.Conn whose Read panics, standing in for a defective
// caller-supplied transport.
type panickingConn struct{ net.Conn }

func (panickingConn) Read([]byte) (int, error) { panic("conn read exploded") }
func (c panickingConn) Close() error           { return c.Conn.Close() }

func TestConnPanicEndsOnlyThatSession(t *testing.T) {
	left, right := net.Pipe()
	defer right.Close()
	s, err := New(panickingConn{left}, Config{Role: RoleESME, Logger: quiet})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.Done():
	case <-time.After(panicTestTimeout):
		t.Fatal("session did not end after its connection panicked")
	}
	s.Wait()
	var p *protocol.PanicError
	if !errors.As(s.Err(), &p) || p.Source != sourceRXLoop || len(p.Stack) == 0 {
		t.Fatalf("Err() = %v, want a *protocol.PanicError from %s with a stack", s.Err(), sourceRXLoop)
	}
	if s.Metrics().PanicsRecovered != 1 {
		t.Fatalf("PanicsRecovered = %d, want 1", s.Metrics().PanicsRecovered)
	}
	ctx, cancel := context.WithTimeout(context.Background(), panicTestTimeout)
	defer cancel()
	var loss *LossError
	if err := s.EnquireLink(ctx); !errors.As(err, &loss) || !errors.As(err, &p) {
		t.Fatalf("request after the panic: err = %v, want a LossError wrapping the PanicError", err)
	}
}

const vendorPanicCommand protocol.CommandID = 0x4A0000F1

func vendorPanicRegistry(t *testing.T, decode codec.CommandDecoder, encode, encodeResp codec.CommandEncoder) *codec.Registry {
	t.Helper()
	builder, err := codec.NewSMPP34RegistryBuilder(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	for _, def := range []codec.CommandDefinition{
		{ID: vendorPanicCommand, Name: "vendor_panic", Decode: decode, Encode: encode,
			Actor: codec.ActorESME, AllowedStates: []protocol.SessionState{protocol.StateBoundTRX}},
		{ID: vendorPanicCommand.ResponseID(), Name: "vendor_panic_resp", Decode: decodeVendorEmpty, Encode: encodeResp},
	} {
		if err := builder.RegisterCommand(def); err != nil {
			t.Fatal(err)
		}
	}
	registry, err := builder.Freeze()
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

// rawPeerForPanicTest starts an SMSC session on registry and binds it from a
// raw peer connection, which the test then drives frame by frame. The library
// does not originate vendor commands, so a raw peer is the way to send one.
func rawPeerForPanicTest(t *testing.T, registry *codec.Registry) (*Session, net.Conn) {
	t.Helper()
	smscConn, peer := net.Pipe()
	t.Cleanup(func() { _ = peer.Close() })
	smsc, err := New(smscConn, Config{Role: RoleSMSC, Registry: registry, Logger: quiet, Handler: bindOK(func(InboundPDU) (Response, error) {
		return Response{Status: protocol.StatusOK, Body: protocol.EmptyBody{}}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = smsc.Close(); smsc.Wait() })
	reply := sendRaw(t, peer, registry, protocol.CommandBindTransceiver, 1,
		protocol.BindRequest{SystemID: []byte("esme"), InterfaceVersion: protocol.InterfaceVersion34})
	if reply.Header.CommandStatus != protocol.StatusOK {
		t.Fatalf("bind status 0x%08x", uint32(reply.Header.CommandStatus))
	}
	return smsc, peer
}

func sendRaw(t *testing.T, peer net.Conn, registry *codec.Registry, command protocol.CommandID, sequence protocol.SequenceNumber, body any) codec.DecodedPDU {
	t.Helper()
	frame, err := codec.EncodePDU(nil, codec.Header{CommandID: command, SequenceNumber: sequence}, body, registry)
	if err != nil {
		t.Fatal(err)
	}
	_ = peer.SetDeadline(time.Now().Add(panicTestTimeout))
	if _, err := peer.Write(frame); err != nil {
		t.Fatal(err)
	}
	reply, err := readOnePDU(peer, registry)
	if err != nil {
		t.Fatal(err)
	}
	if reply.Header.SequenceNumber != sequence {
		t.Fatalf("reply sequence %d, want %d", reply.Header.SequenceNumber, sequence)
	}
	return reply
}

func TestCommandDecoderPanicRejectsOnlyThatPDU(t *testing.T) {
	smscRegistry := vendorPanicRegistry(t, func(codec.Header, []byte) (any, error) { panic("decoder exploded") }, encodeVendorEmpty, encodeVendorEmpty)
	peerRegistry := vendorPanicRegistry(t, decodeVendorEmpty, encodeVendorEmpty, encodeVendorEmpty)
	smsc, peer := rawPeerForPanicTest(t, smscRegistry)

	reply := sendRaw(t, peer, peerRegistry, vendorPanicCommand, 2, protocol.EmptyBody{})
	if reply.Header.CommandID != protocol.CommandGenericNACK || reply.Header.CommandStatus != protocol.StatusSystemError {
		t.Fatalf("reply to a PDU whose decoder panicked = 0x%08x status 0x%08x, want generic_nack ESME_RSYSERR",
			uint32(reply.Header.CommandID), uint32(reply.Header.CommandStatus))
	}
	if got := smsc.Metrics().PanicsRecovered; got != 1 {
		t.Fatalf("PanicsRecovered = %d, want 1", got)
	}
	if reply := sendRaw(t, peer, peerRegistry, protocol.CommandEnquireLink, 3, protocol.EmptyBody{}); reply.Header.CommandStatus != protocol.StatusOK {
		t.Fatalf("session unusable after a decoder panic: enquire_link status 0x%08x", uint32(reply.Header.CommandStatus))
	}
}

func TestResponseEncoderPanicStillAnswersThePDU(t *testing.T) {
	smscRegistry := vendorPanicRegistry(t, decodeVendorEmpty, encodeVendorEmpty, func([]byte, any) ([]byte, error) { panic("encoder exploded") })
	peerRegistry := vendorPanicRegistry(t, decodeVendorEmpty, encodeVendorEmpty, encodeVendorEmpty)
	smsc, peer := rawPeerForPanicTest(t, smscRegistry)

	reply := sendRaw(t, peer, peerRegistry, vendorPanicCommand, 2, protocol.EmptyBody{})
	if reply.Header.CommandID != protocol.CommandGenericNACK || reply.Header.CommandStatus != protocol.StatusSystemError {
		t.Fatalf("reply = 0x%08x status 0x%08x, want generic_nack ESME_RSYSERR", uint32(reply.Header.CommandID), uint32(reply.Header.CommandStatus))
	}
	if got := smsc.Metrics().PanicsRecovered; got != 1 {
		t.Fatalf("PanicsRecovered = %d, want 1", got)
	}
	if reply := sendRaw(t, peer, peerRegistry, protocol.CommandEnquireLink, 3, protocol.EmptyBody{}); reply.Header.CommandStatus != protocol.StatusOK {
		t.Fatalf("session unusable after an encoder panic: enquire_link status 0x%08x", uint32(reply.Header.CommandStatus))
	}
}

func TestNilAndZeroSessionDoNotPanic(t *testing.T) {
	ctx := context.Background()
	for name, s := range map[string]*Session{"nil": nil, "zero": {}} {
		if err := s.Close(); !errors.Is(err, ErrNotInitialized) {
			t.Errorf("%s Close() = %v", name, err)
		}
		if err := s.Err(); !errors.Is(err, ErrNotInitialized) {
			t.Errorf("%s Err() = %v", name, err)
		}
		if _, err := s.SubmitSM(ctx, protocol.SubmitSM{}); !errors.Is(err, ErrNotInitialized) {
			t.Errorf("%s SubmitSM() = %v", name, err)
		}
		if err := s.AlertNotification(ctx, protocol.AlertNotification{}); !errors.Is(err, ErrNotInitialized) {
			t.Errorf("%s AlertNotification() = %v", name, err)
		}
		select {
		case <-s.Done():
		default:
			t.Errorf("%s Done() is not closed", name)
		}
		s.Wait()
		if s.State() != protocol.StateClosed || s.Metrics() != (MetricsSnapshot{}) {
			t.Errorf("%s getters: state=%s metrics=%+v", name, s.State(), s.Metrics())
		}
	}
	var m *StateMachine
	if err := m.BeginOutbound(protocol.CommandSubmitSM); !errors.Is(err, ErrNotInitialized) {
		t.Errorf("nil StateMachine BeginOutbound() = %v", err)
	}
	m.CancelInbound(protocol.CommandUnbind)
	m.Close()
}

func TestNilHandlerFuncAnswersLikeNoHandler(t *testing.T) {
	var f HandlerFunc
	for command, want := range map[protocol.CommandID]protocol.CommandStatus{
		protocol.CommandBindTransceiver: protocol.StatusBindFailed,
		protocol.CommandSubmitSM:        protocol.StatusSystemError,
	} {
		resp, err := f.Handle(context.Background(), nil, InboundPDU{Header: codec.Header{CommandID: command}})
		if err != nil || resp.Status != want {
			t.Errorf("nil HandlerFunc on 0x%08x = %+v, %v; want status 0x%08x", uint32(command), resp, err, uint32(want))
		}
	}
}
