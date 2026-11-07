//go:build interop

package interopprobe

import (
	"bytes"
	"encoding/hex"
	"errors"
	"strings"
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
