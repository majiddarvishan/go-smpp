package session

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/protocol"
)

func TestSessionObservabilityCountersEventsAndOptInTrace(t *testing.T) {
	esmeConn, smscConn := net.Pipe()
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))

	server, err := New(smscConn, Config{
		Role:                RoleSMSC,
		SessionInitTimeout:  time.Second,
		EnquireLinkInterval: -1,
		InactivityTimeout:   -1,
		Handler: HandlerFunc(func(_ context.Context, _ *Session, pdu codec.DecodedPDU) (Response, error) {
			switch pdu.Header.CommandID {
			case protocol.CommandBindTransceiver:
				return Response{Status: protocol.StatusOK, Body: protocol.BindResponse{SystemID: []byte("smsc")}}, nil
			case protocol.CommandSubmitSM:
				return Response{Status: protocol.StatusOK, Body: protocol.SubmitSMResp{MessageID: []byte("observed")}}, nil
			default:
				return Response{Status: protocol.StatusSystemError, Body: protocol.EmptyBody{}}, nil
			}
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	var mu sync.Mutex
	var events []Event
	var traces []PacketTrace
	client, err := New(esmeConn, Config{
		Role:                RoleESME,
		SessionInitTimeout:  time.Second,
		EnquireLinkInterval: -1,
		InactivityTimeout:   -1,
		Logger:              logger,
		Observer: ObserverFunc(func(event Event) {
			mu.Lock()
			events = append(events, event)
			mu.Unlock()
		}),
		PacketTracer: PacketTracerFunc(func(trace PacketTrace) {
			mu.Lock()
			traces = append(traces, trace)
			mu.Unlock()
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := client.BindTransceiver(ctx, protocol.BindRequest{SystemID: []byte("esme"), Password: []byte("secret"), InterfaceVersion: protocol.InterfaceVersion34}); err != nil {
		t.Fatal(err)
	}
	resp, err := client.SubmitSM(ctx, protocol.SubmitSM{DestinationAddr: []byte("123"), ShortMessage: []byte("hello")})
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.MessageID) != "observed" {
		t.Fatalf("message id=%q", resp.MessageID)
	}
	if err := client.EnquireLink(ctx); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(time.Second)
	for {
		metrics := client.Metrics()
		mu.Lock()
		traceCount := len(traces)
		mu.Unlock()
		if metrics.RequestsSent == 3 && metrics.ResponseRTTSamples == 3 && traceCount == 6 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("observability did not catch up: metrics=%+v traces=%d", metrics, traceCount)
		}
		time.Sleep(time.Millisecond)
	}
	metrics := client.Metrics()
	if metrics.RequestsSent != 3 || metrics.ResponsesReceived != 3 {
		t.Fatalf("client traffic metrics=%+v", metrics)
	}
	if metrics.ResponseRTTSamples != 3 || metrics.ResponseRTTLast < 0 || metrics.ResponseRTTMax < 0 || metrics.ResponseRTTTotal < 0 {
		t.Fatalf("client RTT metrics=%+v", metrics)
	}
	if metrics.EnquireLinkSent != 1 || metrics.EnquireLinkResponses != 1 {
		t.Fatalf("client enquire-link metrics=%+v", metrics)
	}
	if metrics.Window.InUse != 0 || metrics.Window.Acquired != 3 {
		t.Fatalf("client window metrics=%+v", metrics.Window)
	}
	deadline = time.Now().Add(time.Second)
	var serverMetrics MetricsSnapshot
	for {
		serverMetrics = server.Metrics()
		if serverMetrics.RequestsReceived == 3 && serverMetrics.ResponsesSent == 3 && serverMetrics.EnquireLinkReceived == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server observability did not catch up: %+v", serverMetrics)
		}
		time.Sleep(time.Millisecond)
	}

	mu.Lock()
	gotEvents := append([]Event(nil), events...)
	gotTraces := append([]PacketTrace(nil), traces...)
	mu.Unlock()
	if !hasEventKind(gotEvents, EventRequestSent) || !hasEventKind(gotEvents, EventResponseReceived) || !hasEventKind(gotEvents, EventResponseRTT) || !hasEventKind(gotEvents, EventEnquireLinkSent) {
		t.Fatalf("missing expected events: %+v", gotEvents)
	}
	if len(gotTraces) != 6 {
		t.Fatalf("trace count=%d want 6", len(gotTraces))
	}
	for _, trace := range gotTraces {
		if trace.Length < codec.HeaderSize {
			t.Fatalf("invalid trace length=%d", trace.Length)
		}
		if trace.RawPDU != nil {
			t.Fatal("raw PDU unexpectedly exposed without TraceRawPDU opt-in")
		}
	}
	if logs.Len() != 0 {
		t.Fatalf("ordinary per-PDU logging must remain disabled by default: %s", logs.String())
	}
}

func TestPacketTraceRawPDURequiresExplicitOptIn(t *testing.T) {
	local, peer := net.Pipe()
	defer peer.Close()
	traceCh := make(chan PacketTrace, 1)
	sess, err := New(local, Config{
		Role:                RoleESME,
		SessionInitTimeout:  -1,
		EnquireLinkInterval: -1,
		InactivityTimeout:   -1,
		TraceRawPDU:         true,
		PacketTracer: PacketTracerFunc(func(trace PacketTrace) {
			if trace.Direction == PacketOutbound {
				select {
				case traceCh <- trace:
				default:
				}
			}
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	peerDone := make(chan error, 1)
	go func() {
		request, err := readOnePDU(peer, registry)
		if err != nil {
			peerDone <- err
			return
		}
		frame, err := codec.EncodePDU(nil, codec.ResponseHeader(request.Header, protocol.CommandBindTransceiverResp, protocol.StatusOK), protocol.BindResponse{}, registry)
		if err == nil {
			_, err = peer.Write(frame)
		}
		peerDone <- err
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := sess.BindTransceiver(ctx, protocol.BindRequest{SystemID: []byte("esme"), InterfaceVersion: protocol.InterfaceVersion34}); err != nil {
		t.Fatal(err)
	}
	if err := <-peerDone; err != nil {
		t.Fatal(err)
	}
	select {
	case trace := <-traceCh:
		if len(trace.RawPDU) != trace.Length || len(trace.RawPDU) < codec.HeaderSize {
			t.Fatalf("raw trace length=%d frame length=%d", len(trace.RawPDU), trace.Length)
		}
	case <-time.After(time.Second):
		t.Fatal("raw packet trace not delivered")
	}
}

func hasEventKind(events []Event, kind EventKind) bool {
	for _, event := range events {
		if event.Kind == kind {
			return true
		}
	}
	return false
}

func BenchmarkSessionMetricsSnapshot(b *testing.B) {
	sess := &Session{window: newRequestWindow(1024, nil)}
	sess.metrics.requestsSent.Store(1000)
	sess.metrics.responsesReceived.Store(1000)
	sess.metrics.rttSamples.Store(1000)
	sess.metrics.rttTotalNS.Store(int64(time.Second))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sess.Metrics()
	}
}

func TestSessionMetricsSnapshotIsAllocationFree(t *testing.T) {
	sess := &Session{window: newRequestWindow(1024, nil)}
	if allocs := testing.AllocsPerRun(1000, func() { _ = sess.Metrics() }); allocs != 0 {
		t.Fatalf("Metrics allocations/run=%v want 0", allocs)
	}
}
