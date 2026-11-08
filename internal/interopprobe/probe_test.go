//go:build interop

package interopprobe

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/message"
	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/session"
)

func TestSanitizeRawPDUReplacesSubmitDataAndPreservesRegisteredDelivery(t *testing.T) {
	registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := codec.EncodePDU(nil, codec.Header{
		CommandID:      protocol.CommandSubmitSM,
		CommandStatus:  protocol.StatusOK,
		SequenceNumber: 7,
	}, protocol.SubmitSM{
		SourceAddr:         []byte("secret-source"),
		DestinationAddr:    []byte("secret-destination"),
		RegisteredDelivery: 0x03,
		DataCoding:         protocol.DataCodingSMSCDefault,
		ShortMessage:       []byte("private message"),
		Optional: []protocol.OptionalParameter{
			{Tag: 0x7fff, Value: []byte("secret-vendor-value")},
		},
	}, registry)
	if err != nil {
		t.Fatal(err)
	}

	frame, ok := sanitizeRawPDU(raw, registry)
	if !ok {
		t.Fatal("sanitizer rejected a supported submit_sm")
	}
	sanitized, err := hex.DecodeString(frame.Hex)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range [][]byte{
		[]byte("secret-source"),
		[]byte("secret-destination"),
		[]byte("private message"),
		[]byte("secret-vendor-value"),
	} {
		if bytes.Contains(sanitized, secret) {
			t.Fatalf("sanitized frame still contains sensitive marker %q", secret)
		}
	}
	decoded, err := codec.DecodePDU(sanitized, registry)
	if err != nil {
		t.Fatal(err)
	}
	submit, ok := decoded.Body.(protocol.SubmitSM)
	if !ok {
		t.Fatalf("decoded body type = %T, want protocol.SubmitSM", decoded.Body)
	}
	if string(submit.SourceAddr) != "1000" || string(submit.DestinationAddr) != "2000" {
		t.Fatalf("addresses were not replaced: source=%q destination=%q", submit.SourceAddr, submit.DestinationAddr)
	}
	if submit.RegisteredDelivery != 0x03 {
		t.Fatalf("registered_delivery = 0x%02x, want 0x03", submit.RegisteredDelivery)
	}
	if len(submit.ShortMessage) != 0 || len(submit.Optional) != 0 {
		t.Fatalf("message data/TLVs were not removed: short_message=%d optional=%d", len(submit.ShortMessage), len(submit.Optional))
	}
	if frame.Expect.Reencode != "decode-only" {
		t.Fatalf("sanitized frame reencode mode = %q, want decode-only", frame.Expect.Reencode)
	}
}

func TestSanitizeRawPDUExcludesBindCredentialFrames(t *testing.T) {
	registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := codec.EncodePDU(nil, codec.Header{
		CommandID:      protocol.CommandBindTransceiver,
		CommandStatus:  protocol.StatusOK,
		SequenceNumber: 3,
	}, protocol.BindRequest{
		SystemID:         []byte("secret-system"),
		Password:         []byte("secretpw"),
		SystemType:       []byte("smpp"),
		InterfaceVersion: protocol.InterfaceVersion34,
	}, registry)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := sanitizeRawPDU(raw, registry); ok {
		t.Fatal("bind frame was retained by the sanitizer")
	}
}

func TestSanitizeRawPDUReplacesDeliveryReceiptTextAndIDs(t *testing.T) {
	registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	original := protocol.DeliverSM{
		SourceAddr:      []byte("secret-source"),
		DestinationAddr: []byte("secret-destination"),
		ESMClass:        0x04,
		DataCoding:      protocol.DataCodingSMSCDefault,
		ShortMessage:    []byte("id:secret-message sub:001 dlvrd:001 submit date:2611071200 done date:2611071201 stat:DELIVRD err:000 text:private text"),
		Optional: []protocol.OptionalParameter{
			{Tag: protocol.TLVTagReceiptedMessageID, Value: append([]byte("secret-message"), 0)},
			{Tag: protocol.TLVTagMessageState, Value: []byte{byte(protocol.MessageStateDelivered)}},
			{Tag: 0x7fff, Value: []byte("private-vendor-value")},
		},
	}
	raw, err := codec.EncodePDU(nil, codec.Header{
		CommandID:      protocol.CommandDeliverSM,
		CommandStatus:  protocol.StatusOK,
		SequenceNumber: 9,
	}, original, registry)
	if err != nil {
		t.Fatal(err)
	}

	frame, ok := sanitizeRawPDU(raw, registry)
	if !ok {
		t.Fatal("sanitizer rejected a supported deliver_sm")
	}
	sanitized, err := hex.DecodeString(frame.Hex)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range [][]byte{
		[]byte("secret-source"),
		[]byte("secret-destination"),
		[]byte("secret-message"),
		[]byte("private text"),
		[]byte("private-vendor-value"),
	} {
		if bytes.Contains(sanitized, secret) {
			t.Fatalf("sanitized frame still contains sensitive marker %q", secret)
		}
	}
	decoded, err := codec.DecodePDU(sanitized, registry)
	if err != nil {
		t.Fatal(err)
	}
	deliver, ok := decoded.Body.(protocol.DeliverSM)
	if !ok {
		t.Fatalf("decoded body type = %T, want protocol.DeliverSM", decoded.Body)
	}
	receipt, err := message.ParseDeliveryReceipt(deliver)
	if err != nil {
		t.Fatalf("parse sanitized receipt: %v", err)
	}
	if receipt.MessageID != "fixture-001" || receipt.TextStatus != "DELIVRD" {
		t.Fatalf("sanitized receipt = (%q, %q), want fixture-001 / DELIVRD", receipt.MessageID, receipt.TextStatus)
	}
	if !strings.Contains(string(deliver.ShortMessage), "text:fixture") {
		t.Fatalf("sanitized receipt text missing placeholder: %q", deliver.ShortMessage)
	}
	if frame.Expect.Receipt == nil || frame.Expect.Receipt.MessageID != "fixture-001" {
		t.Fatalf("fixture receipt expectation was not produced: %+v", frame.Expect.Receipt)
	}
}

func TestSanitizeRawPDUExcludesMalformedConflictingReceiptTLVs(t *testing.T) {
	registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := codec.EncodePDU(nil, codec.Header{
		CommandID:      protocol.CommandDeliverSM,
		CommandStatus:  protocol.StatusOK,
		SequenceNumber: 11,
	}, protocol.DeliverSM{
		ESMClass:     0x04,
		DataCoding:   protocol.DataCodingSMSCDefault,
		ShortMessage: []byte("id:fixture-text sub:001 dlvrd:001 submit date:2611071200 done date:2611071201 stat:DELIVRD err:000 text:fixture"),
		Optional: []protocol.OptionalParameter{
			{Tag: protocol.TLVTagReceiptedMessageID, Value: append([]byte("tlv-id-1"), 0)},
			{Tag: protocol.TLVTagReceiptedMessageID, Value: append([]byte("tlv-id-2"), 0)},
		},
	}, registry)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := codec.DecodePDU(raw, registry)
	if err != nil {
		t.Fatal(err)
	}
	deliver, ok := decoded.Body.(protocol.DeliverSM)
	if !ok {
		t.Fatalf("decoded body type = %T, want protocol.DeliverSM", decoded.Body)
	}
	if _, parseErr := message.ParseDeliveryReceipt(deliver); !errors.Is(parseErr, message.ErrInvalidDeliveryReceipt) {
		t.Fatalf("fixture did not contain malformed conflicting receipt TLVs: %v", parseErr)
	}
	if _, ok := sanitizeRawPDU(raw, registry); ok {
		t.Fatal("malformed conflicting receipt evidence was exported as a fixture")
	}
}

func TestUnbindEventDetailsReportsMissingPeerResponse(t *testing.T) {
	registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := codec.EncodePDU(nil, codec.Header{
		CommandID: protocol.CommandUnbind, CommandStatus: protocol.StatusOK, SequenceNumber: 4,
	}, protocol.EmptyBody{}, registry)
	if err != nil {
		t.Fatal(err)
	}
	frame, ok := sanitizeRawPDU(raw, registry)
	if !ok {
		t.Fatal("sanitizer rejected unbind request")
	}
	frame.Direction = "esme_to_smsc"
	packetCapture := &capture{localRole: "esme", registry: registry, frames: []capturedFrame{{frame: frame}}}
	timeout := &session.TimeoutError{
		Kind: session.TimeoutResponse, Command: protocol.CommandUnbind, Sequence: 4, After: 8 * time.Second,
	}
	details := unbindEventDetails(timeout, packetCapture)
	for key, want := range map[string]string{
		"request_sent": "true", "response_observed": "false", "peer_response": "not_observed",
		"timeout_kind": "response", "command_id": "0x00000006", "sequence_number": "4", "timeout": "8s",
	} {
		if details[key] != want {
			t.Fatalf("%s = %q, want %q (all=%v)", key, details[key], want, details)
		}
	}
}

func TestUnbindEventDetailsRecognizesSMSCInitiatedResponse(t *testing.T) {
	registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	requestRaw, err := codec.EncodePDU(nil, codec.Header{
		CommandID: protocol.CommandUnbind, CommandStatus: protocol.StatusOK, SequenceNumber: 9,
	}, protocol.EmptyBody{}, registry)
	if err != nil {
		t.Fatal(err)
	}
	responseRaw, err := codec.EncodePDU(nil, codec.Header{
		CommandID: protocol.CommandUnbindResp, CommandStatus: protocol.StatusOK, SequenceNumber: 9,
	}, protocol.EmptyBody{}, registry)
	if err != nil {
		t.Fatal(err)
	}
	request, ok := sanitizeRawPDU(requestRaw, registry)
	if !ok {
		t.Fatal("sanitizer rejected unbind request")
	}
	response, ok := sanitizeRawPDU(responseRaw, registry)
	if !ok {
		t.Fatal("sanitizer rejected unbind response")
	}
	request.Direction = "smsc_to_esme"
	response.Direction = "esme_to_smsc"
	packetCapture := &capture{
		localRole: "smsc",
		registry:  registry,
		frames:    []capturedFrame{{frame: request}, {frame: response}},
	}
	details := unbindEventDetails(nil, packetCapture)
	if details["request_sent"] != "true" || details["response_observed"] != "true" {
		t.Fatalf("Unbind exchange was not recognized: %v", details)
	}
	if details["peer_response"] != "" {
		t.Fatalf("successful Unbind was reported as missing peer response: %v", details)
	}
}

func TestESMEUnbindUnsupportedSkipsRequest(t *testing.T) {
	called := false
	event := esmeUnbindEvent(false, func() error {
		called = true
		return nil
	}, nil)

	if called {
		t.Fatal("Unbind operation was called for a peer that does not support it")
	}
	if event.Name != "unbind" || event.Status != StatusUnsupported {
		t.Fatalf("event = %+v, want unbind UNSUPPORTED", event)
	}
	for key, want := range map[string]string{
		"reason":            "peer_capability_not_supported",
		"operation":         "not_attempted",
		"peer_capability":   "unbind",
		"request_sent":      "false",
		"response_observed": "false",
	} {
		if event.Details[key] != want {
			t.Fatalf("%s = %q, want %q (details=%v)", key, event.Details[key], want, event.Details)
		}
	}
}

func TestESMEUnbindCanBeOptedIn(t *testing.T) {
	called := false
	event := esmeUnbindEvent(true, func() error {
		called = true
		return nil
	}, nil)

	if !called {
		t.Fatal("Unbind operation was not called when explicitly enabled")
	}
	if event.Name != "unbind" || event.Status != StatusPass {
		t.Fatalf("event = %+v, want unbind PASS", event)
	}
}

func TestESMEUnbindOptInPreservesMissingResponseFailure(t *testing.T) {
	timeout := &session.TimeoutError{
		Kind: session.TimeoutResponse, Command: protocol.CommandUnbind, Sequence: 3, After: 8 * time.Second,
	}
	event := esmeUnbindEvent(true, func() error { return timeout }, nil)
	if event.Status != StatusFail {
		t.Fatalf("event status = %q, want FAIL", event.Status)
	}
	if event.Details["timeout_kind"] != "response" || event.Details["command_id"] != "0x00000006" {
		t.Fatalf("Unbind response timeout details were not preserved: %v", event.Details)
	}
}

func TestSubmitObservationEventRequiresTheOriginalSession(t *testing.T) {
	original := &session.Session{}
	replacement := &session.Session{}
	observation := submitObservation{
		sess:    original,
		details: map[string]string{"data_coding": "0x00"},
	}

	event := submitObservationEvent(observation, original)
	if event.Status != StatusPass || event.Details["session_match"] != "true" {
		t.Fatalf("same-session event = %+v, want PASS with session_match=true", event)
	}

	observation.sess = replacement
	event = submitObservationEvent(observation, original)
	if event.Status != StatusFail || event.Details["session_match"] != "false" {
		t.Fatalf("replacement-session event = %+v, want FAIL with session_match=false", event)
	}
	if event.Details["reason"] != "submit_received_on_replacement_session" {
		t.Fatalf("replacement-session reason = %q", event.Details["reason"])
	}
}

func TestSessionHealthDetailsPreservesPeerCloseCause(t *testing.T) {
	sess, peerConn := newProbeTestSession(t)
	if err := peerConn.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sess.Done():
	case <-time.After(time.Second):
		t.Fatal("session did not close after peer EOF")
	}

	details := sessionHealthDetails(nil, sess)
	if details["session_closed"] != "true" {
		t.Fatalf("session_closed = %q, want true", details["session_closed"])
	}
	if details["close_cause_reason"] != "peer_closed_connection" {
		t.Fatalf("close cause reason = %q, want peer_closed_connection (details=%v)", details["close_cause_reason"], details)
	}
}

func TestSafeErrorDetailsClassifiesBrokenPipe(t *testing.T) {
	err := &net.OpError{Op: "write", Net: "tcp", Err: syscall.EPIPE}
	details := safeErrorDetails(err)
	if details["network_operation"] != "write" || details["network_error"] != "broken_pipe" {
		t.Fatalf("broken pipe classification = %v", details)
	}
}

func newProbeTestSession(t *testing.T) (*session.Session, net.Conn) {
	t.Helper()
	serverConn, peerConn := net.Pipe()
	sess, err := session.New(serverConn, session.Config{
		Role:                session.RoleSMSC,
		SessionInitTimeout:  -1,
		EnquireLinkInterval: -1,
		EnquireLinkTimeout:  -1,
		InactivityTimeout:   -1,
	})
	if err != nil {
		_ = serverConn.Close()
		_ = peerConn.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = sess.Close()
		_ = peerConn.Close()
		sess.Wait()
	})
	return sess, peerConn
}

func TestWaitForBindResponsePrefersWrittenResponseAfterSessionClose(t *testing.T) {
	sess, _ := newProbeTestSession(t)
	responses := make(chan protocol.CommandStatus, 1)
	responses <- protocol.StatusOK

	_ = sess.Close()
	sess.Wait()

	if err := waitForBindResponse(context.Background(), sess, responses); err != nil {
		t.Fatalf("waitForBindResponse() = %v, want success for already-written bind response", err)
	}
}

func TestWaitForBindResponsePreservesPeerCloseCause(t *testing.T) {
	sess, peerConn := newProbeTestSession(t)
	responses := make(chan protocol.CommandStatus, 1)
	if err := peerConn.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sess.Done():
	case <-time.After(time.Second):
		t.Fatal("session did not close after peer EOF")
	}

	err := waitForBindResponse(context.Background(), sess, responses)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("waitForBindResponse() error = %v, want wrapped io.EOF", err)
	}
	if got := safeErrorDetails(err)["reason"]; got != "peer_closed_connection" {
		t.Fatalf("safe error reason = %q, want peer_closed_connection", got)
	}
}

type probeReadErrorConn struct {
	net.Conn
	readErr error
}

func (c *probeReadErrorConn) Read([]byte) (int, error) {
	return 0, c.readErr
}

func TestTCPTraceCorrelatesBindResponseAndOrdersResetBeforeLocalClose(t *testing.T) {
	trace := newTCPTrace()
	local, peer := net.Pipe()
	defer peer.Close()
	tracked := trace.track(local)
	tracked.Conn = &probeReadErrorConn{Conn: local, readErr: syscall.ECONNRESET}

	// Feed a bind_transceiver_resp in two fragments to exercise stream framing.
	frame := make([]byte, 16)
	binary.BigEndian.PutUint32(frame[0:4], uint32(len(frame)))
	binary.BigEndian.PutUint32(frame[4:8], uint32(protocol.CommandBindTransceiverResp))
	binary.BigEndian.PutUint32(frame[8:12], uint32(protocol.StatusOK))
	binary.BigEndian.PutUint32(frame[12:16], 77)
	tracked.observeBytes("smsc_to_peer", frame[:5])
	tracked.observeBytes("smsc_to_peer", frame[5:])

	select {
	case status := <-tracked.bindResponses:
		if status != protocol.StatusOK {
			t.Fatalf("bind response status = 0x%08x, want OK", uint32(status))
		}
	default:
		t.Fatal("complete bind_transceiver_resp was not correlated to this TCP connection")
	}

	if _, err := tracked.Read(make([]byte, 32)); !errors.Is(err, syscall.ECONNRESET) {
		t.Fatalf("tracked Read error = %v, want ECONNRESET", err)
	}
	if err := tracked.Close(); err != nil {
		t.Fatalf("tracked Close: %v", err)
	}

	events := trace.reportEvents()
	wantNames := []string{"tcp_accepted", "tcp_pdu_written", "tcp_read_error", "tcp_local_close"}
	if len(events) != len(wantNames) {
		t.Fatalf("trace event count = %d, want %d: %+v", len(events), len(wantNames), events)
	}
	for i, want := range wantNames {
		if events[i].Name != want {
			t.Fatalf("trace event[%d] = %q, want %q", i, events[i].Name, want)
		}
		if events[i].Details["connection_id"] != "1" {
			t.Fatalf("trace event[%d] connection_id = %q, want 1", i, events[i].Details["connection_id"])
		}
	}
	if events[1].Details["command_id"] != "0x80000009" || events[1].Details["sequence_number"] != "77" {
		t.Fatalf("bind response metadata = %v", events[1].Details)
	}
	if events[2].Details["network_error"] != "connection_reset" {
		t.Fatalf("read error metadata = %v", events[2].Details)
	}
	if events[3].Details["prior_io_error"] != "read:connection_reset" {
		t.Fatalf("local close ordering = %v, want read reset before local close", events[3].Details)
	}
}
