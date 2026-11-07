//go:build interop

package interopprobe

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"sync/atomic"
	"time"

	"github.com/majiddarvishan/go-smpp/client"
	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/message"
	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/server"
	"github.com/majiddarvishan/go-smpp/session"
)

const (
	StatusPass    = "PASS"
	StatusFail    = "FAIL"
	StatusBlocked = "BLOCKED"
	StatusNotSeen = "NOT_OBSERVED"
)

type Config struct {
	Mode string

	SMSCAddress    string
	SMSCSystemID   string
	SMSCSystemType string
	SMSCPassword   []byte

	ESMEListenAddress string
	ESMESystemID      string
	ESMESystemType    string
	ESMEPassword      []byte

	SourceAddress      string
	DestinationAddress string
	Message            string

	RequestTimeout time.Duration
	PeerWait       time.Duration
	DLRWait        time.Duration
	SubmitWait     time.Duration
}

type Report struct {
	SchemaVersion int           `json:"schema_version"`
	StartedAt     string        `json:"started_at"`
	CompletedAt   string        `json:"completed_at"`
	Platform      string        `json:"platform"`
	Results       []PhaseResult `json:"results"`
	Candidates    []Fixture     `json:"candidate_fixtures"`
}

type PhaseResult struct {
	Name           string  `json:"name"`
	PeerAlias      string  `json:"peer_alias"`
	Role           string  `json:"role"`
	Endpoint       string  `json:"endpoint"`
	Status         string  `json:"status"`
	StartedAt      string  `json:"started_at"`
	CompletedAt    string  `json:"completed_at"`
	FramesCaptured int     `json:"sanitized_frames_captured"`
	FramesDropped  uint64  `json:"frames_dropped_or_excluded"`
	Events         []Event `json:"events"`
}

type Event struct {
	Name    string            `json:"name"`
	Status  string            `json:"status"`
	Details map[string]string `json:"details,omitempty"`
}

type FixtureManifest struct {
	SchemaVersion int       `json:"schema_version"`
	Fixtures      []Fixture `json:"fixtures"`
}

type Fixture struct {
	ID              string         `json:"id"`
	Source          string         `json:"source"`
	PeerAlias       string         `json:"peer_alias"`
	ProtocolVersion string         `json:"protocol_version"`
	CaptureMethod   string         `json:"capture_method"`
	CapturedAt      string         `json:"captured_at"`
	EvidenceRef     string         `json:"evidence_ref"`
	Sanitization    Sanitization   `json:"sanitization"`
	Frames          []FixtureFrame `json:"frames"`
}

type Sanitization struct {
	Reviewed                bool   `json:"reviewed"`
	Policy                  string `json:"policy"`
	BindFramesExcluded      bool   `json:"bind_frames_excluded"`
	SensitiveValuesReplaced bool   `json:"sensitive_values_replaced"`
	UnknownTLVs             string `json:"unknown_tlvs"`
}

type FixtureFrame struct {
	Direction string          `json:"direction"`
	Hex       string          `json:"hex"`
	Expect    FixtureExpected `json:"expect"`
}

type FixtureExpected struct {
	Outcome        string           `json:"outcome"`
	CommandID      string           `json:"command_id"`
	CommandStatus  string           `json:"command_status"`
	SequenceNumber uint32           `json:"sequence_number"`
	BodyType       string           `json:"body_type,omitempty"`
	Reencode       string           `json:"reencode,omitempty"`
	Receipt        *ReceiptExpected `json:"receipt,omitempty"`
}

type ReceiptExpected struct {
	MessageID  string `json:"message_id"`
	TextStatus string `json:"text_status"`
}

type capture struct {
	localRole string
	registry  *codec.Registry
	mu        sync.Mutex
	frames    []capturedFrame
	dropped   atomic.Uint64
}

type capturedFrame struct {
	at    time.Time
	frame FixtureFrame
}

type deliveryObservation struct {
	details map[string]string
}

type submitObservation struct {
	details map[string]string
}

type bindAttempt struct {
	sess   *session.Session
	mode   session.BindMode
	status protocol.CommandStatus
	reason string
}

func Run(ctx context.Context, cfg Config, output io.Writer) Report {
	if output == nil {
		output = io.Discard
	}
	now := time.Now().UTC()
	report := Report{
		SchemaVersion: 1,
		StartedAt:     now.Format(time.RFC3339Nano),
		Platform:      runtime.GOOS + "/" + runtime.GOARCH,
	}
	if cfg.Mode == "smsc" || cfg.Mode == "all" {
		result, fixture := runSMSC(ctx, cfg)
		report.Results = append(report.Results, result)
		if fixture != nil {
			report.Candidates = append(report.Candidates, *fixture)
		}
	}
	if cfg.Mode == "esme" || cfg.Mode == "all" {
		result, fixture := runESME(ctx, cfg, output)
		report.Results = append(report.Results, result)
		if fixture != nil {
			report.Candidates = append(report.Candidates, *fixture)
		}
	}
	report.CompletedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return report
}

func runSMSC(ctx context.Context, cfg Config) (PhaseResult, *Fixture) {
	started := time.Now().UTC()
	result := PhaseResult{
		Name:      "go-smpp-as-esme",
		PeerAlias: "peer-001",
		Role:      "ESME",
		Endpoint:  cfg.SMSCAddress,
		Status:    StatusPass,
		StartedAt: started.Format(time.RFC3339Nano),
	}
	packetCapture, err := newCapture("esme")
	if err != nil {
		result.addEvent("capture_setup", StatusFail, safeErrorDetails(err))
		result.setStatus(StatusFail)
		result.finish(packetCapture)
		return result, nil
	}

	deliveries := make(chan deliveryObservation, 8)
	handler := session.HandlerFunc(func(_ context.Context, _ *session.Session, pdu session.InboundPDU) (session.Response, error) {
		deliver, ok := pdu.Body.(protocol.DeliverSM)
		if !ok {
			return session.Response{Status: protocol.StatusSystemError, Body: protocol.EmptyBody{}}, nil
		}
		observation := deliveryObservation{details: observeDelivery(deliver)}
		select {
		case deliveries <- observation:
		default:
		}
		return session.Response{Status: protocol.StatusOK, Body: protocol.DeliverSMResp{}}, nil
	})

	clientConfig := client.Config{
		Network: "tcp",
		Address: cfg.SMSCAddress,
		SessionConfig: session.Config{
			Profile:             protocol.SMPP34Profile(),
			MaxPending:          4,
			WindowSize:          4,
			TXQueueSize:         16,
			ResponseTimeout:     cfg.RequestTimeout,
			SessionInitTimeout:  cfg.RequestTimeout,
			EnquireLinkInterval: -1,
			EnquireLinkTimeout:  -1,
			InactivityTimeout:   -1,
			Handler:             handler,
			PacketTracer:        session.PacketTracerFunc(packetCapture.tracePacket),
			TraceRawPDU:         true,
		},
	}

	dialCtx, cancelDial := context.WithTimeout(ctx, cfg.RequestTimeout)
	c, err := client.Dial(dialCtx, clientConfig)
	cancelDial()
	if err != nil {
		status := StatusFail
		if isNetworkUnavailable(err) {
			status = StatusBlocked
		}
		result.addEvent("tcp_connect", status, safeErrorDetails(err))
		result.setStatus(status)
		result.finish(packetCapture)
		return result, packetCapture.fixture("simulator-smsc-peer-001", "peer-001", "INT-01-PEER-001-001")
	}
	defer func() {
		_ = c.Close()
		c.Wait()
	}()
	result.addEvent("tcp_connect", StatusPass, map[string]string{"transport": "tcp", "tls": "false", "connection_limit": "1"})

	bindCtx, cancelBind := context.WithTimeout(ctx, cfg.RequestTimeout)
	_, err = c.BindTransceiver(bindCtx, protocol.BindRequest{
		SystemID:         []byte(cfg.SMSCSystemID),
		Password:         append([]byte(nil), cfg.SMSCPassword...),
		SystemType:       []byte(cfg.SMSCSystemType),
		InterfaceVersion: protocol.InterfaceVersion34,
		AddressTON:       protocol.TONUnknown,
		AddressNPI:       protocol.NPIUnknown,
	})
	cancelBind()
	if err != nil {
		result.addEvent("bind_transceiver", StatusFail, safeErrorDetails(err))
		result.setStatus(StatusFail)
		result.finish(packetCapture)
		return result, packetCapture.fixture("simulator-smsc-peer-001", "peer-001", "INT-01-PEER-001-001")
	}
	result.addEvent("bind_transceiver", StatusPass, map[string]string{"bind_type": "TRX", "interface_version": "3.4"})

	linkCtx, cancelLink := context.WithTimeout(ctx, cfg.RequestTimeout)
	err = c.EnquireLink(linkCtx)
	cancelLink()
	if err != nil {
		result.addEvent("enquire_link", StatusFail, safeErrorDetails(err))
		result.setStatus(StatusFail)
	} else {
		result.addEvent("enquire_link", StatusPass, nil)
	}

	registeredDelivery := protocol.RegisteredDelivery{
		SMSCReceipt: protocol.SMSCReceiptModeSuccessOnly,
	}.Encode()
	submitCtx, cancelSubmit := context.WithTimeout(ctx, cfg.RequestTimeout)
	response, submitErr := c.SubmitSM(submitCtx, protocol.SubmitSM{
		SourceAddrTON:      protocol.TONUnknown,
		SourceAddrNPI:      protocol.NPIUnknown,
		SourceAddr:         []byte(cfg.SourceAddress),
		DestAddrTON:        protocol.TONUnknown,
		DestAddrNPI:        protocol.NPIUnknown,
		DestinationAddr:    []byte(cfg.DestinationAddress),
		RegisteredDelivery: registeredDelivery,
		DataCoding:         protocol.DataCodingSMSCDefault,
		ShortMessage:       []byte(cfg.Message),
	})
	cancelSubmit()
	if submitErr != nil {
		var responseError *session.ResponseError
		if errors.As(submitErr, &responseError) {
			result.addEvent("submit_sm_resp", StatusPass, map[string]string{
				"command_status":      fmt.Sprintf("0x%08x", uint32(responseError.Status)),
				"message_accepted":    "false",
				"registered_delivery": fmt.Sprintf("0x%02x", registeredDelivery),
				"message_id_present":  "false",
			})
		} else {
			result.addEvent("submit_sm", StatusFail, safeErrorDetails(submitErr))
			result.setStatus(StatusFail)
		}
	} else {
		result.addEvent("submit_sm_resp", StatusPass, map[string]string{
			"command_status":      "0x00000000",
			"message_accepted":    "true",
			"registered_delivery": fmt.Sprintf("0x%02x", registeredDelivery),
			"message_id_present":  fmt.Sprintf("%t", len(response.MessageID) > 0),
		})
	}

	timer := time.NewTimer(cfg.DLRWait)
	select {
	case observation := <-deliveries:
		result.addEvent("deliver_sm_from_smsc", StatusPass, observation.details)
	case <-timer.C:
		result.addEvent("deliver_sm_from_smsc", StatusNotSeen, map[string]string{"observation_window": cfg.DLRWait.String()})
	case <-ctx.Done():
		result.addEvent("deliver_sm_from_smsc", StatusNotSeen, map[string]string{"reason": "run_context_cancelled"})
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}

	unbindCtx, cancelUnbind := context.WithTimeout(ctx, cfg.RequestTimeout)
	unbindErr := c.Unbind(unbindCtx)
	cancelUnbind()
	if unbindErr != nil {
		result.addEvent("unbind", StatusFail, unbindEventDetails(unbindErr, packetCapture))
		result.setStatus(StatusFail)
	} else {
		result.addEvent("unbind", StatusPass, unbindEventDetails(nil, packetCapture))
	}

	result.finish(packetCapture)
	return result, packetCapture.fixture("simulator-smsc-peer-001", "peer-001", "INT-01-PEER-001-001")
}

func runESME(ctx context.Context, cfg Config, output io.Writer) (PhaseResult, *Fixture) {
	started := time.Now().UTC()
	result := PhaseResult{
		Name:      "go-smpp-as-smsc",
		PeerAlias: "peer-002",
		Role:      "SMSC",
		Endpoint:  cfg.ESMEListenAddress,
		Status:    StatusPass,
		StartedAt: started.Format(time.RFC3339Nano),
	}
	packetCapture, err := newCapture("smsc")
	if err != nil {
		result.addEvent("capture_setup", StatusFail, safeErrorDetails(err))
		result.setStatus(StatusFail)
		result.finish(packetCapture)
		return result, nil
	}

	bindAttempts := make(chan bindAttempt, 4)
	bindResponses := make(chan protocol.CommandStatus, 1)
	submits := make(chan submitObservation, 8)
	authenticator := server.AuthenticatorFunc(func(_ context.Context, sess *session.Session, mode session.BindMode, request protocol.BindRequest) (server.BindResult, error) {
		status := protocol.StatusOK
		reason := ""
		switch {
		case mode != session.BindTRX:
			status, reason = protocol.StatusBindFailed, "bind_mode_mismatch"
		case !bytes.Equal(request.SystemID, []byte(cfg.ESMESystemID)):
			status, reason = protocol.StatusInvalidSystemID, "system_id_mismatch"
		case !bytes.Equal(request.Password, cfg.ESMEPassword):
			status, reason = protocol.StatusInvalidPassword, "password_mismatch"
		case !bytes.Equal(request.SystemType, []byte(cfg.ESMESystemType)):
			status, reason = protocol.StatusInvalidSystemType, "system_type_mismatch"
		case request.InterfaceVersion != protocol.InterfaceVersion34:
			status, reason = protocol.StatusBindFailed, "interface_version_mismatch"
		}
		attempt := bindAttempt{sess: sess, mode: mode, status: status, reason: reason}
		select {
		case bindAttempts <- attempt:
		default:
		}
		return server.BindResult{Status: status, SystemID: []byte("go-smpp-interop")}, nil
	})
	submitHandler := server.SubmitHandlerFunc(func(_ context.Context, _ *session.Session, request protocol.SubmitSM) (server.SubmitResult, error) {
		observation := submitObservation{details: observeSubmit(request)}
		select {
		case submits <- observation:
		default:
		}
		return server.SubmitResult{Status: protocol.StatusOK, MessageID: []byte("interop-submit-001")}, nil
	})

	srv, err := server.Listen(ctx, server.Config{
		Network:       "tcp",
		Address:       cfg.ESMEListenAddress,
		MaxSessions:   1,
		Authenticator: authenticator,
		SubmitHandler: submitHandler,
		SessionConfig: session.Config{
			Profile:             protocol.SMPP34Profile(),
			MaxPending:          4,
			WindowSize:          4,
			TXQueueSize:         16,
			ResponseTimeout:     cfg.RequestTimeout,
			SessionInitTimeout:  cfg.PeerWait,
			EnquireLinkInterval: -1,
			EnquireLinkTimeout:  -1,
			InactivityTimeout:   -1,
			Observer: session.ObserverFunc(func(event session.Event) {
				if event.Kind == session.EventResponseSent && event.Command == protocol.CommandBindTransceiverResp {
					select {
					case bindResponses <- event.Status:
					default:
					}
				}
			}),
			PacketTracer: session.PacketTracerFunc(packetCapture.tracePacket),
			TraceRawPDU:  true,
		},
	})
	if err != nil {
		status := StatusFail
		if isNetworkUnavailable(err) {
			status = StatusBlocked
		}
		result.addEvent("listen", status, safeErrorDetails(err))
		result.setStatus(status)
		result.finish(packetCapture)
		return result, packetCapture.fixture("simulator-esme-peer-002", "peer-002", "INT-01-PEER-002-001")
	}
	defer srv.Close()

	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- srv.Serve(ctx)
	}()
	result.addEvent("listen", StatusPass, map[string]string{"transport": "tcp", "tls": "false", "max_sessions": "1"})
	fmt.Fprintln(output, "READY: go-smpp SMSC listening on", cfg.ESMEListenAddress, "; ESME simulator may now connect.")
	fmt.Fprintln(output, "Start or restart the ESME simulator now. Do not issue submit_sm yet; wait until the bind and delivery handshake completes and the OBSERVE prompt appears.")

	peerCtx, cancelPeer := context.WithTimeout(ctx, cfg.PeerWait)
	defer cancelPeer()
	var attempt bindAttempt
	select {
	case attempt = <-bindAttempts:
	case serveErr := <-serveErrors:
		result.addEvent("peer_bind", StatusBlocked, map[string]string{"reason": "server_stopped_before_bind", "error_type": fmt.Sprintf("%T", serveErr)})
		result.setStatus(StatusBlocked)
		result.finish(packetCapture)
		return result, packetCapture.fixture("simulator-esme-peer-002", "peer-002", "INT-01-PEER-002-001")
	case <-peerCtx.Done():
		result.addEvent("peer_bind", StatusBlocked, map[string]string{"reason": "no_esme_bind_received", "wait": cfg.PeerWait.String()})
		result.setStatus(StatusBlocked)
		result.finish(packetCapture)
		return result, packetCapture.fixture("simulator-esme-peer-002", "peer-002", "INT-01-PEER-002-001")
	}

	if attempt.status != protocol.StatusOK {
		result.addEvent("bind_transceiver", StatusFail, map[string]string{
			"command_status": fmt.Sprintf("0x%08x", uint32(attempt.status)),
			"reason":         attempt.reason,
			"bind_mode_code": fmt.Sprint(attempt.mode),
		})
		result.setStatus(StatusFail)
		result.finish(packetCapture)
		return result, packetCapture.fixture("simulator-esme-peer-002", "peer-002", "INT-01-PEER-002-001")
	}
	if err := waitForBindResponse(peerCtx, attempt.sess, bindResponses); err != nil {
		details := safeErrorDetails(err)
		details["authenticator_result"] = "accepted"
		details["bind_mode"] = attempt.mode.String()
		if closeErr := attempt.sess.Err(); closeErr != nil {
			details["session_close_cause_type"] = fmt.Sprintf("%T", closeErr)
		}
		result.addEvent("bind_transceiver", StatusFail, details)
		result.setStatus(StatusFail)
		result.finish(packetCapture)
		return result, packetCapture.fixture("simulator-esme-peer-002", "peer-002", "INT-01-PEER-002-001")
	}
	result.addEvent("bind_transceiver", StatusPass, map[string]string{"bind_type": "TRX", "interface_version": "3.4"})

	linkCtx, cancelLink := context.WithTimeout(ctx, cfg.RequestTimeout)
	err = attempt.sess.EnquireLink(linkCtx)
	cancelLink()
	if err != nil {
		result.addEvent("enquire_link", StatusFail, safeErrorDetails(err))
		result.setStatus(StatusFail)
	} else {
		result.addEvent("enquire_link", StatusPass, nil)
	}

	dlrText := []byte("id:interop-001 sub:001 dlvrd:001 submit date:2611071200 done date:2611071201 stat:DELIVRD err:000 text:go-smpp interop probe")
	deliverCtx, cancelDeliver := context.WithTimeout(ctx, cfg.RequestTimeout)
	_, deliverErr := srv.DeliverSM(deliverCtx, attempt.sess, protocol.DeliverSM{
		SourceAddrTON:   protocol.TONUnknown,
		SourceAddrNPI:   protocol.NPIUnknown,
		SourceAddr:      []byte(cfg.SourceAddress),
		DestAddrTON:     protocol.TONUnknown,
		DestAddrNPI:     protocol.NPIUnknown,
		DestinationAddr: []byte(cfg.DestinationAddress),
		ESMClass:        0x04,
		DataCoding:      protocol.DataCodingSMSCDefault,
		ShortMessage:    dlrText,
	})
	cancelDeliver()
	if deliverErr != nil {
		var responseError *session.ResponseError
		if errors.As(deliverErr, &responseError) {
			result.addEvent("deliver_sm_resp", StatusPass, map[string]string{
				"command_status":   fmt.Sprintf("0x%08x", uint32(responseError.Status)),
				"message_accepted": "false",
			})
		} else {
			result.addEvent("deliver_sm", StatusFail, safeErrorDetails(deliverErr))
			result.setStatus(StatusFail)
		}
	} else {
		result.addEvent("deliver_sm_resp", StatusPass, map[string]string{
			"command_status":   "0x00000000",
			"message_accepted": "true",
		})
	}

	fmt.Fprintf(output, "OBSERVE: use the ESME simulator CLI to send exactly one submit_sm now (window=%s).\n", cfg.SubmitWait)
	submitTimer := time.NewTimer(cfg.SubmitWait)
	select {
	case observation := <-submits:
		result.addEvent("submit_sm_from_esme", StatusPass, observation.details)
	case <-submitTimer.C:
		result.addEvent("submit_sm_from_esme", StatusNotSeen, map[string]string{"observation_window": cfg.SubmitWait.String()})
	case <-ctx.Done():
		result.addEvent("submit_sm_from_esme", StatusNotSeen, map[string]string{"reason": "run_context_cancelled"})
	}
	if !submitTimer.Stop() {
		select {
		case <-submitTimer.C:
		default:
		}
	}

	unbindCtx, cancelUnbind := context.WithTimeout(ctx, cfg.RequestTimeout)
	unbindErr := attempt.sess.Unbind(unbindCtx)
	cancelUnbind()
	if unbindErr != nil {
		result.addEvent("unbind", StatusFail, unbindEventDetails(unbindErr, packetCapture))
		result.setStatus(StatusFail)
	} else {
		result.addEvent("unbind", StatusPass, unbindEventDetails(nil, packetCapture))
	}
	_ = attempt.sess.Close()
	attempt.sess.Wait()

	result.finish(packetCapture)
	return result, packetCapture.fixture("simulator-esme-peer-002", "peer-002", "INT-01-PEER-002-001")
}

func waitForBindResponse(ctx context.Context, sess *session.Session, responses <-chan protocol.CommandStatus) error {
	// Prefer an already-observed successful bind response even if the peer
	// closes immediately afterward. Polling BindMode can miss the short-lived
	// Bound_TRX state when a peer closes between two ticker ticks.
	select {
	case status := <-responses:
		return bindResponseStatusError(status)
	default:
	}

	select {
	case status := <-responses:
		return bindResponseStatusError(status)
	case <-sess.Done():
		// The response event is emitted after the response has been written.
		// Check once more before treating a simultaneous close as a failed bind.
		select {
		case status := <-responses:
			return bindResponseStatusError(status)
		default:
		}
		if cause := sess.Err(); cause != nil {
			return fmt.Errorf("session closed before bind_transceiver_resp was sent: %w", cause)
		}
		return errors.New("session closed before bind_transceiver_resp was sent")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func bindResponseStatusError(status protocol.CommandStatus) error {
	if status.OK() {
		return nil
	}
	return &session.ResponseError{Command: protocol.CommandBindTransceiverResp, Status: status}
}

func observeDelivery(deliver protocol.DeliverSM) map[string]string {
	details := map[string]string{
		"esm_class":            fmt.Sprintf("0x%02x", deliver.ESMClass),
		"data_coding":          fmt.Sprintf("0x%02x", uint8(deliver.DataCoding)),
		"short_message_octets": fmt.Sprint(len(deliver.ShortMessage)),
		"optional_tlv_count":   fmt.Sprint(len(deliver.Optional)),
	}
	receipt, err := message.ParseDeliveryReceipt(deliver)
	details["receipt_message_id_conflict"] = fmt.Sprintf("%t", receipt.MessageIDConflict)
	details["receipt_state_conflict"] = fmt.Sprintf("%t", receipt.StateConflict)
	switch {
	case err == nil:
		details["receipt_parse"] = "valid"
		details["receipt_message_id_present"] = fmt.Sprintf("%t", receipt.MessageID != "")
		details["receipt_text_status_present"] = fmt.Sprintf("%t", receipt.TextStatus != "")
		details["receipt_state_present"] = fmt.Sprintf("%t", receipt.HasState)
		details["receipt_message_id_source"] = string(receipt.MessageIDSource)
		details["receipt_state_source"] = string(receipt.StateSource)
	case errors.Is(err, message.ErrNotDeliveryReceipt):
		details["receipt_parse"] = "not_a_receipt"
	case errors.Is(err, message.ErrUnsupportedDeliveryReceiptDataCoding):
		details["receipt_parse"] = "unsupported_data_coding"
	default:
		details["receipt_parse"] = "invalid_or_ambiguous"
	}
	return details
}

func observeSubmit(submit protocol.SubmitSM) map[string]string {
	return map[string]string{
		"esm_class":            fmt.Sprintf("0x%02x", submit.ESMClass),
		"data_coding":          fmt.Sprintf("0x%02x", uint8(submit.DataCoding)),
		"registered_delivery":  fmt.Sprintf("0x%02x", submit.RegisteredDelivery),
		"short_message_octets": fmt.Sprint(len(submit.ShortMessage)),
		"optional_tlv_count":   fmt.Sprint(len(submit.Optional)),
	}
}

func safeErrorDetails(err error) map[string]string {
	if err == nil {
		return nil
	}
	details := map[string]string{"error_type": fmt.Sprintf("%T", err)}
	if errors.Is(err, context.DeadlineExceeded) {
		details["reason"] = "deadline_exceeded"
	}
	if errors.Is(err, context.Canceled) {
		details["reason"] = "context_canceled"
	}
	if errors.Is(err, io.EOF) {
		details["reason"] = "peer_closed_connection"
	}
	if errors.Is(err, syscall.ECONNRESET) {
		details["reason"] = "peer_reset_connection"
	}
	if errors.Is(err, net.ErrClosed) {
		details["reason"] = "connection_closed"
	}
	if errors.Is(err, session.ErrSessionClosed) {
		details["reason"] = "session_closed"
	}
	var fatal *protocol.FatalError
	if errors.As(err, &fatal) {
		details["reason"] = "fatal_protocol_error"
		details["fatal_kind"] = fatal.Kind.String()
		details["command_id"] = fmt.Sprintf("0x%08x", uint32(fatal.Command))
		details["sequence_number"] = fmt.Sprint(uint32(fatal.Sequence))
	}
	if cause := errors.Unwrap(err); cause != nil {
		details["cause_type"] = fmt.Sprintf("%T", cause)
	}
	var opError *net.OpError
	if errors.As(err, &opError) {
		details["network_operation"] = opError.Op
	}
	var timeoutError *session.TimeoutError
	if errors.As(err, &timeoutError) {
		details["timeout_kind"] = timeoutError.Kind.String()
		details["command_id"] = fmt.Sprintf("0x%08x", uint32(timeoutError.Command))
		details["sequence_number"] = fmt.Sprint(uint32(timeoutError.Sequence))
		details["timeout"] = timeoutError.After.String()
	}
	var responseError *session.ResponseError
	if errors.As(err, &responseError) {
		details["command_status"] = fmt.Sprintf("0x%08x", uint32(responseError.Status))
	}
	return details
}

func (c *capture) hasCommand(direction string, command protocol.CommandID) bool {
	if c == nil {
		return false
	}
	frames, _ := c.snapshot()
	for _, frame := range frames {
		if frame.Direction != direction {
			continue
		}
		raw, err := hex.DecodeString(frame.Hex)
		if err != nil {
			continue
		}
		header, err := codec.DecodeHeader(raw)
		if err == nil && header.CommandID == command {
			return true
		}
	}
	return false
}

func unbindEventDetails(err error, packetCapture *capture) map[string]string {
	requestDirection, responseDirection := "esme_to_smsc", "smsc_to_esme"
	if packetCapture != nil && packetCapture.localRole == "smsc" {
		requestDirection, responseDirection = responseDirection, requestDirection
	}
	details := map[string]string{
		"request_sent":      fmt.Sprint(packetCapture.hasCommand(requestDirection, protocol.CommandUnbind)),
		"response_observed": fmt.Sprint(packetCapture.hasCommand(responseDirection, protocol.CommandUnbindResp)),
	}
	for key, value := range safeErrorDetails(err) {
		details[key] = value
	}
	if details["request_sent"] == "true" && details["response_observed"] == "false" {
		details["peer_response"] = "not_observed"
	}
	return details
}

func isNetworkUnavailable(err error) bool {
	var opError *net.OpError
	return errors.Is(err, context.DeadlineExceeded) || errors.As(err, &opError)
}

func newCapture(localRole string) (*capture, error) {
	registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
	if err != nil {
		return nil, err
	}
	return &capture{localRole: localRole, registry: registry}, nil
}

func (c *capture) tracePacket(trace session.PacketTrace) {
	if c == nil || len(trace.RawPDU) == 0 {
		if c != nil {
			c.dropped.Add(1)
		}
		return
	}
	frame, ok := sanitizeRawPDU(trace.RawPDU, c.registry)
	if !ok {
		c.dropped.Add(1)
		return
	}
	switch {
	case c.localRole == "esme" && trace.Direction == session.PacketOutbound:
		frame.Direction = "esme_to_smsc"
	case c.localRole == "esme" && trace.Direction == session.PacketInbound:
		frame.Direction = "smsc_to_esme"
	case c.localRole == "smsc" && trace.Direction == session.PacketInbound:
		frame.Direction = "esme_to_smsc"
	case c.localRole == "smsc" && trace.Direction == session.PacketOutbound:
		frame.Direction = "smsc_to_esme"
	default:
		c.dropped.Add(1)
		return
	}
	c.mu.Lock()
	c.frames = append(c.frames, capturedFrame{at: trace.At, frame: frame})
	c.mu.Unlock()
}

func (c *capture) snapshot() ([]FixtureFrame, uint64) {
	c.mu.Lock()
	frames := append([]capturedFrame(nil), c.frames...)
	c.mu.Unlock()
	sort.SliceStable(frames, func(i, j int) bool { return frames[i].at.Before(frames[j].at) })
	result := make([]FixtureFrame, 0, len(frames))
	for _, frame := range frames {
		result = append(result, frame.frame)
	}
	return result, c.dropped.Load()
}

func (c *capture) fixture(id, peerAlias, evidenceRef string) *Fixture {
	if c == nil {
		return nil
	}
	frames, _ := c.snapshot()
	if len(frames) == 0 {
		return nil
	}
	return &Fixture{
		ID:              id,
		Source:          "simulator-peer",
		PeerAlias:       peerAlias,
		ProtocolVersion: "3.4",
		CaptureMethod:   "wire-capture-sanitized",
		CapturedAt:      time.Now().UTC().Format(time.RFC3339),
		EvidenceRef:     evidenceRef,
		Sanitization: Sanitization{
			Reviewed:                false,
			Policy:                  "peer-aware-allowlist-v1",
			BindFramesExcluded:      true,
			SensitiveValuesReplaced: true,
			UnknownTLVs:             "removed",
		},
		Frames: frames,
	}
}

func sanitizeRawPDU(raw []byte, registry *codec.Registry) (FixtureFrame, bool) {
	header, err := codec.DecodeHeader(raw)
	if err != nil || isCredentialCommand(header.CommandID) {
		return FixtureFrame{}, false
	}
	decoded, err := codec.DecodePDU(raw, registry)
	if err != nil {
		return FixtureFrame{}, false
	}

	var body any
	switch decoded.Header.CommandID {
	case protocol.CommandEnquireLink, protocol.CommandEnquireLinkResp,
		protocol.CommandUnbind, protocol.CommandUnbindResp:
		body = protocol.EmptyBody{}
	case protocol.CommandSubmitSM:
		original, ok := decoded.Body.(protocol.SubmitSM)
		if !ok {
			return FixtureFrame{}, false
		}
		body = sanitizeSubmitSM(original)
	case protocol.CommandSubmitSMResp:
		switch original := decoded.Body.(type) {
		case protocol.SubmitSMResp:
			body = protocol.SubmitSMResp{MessageID: []byte("fixture-001")}
		case protocol.EmptyBody:
			body = original
		default:
			return FixtureFrame{}, false
		}
	case protocol.CommandDeliverSM:
		original, ok := decoded.Body.(protocol.DeliverSM)
		if !ok {
			return FixtureFrame{}, false
		}
		receipt, receiptErr := message.ParseDeliveryReceipt(original)
		if errors.Is(receiptErr, message.ErrInvalidDeliveryReceipt) ||
			(receiptErr == nil && (receipt.MessageIDConflict || receipt.StateConflict)) {
			return FixtureFrame{}, false
		}
		body = sanitizeDeliverSM(original)
	case protocol.CommandDeliverSMResp:
		switch original := decoded.Body.(type) {
		case protocol.DeliverSMResp:
			body = protocol.DeliverSMResp{}
		case protocol.EmptyBody:
			body = original
		default:
			return FixtureFrame{}, false
		}
	default:
		return FixtureFrame{}, false
	}

	encoded, err := codec.EncodePDU(nil, decoded.Header, body, registry)
	if err != nil {
		return FixtureFrame{}, false
	}
	expected := FixtureExpected{
		Outcome:        "accept",
		CommandID:      fmt.Sprintf("0x%08x", uint32(decoded.Header.CommandID)),
		CommandStatus:  fmt.Sprintf("0x%08x", uint32(decoded.Header.CommandStatus)),
		SequenceNumber: uint32(decoded.Header.SequenceNumber),
		BodyType:       fmt.Sprintf("%T", body),
		Reencode:       "decode-only",
	}
	if deliver, ok := body.(protocol.DeliverSM); ok {
		receipt, receiptErr := message.ParseDeliveryReceipt(deliver)
		if receiptErr == nil && receipt.MessageID != "" && receipt.TextStatus != "" {
			expected.Receipt = &ReceiptExpected{MessageID: receipt.MessageID, TextStatus: receipt.TextStatus}
		}
	}
	return FixtureFrame{Hex: hex.EncodeToString(encoded), Expect: expected}, true
}

func isCredentialCommand(command protocol.CommandID) bool {
	switch command {
	case protocol.CommandBindReceiver, protocol.CommandBindReceiverResp,
		protocol.CommandBindTransmitter, protocol.CommandBindTransmitterResp,
		protocol.CommandBindTransceiver, protocol.CommandBindTransceiverResp,
		protocol.CommandOutbind:
		return true
	default:
		return false
	}
}

func sanitizeSubmitSM(in protocol.SubmitSM) protocol.SubmitSM {
	return protocol.SubmitSM{
		SourceAddrTON:        in.SourceAddrTON,
		SourceAddrNPI:        in.SourceAddrNPI,
		SourceAddr:           []byte("1000"),
		DestAddrTON:          in.DestAddrTON,
		DestAddrNPI:          in.DestAddrNPI,
		DestinationAddr:      []byte("2000"),
		ESMClass:             in.ESMClass,
		ProtocolID:           in.ProtocolID,
		PriorityFlag:         in.PriorityFlag,
		RegisteredDelivery:   in.RegisteredDelivery,
		ReplaceIfPresentFlag: in.ReplaceIfPresentFlag,
		DataCoding:           in.DataCoding,
		SMDefaultMsgID:       in.SMDefaultMsgID,
	}
}

func sanitizeDeliverSM(in protocol.DeliverSM) protocol.DeliverSM {
	out := protocol.DeliverSM{
		SourceAddrTON:        in.SourceAddrTON,
		SourceAddrNPI:        in.SourceAddrNPI,
		SourceAddr:           []byte("1000"),
		DestAddrTON:          in.DestAddrTON,
		DestAddrNPI:          in.DestAddrNPI,
		DestinationAddr:      []byte("2000"),
		ESMClass:             in.ESMClass,
		ProtocolID:           in.ProtocolID,
		PriorityFlag:         in.PriorityFlag,
		RegisteredDelivery:   in.RegisteredDelivery,
		ReplaceIfPresentFlag: in.ReplaceIfPresentFlag,
		DataCoding:           in.DataCoding,
		SMDefaultMsgID:       in.SMDefaultMsgID,
	}

	receipt, receiptErr := message.ParseDeliveryReceipt(in)
	var replacementText []byte
	if receiptErr == nil && receipt.MessageID != "" && receipt.TextStatus != "" {
		replacementText = encodeSafeReceipt(receipt.TextStatus, in.DataCoding)
	}
	if len(replacementText) > 0 && len(in.ShortMessage) > 0 {
		out.ShortMessage = replacementText
	}
	out.Optional = sanitizeReceiptTLVs(in.Optional, replacementText, len(in.ShortMessage) == 0)
	return out
}

func sanitizeReceiptTLVs(items []protocol.OptionalParameter, replacementText []byte, usePayload bool) []protocol.OptionalParameter {
	result := make([]protocol.OptionalParameter, 0, len(items))
	for _, item := range items {
		switch item.Tag {
		case protocol.TLVTagReceiptedMessageID:
			result = append(result, protocol.OptionalParameter{Tag: item.Tag, Value: append([]byte("fixture-001"), 0)})
		case protocol.TLVTagMessageState:
			if len(item.Value) == 1 {
				result = append(result, protocol.OptionalParameter{Tag: item.Tag, Value: []byte{item.Value[0]}})
			}
		case protocol.TLVTagNetworkErrorCode:
			if len(item.Value) == 3 {
				result = append(result, protocol.OptionalParameter{Tag: item.Tag, Value: append([]byte(nil), item.Value...)})
			}
		case protocol.TLVTagDeliveryFailureReason:
			if len(item.Value) == 1 {
				result = append(result, protocol.OptionalParameter{Tag: item.Tag, Value: []byte{item.Value[0]}})
			}
		case protocol.TLVTagMessagePayload:
			if usePayload && len(replacementText) > 0 {
				result = append(result, protocol.OptionalParameter{Tag: item.Tag, Value: append([]byte(nil), replacementText...)})
			}
		}
	}
	return result
}

func encodeSafeReceipt(status string, coding protocol.DataCoding) []byte {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "ENROUTE", "DELIVRD", "EXPIRED", "DELETED", "UNDELIV", "ACCEPTD", "UNKNOWN", "REJECTD":
		status = strings.ToUpper(strings.TrimSpace(status))
	default:
		status = "OTHER"
	}
	text := fmt.Sprintf("id:fixture-001 sub:001 dlvrd:001 submit date:2611071200 done date:2611071201 stat:%s err:000 text:fixture", status)
	switch coding {
	case protocol.DataCodingSMSCDefault, protocol.DataCodingIA5, protocol.DataCodingLatin1:
		return []byte(text)
	case protocol.DataCodingUCS2:
		encoded := make([]byte, len(text)*2)
		for index := range text {
			encoded[index*2+1] = text[index]
		}
		return encoded
	default:
		return nil
	}
}

func (r *PhaseResult) addEvent(name, status string, details map[string]string) {
	r.Events = append(r.Events, Event{Name: name, Status: status, Details: details})
}

func (r *PhaseResult) setStatus(status string) {
	if status == StatusFail {
		r.Status = StatusFail
		return
	}
	if status == StatusBlocked && r.Status != StatusFail {
		r.Status = StatusBlocked
	}
}

func (r *PhaseResult) finish(packetCapture *capture) {
	r.CompletedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if packetCapture != nil {
		frames, dropped := packetCapture.snapshot()
		r.FramesCaptured = len(frames)
		r.FramesDropped = dropped
	}
}
