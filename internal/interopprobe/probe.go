//go:build interop

package interopprobe

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/majiddarvishan/go-smpp/client"
	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/message"
	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/server"
	"github.com/majiddarvishan/go-smpp/session"
	"github.com/majiddarvishan/go-smpp/transport"
)

const (
	StatusPass        = "PASS"
	StatusFail        = "FAIL"
	StatusBlocked     = "BLOCKED"
	StatusNotSeen     = "NOT_OBSERVED"
	StatusUnsupported = "UNSUPPORTED"
	StatusObserved    = "OBSERVED"
)

// interopMessageID is intentionally decimal-only for the peer interoperability
// fixture. SMPP still encodes message_id as a NUL-terminated C-Octet String.
const interopMessageID = "12345678901234567890"

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

	RequestTimeout    time.Duration
	PeerWait          time.Duration
	DLRWait           time.Duration
	SubmitWait        time.Duration
	AttemptESMEUnbind bool
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
	sess         *session.Session
	connectionID uint64
	details      map[string]string
}

type bindAttempt struct {
	sess          *session.Session
	connectionID  uint64
	bindResponses <-chan protocol.CommandStatus
	mode          session.BindMode
	status        protocol.CommandStatus
	reason        string
}

type tcpTrace struct {
	mu       sync.Mutex
	nextID   uint64
	nextSeq  uint64
	byRemote map[string]*trackedConn
	events   []tcpTraceEvent
	shutdown bool
}

type tcpTraceEvent struct {
	order                  uint64
	at                     string
	connectionID           uint64
	name                   string
	direction              string
	commandID              uint32
	commandStatus          uint32
	sequence               uint32
	length                 int
	errorType              string
	networkError           string
	priorIOError           string
	probeShutdown          bool
	attemptedCommandID     uint32
	attemptedCommandStatus uint32
	attemptedSequence      uint32
	attemptedLength        int
}

type trackedConn struct {
	net.Conn
	trace         *tcpTrace
	id            uint64
	mu            sync.Mutex
	readBuffer    []byte
	writeBuffer   []byte
	bindResponses chan protocol.CommandStatus
	closeOnce     sync.Once
	closeErr      error
	lastIOError   string
}

type tcpTraceListener struct {
	net.Listener
	trace *tcpTrace
}

func newTCPTrace() *tcpTrace {
	return &tcpTrace{byRemote: make(map[string]*trackedConn)}
}

func (t *tcpTrace) track(conn net.Conn) *trackedConn {
	if t == nil || conn == nil {
		return nil
	}
	t.mu.Lock()
	t.nextID++
	tracked := &trackedConn{
		Conn:          conn,
		trace:         t,
		id:            t.nextID,
		bindResponses: make(chan protocol.CommandStatus, 1),
	}
	if remote := conn.RemoteAddr(); remote != nil {
		t.byRemote[remote.String()] = tracked
	}
	t.mu.Unlock()
	t.record(tcpTraceEvent{connectionID: tracked.id, name: "tcp_accepted"})
	return tracked
}

func (l *tcpTraceListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return l.trace.track(conn), nil
}

func (t *tcpTrace) connectionFor(sess *session.Session) *trackedConn {
	if t == nil || sess == nil || sess.RemoteAddr() == nil {
		return nil
	}
	t.mu.Lock()
	tracked := t.byRemote[sess.RemoteAddr().String()]
	t.mu.Unlock()
	return tracked
}

func (t *tcpTrace) record(event tcpTraceEvent) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.nextSeq++
	event.order = t.nextSeq
	event.at = time.Now().UTC().Format(time.RFC3339Nano)
	t.events = append(t.events, event)
	t.mu.Unlock()
}

func (t *tcpTrace) markShutdown() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.shutdown = true
	t.mu.Unlock()
}

func (t *tcpTrace) shutdownStarted() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	started := t.shutdown
	t.mu.Unlock()
	return started
}

func (t *tcpTrace) reportEvents() []Event {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	items := append([]tcpTraceEvent(nil), t.events...)
	t.mu.Unlock()
	sort.Slice(items, func(i, j int) bool { return items[i].order < items[j].order })
	result := make([]Event, 0, len(items))
	for _, item := range items {
		details := map[string]string{
			"order":         fmt.Sprint(item.order),
			"at":            item.at,
			"connection_id": fmt.Sprint(item.connectionID),
		}
		if item.direction != "" {
			details["direction"] = item.direction
		}
		if item.commandID != 0 {
			details["command_id"] = fmt.Sprintf("0x%08x", item.commandID)
			details["command_status"] = fmt.Sprintf("0x%08x", item.commandStatus)
			details["sequence_number"] = fmt.Sprint(item.sequence)
			details["pdu_length"] = fmt.Sprint(item.length)
		}
		if item.errorType != "" {
			details["error_type"] = item.errorType
		}
		if item.networkError != "" {
			details["network_error"] = item.networkError
		}
		if item.priorIOError != "" {
			details["prior_io_error"] = item.priorIOError
		}
		if item.probeShutdown {
			details["probe_shutdown_started"] = "true"
		}
		if item.attemptedCommandID != 0 {
			details["attempted_command_id"] = fmt.Sprintf("0x%08x", item.attemptedCommandID)
			details["attempted_command_status"] = fmt.Sprintf("0x%08x", item.attemptedCommandStatus)
			details["attempted_sequence_number"] = fmt.Sprint(item.attemptedSequence)
			details["attempted_pdu_length"] = fmt.Sprint(item.attemptedLength)
		}
		result = append(result, Event{Name: item.name, Status: StatusObserved, Details: details})
	}
	return result
}

func (c *trackedConn) observeBytes(direction string, data []byte, allowBindResponse bool) {
	if c == nil || c.trace == nil || len(data) == 0 {
		return
	}
	c.mu.Lock()
	buffer := &c.readBuffer
	name := "tcp_pdu_read"
	if direction == "smsc_to_peer" {
		buffer = &c.writeBuffer
		name = "tcp_pdu_written"
	}
	*buffer = append(*buffer, data...)
	for len(*buffer) >= 4 {
		length := int(binary.BigEndian.Uint32((*buffer)[:4]))
		if length < 16 || length > 1<<20 {
			c.trace.record(tcpTraceEvent{connectionID: c.id, name: "tcp_invalid_pdu_length", direction: direction})
			*buffer = nil
			break
		}
		if len(*buffer) < length {
			break
		}
		frame := (*buffer)[:length]
		commandID := binary.BigEndian.Uint32(frame[4:8])
		commandStatus := binary.BigEndian.Uint32(frame[8:12])
		sequence := binary.BigEndian.Uint32(frame[12:16])
		c.trace.record(tcpTraceEvent{
			connectionID: c.id, name: name, direction: direction,
			commandID: commandID, commandStatus: commandStatus,
			sequence: sequence, length: length,
		})
		if allowBindResponse && direction == "smsc_to_peer" && commandID == uint32(protocol.CommandBindTransceiverResp) {
			select {
			case c.bindResponses <- protocol.CommandStatus(commandStatus):
			default:
			}
		}
		*buffer = (*buffer)[length:]
	}
	c.mu.Unlock()
}

func (c *trackedConn) Read(buffer []byte) (int, error) {
	n, err := c.Conn.Read(buffer)
	if n > 0 {
		c.observeBytes("peer_to_smsc", buffer[:n], false)
	}
	if err != nil {
		c.recordIOError("read", err, nil)
	}
	return n, err
}

func (c *trackedConn) Write(buffer []byte) (int, error) {
	n, err := c.Conn.Write(buffer)
	if n > 0 {
		c.observeBytes("smsc_to_peer", buffer[:n], err == nil)
	}
	if err != nil {
		c.recordIOError("write", err, buffer)
	}
	return n, err
}

func (c *trackedConn) recordIOError(direction string, err error, attempted []byte) {
	details := safeErrorDetails(err)
	networkError := details["network_error"]
	if networkError == "" {
		networkError = details["reason"]
	}
	if networkError == "" {
		networkError = "other"
	}
	c.mu.Lock()
	c.lastIOError = direction + ":" + networkError
	c.mu.Unlock()

	event := tcpTraceEvent{
		connectionID: c.id, name: "tcp_" + direction + "_error",
		direction: direction, errorType: fmt.Sprintf("%T", err), networkError: networkError,
	}
	// A failed Write is not a transmitted PDU. Record only its attempted SMPP
	// header when the complete header and declared frame length are available.
	// Never retain the payload or any raw PDU bytes.
	if direction == "write" && len(attempted) >= 16 {
		length := int(binary.BigEndian.Uint32(attempted[:4]))
		if length >= 16 && length <= len(attempted) {
			event.attemptedCommandID = binary.BigEndian.Uint32(attempted[4:8])
			event.attemptedCommandStatus = binary.BigEndian.Uint32(attempted[8:12])
			event.attemptedSequence = binary.BigEndian.Uint32(attempted[12:16])
			event.attemptedLength = length
		}
	}
	c.trace.record(event)
}

func (c *trackedConn) Close() error {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		prior := c.lastIOError
		c.mu.Unlock()
		c.trace.record(tcpTraceEvent{connectionID: c.id, name: "tcp_local_close", priorIOError: prior, probeShutdown: c.trace.shutdownStarted()})
		c.closeErr = c.Conn.Close()
	})
	return c.closeErr
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

	connectionTrace := newTCPTrace()
	bindAttempts := make(chan bindAttempt, 4)
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
		tracked := connectionTrace.connectionFor(sess)
		attempt := bindAttempt{sess: sess, mode: mode, status: status, reason: reason}
		if tracked != nil {
			attempt.connectionID = tracked.id
			attempt.bindResponses = tracked.bindResponses
		}
		select {
		case bindAttempts <- attempt:
		default:
		}
		return server.BindResult{Status: status, SystemID: []byte("go-smpp-interop")}, nil
	})
	submitHandler := server.SubmitHandlerFunc(func(_ context.Context, sess *session.Session, request protocol.SubmitSM) (server.SubmitResult, error) {
		tracked := connectionTrace.connectionFor(sess)
		observation := submitObservation{sess: sess, details: observeSubmit(request)}
		if tracked != nil {
			observation.connectionID = tracked.id
		}
		select {
		case submits <- observation:
		default:
		}
		return server.SubmitResult{Status: protocol.StatusOK, MessageID: []byte(interopMessageID)}, nil
	})

	listener, err := transport.ListenTCP(ctx, "tcp", cfg.ESMEListenAddress, nil)
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
	srv := server.NewWithConfig(&tcpTraceListener{Listener: listener, trace: connectionTrace}, server.Config{
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
			PacketTracer:        session.PacketTracerFunc(packetCapture.tracePacket),
			TraceRawPDU:         true,
		},
	})
	defer srv.Close()

	serveErrors := make(chan error, 1)
	serveDone := make(chan struct{})
	closeAndTrace := func() {
		connectionTrace.markShutdown()
		closeProbeServer(srv)
		<-serveDone // Ensure the accept loop cannot add another tracked connection after the final trace snapshot.
		result.Events = append(result.Events, connectionTrace.reportEvents()...)
	}
	go func() {
		defer close(serveDone)
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
		closeAndTrace()
		result.finish(packetCapture)
		return result, packetCapture.fixture("simulator-esme-peer-002", "peer-002", "INT-01-PEER-002-001")
	case <-peerCtx.Done():
		result.addEvent("peer_bind", StatusBlocked, map[string]string{"reason": "no_esme_bind_received", "wait": cfg.PeerWait.String()})
		result.setStatus(StatusBlocked)
		closeAndTrace()
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
		closeAndTrace()
		result.finish(packetCapture)
		return result, packetCapture.fixture("simulator-esme-peer-002", "peer-002", "INT-01-PEER-002-001")
	}
	if attempt.bindResponses == nil {
		result.addEvent("bind_transceiver", StatusFail, map[string]string{"reason": "accepted_connection_trace_missing"})
		result.setStatus(StatusFail)
		closeAndTrace()
		result.finish(packetCapture)
		return result, nil
	}
	if err := waitForBindResponse(peerCtx, attempt.sess, attempt.bindResponses); err != nil {
		details := safeErrorDetails(err)
		details["authenticator_result"] = "accepted"
		details["bind_mode"] = attempt.mode.String()
		if attempt.connectionID > 0 {
			details["connection_id"] = fmt.Sprint(attempt.connectionID)
		}
		if closeErr := attempt.sess.Err(); closeErr != nil {
			details["session_close_cause_type"] = fmt.Sprintf("%T", closeErr)
		}
		result.addEvent("bind_transceiver", StatusFail, details)
		result.setStatus(StatusFail)
		closeAndTrace()
		result.finish(packetCapture)
		return result, packetCapture.fixture("simulator-esme-peer-002", "peer-002", "INT-01-PEER-002-001")
	}
	bindDetails := map[string]string{"bind_type": "TRX", "interface_version": "3.4"}
	if attempt.connectionID > 0 {
		bindDetails["connection_id"] = fmt.Sprint(attempt.connectionID)
	}
	result.addEvent("bind_transceiver", StatusPass, bindDetails)

	linkCtx, cancelLink := context.WithTimeout(ctx, cfg.RequestTimeout)
	err = attempt.sess.EnquireLink(linkCtx)
	cancelLink()
	if err != nil {
		details := safeErrorDetails(err)
		if attempt.connectionID > 0 {
			details["connection_id"] = fmt.Sprint(attempt.connectionID)
		}
		result.addEvent("enquire_link", StatusFail, details)
		result.setStatus(StatusFail)
	} else {
		result.addEvent("enquire_link", StatusPass, nil)
	}
	health := sessionHealthDetails(srv, attempt.sess)
	if attempt.connectionID > 0 {
		health["connection_id"] = fmt.Sprint(attempt.connectionID)
	}
	healthStatus := StatusPass
	if health["session_closed"] == "true" || health["server_owns_session"] != "true" {
		healthStatus = StatusFail
		result.setStatus(StatusFail)
	}
	result.addEvent("session_health_after_enquire_link", healthStatus, health)

	dlrText := []byte(fmt.Sprintf("id:%s sub:001 dlvrd:001 submit date:2611071200 done date:2611071201 stat:DELIVRD err:000 text:go-smpp interop probe", interopMessageID))
	if sessionIsDone(attempt.sess) || health["server_owns_session"] != "true" {
		details := sessionHealthDetails(srv, attempt.sess)
		if attempt.connectionID > 0 {
			details["connection_id"] = fmt.Sprint(attempt.connectionID)
		}
		details["reason"] = "session_unavailable_before_deliver_sm"
		result.addEvent("deliver_sm", StatusBlocked, details)
		result.setStatus(StatusBlocked)
	} else {
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
	}

	fmt.Fprintf(output, "OBSERVE: use the ESME simulator CLI to send exactly one submit_sm now (window=%s).\n", cfg.SubmitWait)
	submitTimer := time.NewTimer(cfg.SubmitWait)
	select {
	case observation := <-submits:
		event := submitObservationEvent(observation, attempt.sess)
		result.Events = append(result.Events, event)
		if event.Status == StatusFail {
			result.setStatus(StatusFail)
		}
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

	unbindEvent := esmeUnbindEvent(cfg.AttemptESMEUnbind, func() error {
		unbindCtx, cancelUnbind := context.WithTimeout(ctx, cfg.RequestTimeout)
		defer cancelUnbind()
		return attempt.sess.Unbind(unbindCtx)
	}, packetCapture)
	result.Events = append(result.Events, unbindEvent)
	if unbindEvent.Status == StatusFail {
		result.setStatus(StatusFail)
	}
	_ = attempt.sess.Close()
	attempt.sess.Wait()
	closeAndTrace()

	result.finish(packetCapture)
	fixture := packetCapture.fixture("simulator-esme-peer-002", "peer-002", "INT-01-PEER-002-001")
	for _, event := range result.Events {
		if event.Name == "submit_sm_from_esme" && event.Details["session_match"] == "false" {
			fixture = nil // Never emit a fixture that mixes multiple TCP sessions.
			break
		}
	}
	return result, fixture
}

func sessionIsDone(sess *session.Session) bool {
	if sess == nil {
		return true
	}
	select {
	case <-sess.Done():
		return true
	default:
		return false
	}
}

func sessionHealthDetails(srv *server.Server, sess *session.Session) map[string]string {
	details := map[string]string{
		"session_closed": "true",
		"session_state":  protocol.StateClosed.String(),
	}
	if sess == nil {
		details["reason"] = "missing_session"
		return details
	}
	details["session_state"] = sess.State().String()
	if !sessionIsDone(sess) {
		details["session_closed"] = "false"
	}
	if srv == nil {
		details["server_owns_session"] = "unknown"
		details["server_active_sessions"] = "unknown"
	} else {
		active := srv.Sessions()
		details["server_active_sessions"] = fmt.Sprint(len(active))
		owned := false
		for _, current := range active {
			if current == sess {
				owned = true
				break
			}
		}
		details["server_owns_session"] = fmt.Sprint(owned)
	}
	if closeErr := sess.Err(); closeErr != nil {
		details["close_cause_type"] = fmt.Sprintf("%T", closeErr)
		for key, value := range safeErrorDetails(closeErr) {
			details["close_cause_"+key] = value
		}
	}
	return details
}

func submitObservationEvent(observation submitObservation, expected *session.Session) Event {
	details := make(map[string]string, len(observation.details)+2)
	for key, value := range observation.details {
		details[key] = value
	}
	if expected == nil || observation.sess != expected {
		details["session_match"] = "false"
		if observation.connectionID > 0 {
			details["connection_id"] = fmt.Sprint(observation.connectionID)
		}
		details["reason"] = "submit_received_on_replacement_session"
		return Event{Name: "submit_sm_from_esme", Status: StatusFail, Details: details}
	}
	details["session_match"] = "true"
	if observation.connectionID > 0 {
		details["connection_id"] = fmt.Sprint(observation.connectionID)
	}
	return Event{Name: "submit_sm_from_esme", Status: StatusPass, Details: details}
}

func closeProbeServer(srv *server.Server) {
	if srv == nil {
		return
	}
	_ = srv.Close()
	// Server.Close closes every active session. Sessions removed by the
	// server's watcher have already been joined; join any still in its snapshot
	// before the report/candidate capture takes its final snapshot.
	for _, sess := range srv.Sessions() {
		sess.Wait()
	}
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
		// Join the RX/TX loops before checking the observer channel. A peer can
		// close immediately after reading bind_transceiver_resp, racing the RX
		// close against the TX loop's post-write observer callback.
		sess.Wait()
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
		details["reason"] = "tcp_reset_received"
		details["network_error"] = "connection_reset"
	}
	if errors.Is(err, syscall.EPIPE) {
		details["network_error"] = "broken_pipe"
	}
	if errors.Is(err, syscall.ECONNABORTED) {
		details["network_error"] = "connection_aborted"
	}
	if errors.Is(err, syscall.ETIMEDOUT) {
		details["network_error"] = "network_timeout"
	}
	if errors.Is(err, net.ErrClosed) {
		details["reason"] = "connection_closed"
		details["network_error"] = "local_connection_closed"
	}
	if errors.Is(err, server.ErrUnknownSession) {
		details["reason"] = "session_not_owned_by_server"
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

func esmeUnbindEvent(attempt bool, unbind func() error, packetCapture *capture) Event {
	if !attempt {
		return Event{
			Name:   "unbind",
			Status: StatusUnsupported,
			Details: map[string]string{
				"reason":             "peer_capability_not_supported",
				"operation":          "not_attempted",
				"operation_kind":     "server_initiated_unbind",
				"initiator_role":     "smsc",
				"peer_role":          "esme",
				"request_direction":  "smsc_to_esme",
				"peer_capability":    "server_initiated_unbind",
				"request_sent":       "false",
				"response_observed":  "false",
			},
		}
	}
	if unbind == nil {
		return Event{
			Name:    "unbind",
			Status:  StatusFail,
			Details: map[string]string{"reason": "unbind_operation_missing"},
		}
	}
	err := unbind()
	if err != nil {
		return Event{Name: "unbind", Status: StatusFail, Details: unbindEventDetails(err, packetCapture)}
	}
	return Event{Name: "unbind", Status: StatusPass, Details: unbindEventDetails(nil, packetCapture)}
}

func unbindEventDetails(err error, packetCapture *capture) map[string]string {
	requestDirection, responseDirection := "esme_to_smsc", "smsc_to_esme"
	if packetCapture != nil && packetCapture.localRole == "smsc" {
		requestDirection, responseDirection = responseDirection, requestDirection
	}
	details := map[string]string{
		"request_sent":      fmt.Sprint(packetCapture.hasCommand(requestDirection, protocol.CommandUnbind)),
		"response_observed": fmt.Sprint(packetCapture.hasCommand(responseDirection, protocol.CommandUnbindResp)),
		"operation_kind":    "unknown",
		"initiator_role":    "unknown",
		"peer_role":         "unknown",
		"request_direction": "unknown",
	}
	if packetCapture != nil && packetCapture.localRole == "smsc" {
		details["operation_kind"] = "server_initiated_unbind"
		details["initiator_role"] = "smsc"
		details["peer_role"] = "esme"
		details["request_direction"] = "smsc_to_esme"
	} else if packetCapture != nil && packetCapture.localRole == "esme" {
		details["operation_kind"] = "esme_initiated_unbind"
		details["initiator_role"] = "esme"
		details["peer_role"] = "smsc"
		details["request_direction"] = "esme_to_smsc"
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
