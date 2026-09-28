package session

import (
	"context"
	"errors"
	"net"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/transport"
)

func TestResponseTimeoutStartsAfterFullDispatch(t *testing.T) {
	clientConn, peerConn := net.Pipe()
	defer peerConn.Close()
	registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	allowSubmitRead := make(chan struct{})
	submitRead := make(chan struct{})
	peerErr := make(chan error, 1)
	go func() {
		bind, err := readOnePDU(peerConn, registry)
		if err != nil {
			peerErr <- err
			return
		}
		frame, err := codec.EncodePDU(nil, codec.ResponseHeader(bind.Header, protocol.CommandBindTransceiverResp, protocol.StatusOK), protocol.BindResponse{SystemID: []byte("smsc")}, registry)
		if err != nil {
			peerErr <- err
			return
		}
		if err := transport.WriteFull(peerConn, frame); err != nil {
			peerErr <- err
			return
		}
		<-allowSubmitRead
		if _, err := readOnePDU(peerConn, registry); err != nil {
			peerErr <- err
			return
		}
		close(submitRead)
		peerErr <- nil
	}()

	sess, err := New(clientConn, Config{
		Role:                RoleESME,
		ResponseTimeout:     40 * time.Millisecond,
		SessionInitTimeout:  time.Second,
		EnquireLinkInterval: -1,
		InactivityTimeout:   -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := sess.BindTransceiver(ctx, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion34}); err != nil {
		t.Fatal(err)
	}

	result := make(chan error, 1)
	go func() {
		_, err := sess.SubmitSM(ctx, protocol.SubmitSM{DestinationAddr: []byte("1")})
		result <- err
	}()
	// The peer is deliberately not reading the submit PDU, so net.Pipe keeps the
	// TX write blocked. The protocol response timeout must not start yet.
	time.Sleep(90 * time.Millisecond)
	select {
	case err := <-result:
		t.Fatalf("request completed before full dispatch: %v", err)
	default:
	}

	close(allowSubmitRead)
	select {
	case <-submitRead:
	case <-time.After(time.Second):
		t.Fatal("peer did not receive submit_sm")
	}
	started := time.Now()
	var timeout *TimeoutError
	select {
	case err := <-result:
		if !errors.As(err, &timeout) || timeout.Kind != TimeoutResponse {
			t.Fatalf("submit error=%T %v", err, err)
		}
	case <-time.After(time.Second):
		t.Fatal("response timeout did not fire")
	}
	if elapsed := time.Since(started); elapsed < 20*time.Millisecond {
		t.Fatalf("response timeout fired too early after dispatch: %s", elapsed)
	}
	if got := sess.Window().InUse; got != 0 {
		t.Fatalf("window in use after timeout=%d", got)
	}
	if got := sess.Metrics().ResponseTimeouts; got != 1 {
		t.Fatalf("response timeout metric=%d want 1", got)
	}
	if err := <-peerErr; err != nil {
		t.Fatal(err)
	}
}

func TestSessionInitTimeout(t *testing.T) {
	sessionConn, peerConn := net.Pipe()
	defer peerConn.Close()
	sess, err := New(sessionConn, Config{
		Role:                RoleSMSC,
		SessionInitTimeout:  40 * time.Millisecond,
		EnquireLinkInterval: -1,
		InactivityTimeout:   -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	select {
	case <-sess.Done():
	case <-time.After(time.Second):
		t.Fatal("session init timeout did not close session")
	}
	var timeout *TimeoutError
	if !errors.As(sess.Err(), &timeout) || timeout.Kind != TimeoutSessionInit {
		t.Fatalf("terminal error=%T %v", sess.Err(), sess.Err())
	}
	if got := sess.Metrics().SessionInitTimeouts; got != 1 {
		t.Fatalf("session init timeout metric=%d want 1", got)
	}
}

// TestWriteTimeoutClosesSessionOnStalledPeer reproduces finding B2: with no
// write deadline anywhere in the library, a peer that accepts the connection
// and then never reads leaves txLoop's conn.Write blocked forever, hanging
// the session (and every caller waiting on a request) indefinitely. net.Pipe
// is synchronous, so simply never reading on the peer side reproduces this
// directly, without needing to actually fill a real kernel socket buffer.
func TestWriteTimeoutClosesSessionOnStalledPeer(t *testing.T) {
	sessionConn, peerConn := net.Pipe()
	defer peerConn.Close()
	sess, err := New(sessionConn, Config{
		Role:                RoleESME,
		WriteTimeout:        30 * time.Millisecond,
		EnquireLinkInterval: -1,
		InactivityTimeout:   -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	// The peer never reads, so this bind request's write can never complete
	// on its own. Run it in the background: without the write deadline this
	// would block forever.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = sess.BindTransceiver(ctx, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion34})
	}()

	select {
	case <-sess.Done():
	case <-time.After(time.Second):
		t.Fatal("session did not terminate when the write deadline expired against a stalled peer")
	}
	if !errors.Is(sess.Err(), os.ErrDeadlineExceeded) {
		t.Fatalf("terminal error = %v (%T), want it to wrap os.ErrDeadlineExceeded", sess.Err(), sess.Err())
	}
}

func TestAutomaticEnquireLinkHealthyPeer(t *testing.T) {
	clientConn, peerConn := net.Pipe()
	defer peerConn.Close()
	registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	enquireSeen := make(chan struct{})
	peerErr := make(chan error, 1)
	go func() {
		bind, err := readOnePDU(peerConn, registry)
		if err != nil {
			peerErr <- err
			return
		}
		frame, err := codec.EncodePDU(nil, codec.ResponseHeader(bind.Header, protocol.CommandBindTransceiverResp, protocol.StatusOK), protocol.BindResponse{SystemID: []byte("smsc")}, registry)
		if err != nil {
			peerErr <- err
			return
		}
		if err := transport.WriteFull(peerConn, frame); err != nil {
			peerErr <- err
			return
		}
		request, err := readOnePDU(peerConn, registry)
		if err != nil {
			peerErr <- err
			return
		}
		if request.Header.CommandID != protocol.CommandEnquireLink {
			peerErr <- errors.New("expected enquire_link")
			return
		}
		close(enquireSeen)
		frame, err = codec.EncodePDU(nil, codec.ResponseHeader(request.Header, protocol.CommandEnquireLinkResp, protocol.StatusOK), protocol.EmptyBody{}, registry)
		if err != nil {
			peerErr <- err
			return
		}
		peerErr <- transport.WriteFull(peerConn, frame)
	}()
	sess, err := New(clientConn, Config{
		Role:                RoleESME,
		SessionInitTimeout:  time.Second,
		ResponseTimeout:     100 * time.Millisecond,
		EnquireLinkInterval: 35 * time.Millisecond,
		EnquireLinkTimeout:  80 * time.Millisecond,
		InactivityTimeout:   300 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := sess.BindTransceiver(ctx, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion34}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-enquireSeen:
	case <-time.After(time.Second):
		t.Fatal("automatic enquire_link not sent")
	}
	if err := <-peerErr; err != nil {
		t.Fatal(err)
	}
	select {
	case <-sess.Done():
		t.Fatalf("healthy enquire_link closed session: %v", sess.Err())
	case <-time.After(25 * time.Millisecond):
	}
}

func TestAutomaticEnquireLinkTimeoutClosesSession(t *testing.T) {
	clientConn, peerConn := net.Pipe()
	defer peerConn.Close()
	registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		bind, err := readOnePDU(peerConn, registry)
		if err != nil {
			return
		}
		frame, _ := codec.EncodePDU(nil, codec.ResponseHeader(bind.Header, protocol.CommandBindTransceiverResp, protocol.StatusOK), protocol.BindResponse{}, registry)
		_ = transport.WriteFull(peerConn, frame)
		_, _ = readOnePDU(peerConn, registry) // consume enquire_link but never answer it
	}()
	sess, err := New(clientConn, Config{
		Role:                RoleESME,
		SessionInitTimeout:  time.Second,
		EnquireLinkInterval: 30 * time.Millisecond,
		EnquireLinkTimeout:  35 * time.Millisecond,
		InactivityTimeout:   time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := sess.BindTransceiver(ctx, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion34}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sess.Done():
	case <-time.After(time.Second):
		t.Fatal("unresponsive enquire_link peer was not closed")
	}
	var timeout *TimeoutError
	if !errors.As(sess.Err(), &timeout) || timeout.Kind != TimeoutEnquireLink {
		t.Fatalf("terminal error=%T %v", sess.Err(), sess.Err())
	}
	metrics := sess.Metrics()
	if metrics.ResponseTimeouts != 1 || metrics.EnquireLinkTimeouts != 1 || metrics.EnquireLinkSent != 1 {
		t.Fatalf("enquire timeout metrics=%+v", metrics)
	}
}

func TestInactivityTimeoutClosesBoundSession(t *testing.T) {
	clientConn, peerConn := net.Pipe()
	defer peerConn.Close()
	registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		bind, err := readOnePDU(peerConn, registry)
		if err != nil {
			return
		}
		frame, _ := codec.EncodePDU(nil, codec.ResponseHeader(bind.Header, protocol.CommandBindTransceiverResp, protocol.StatusOK), protocol.BindResponse{}, registry)
		_ = transport.WriteFull(peerConn, frame)
	}()
	sess, err := New(clientConn, Config{
		Role:                RoleESME,
		SessionInitTimeout:  time.Second,
		EnquireLinkInterval: -1,
		InactivityTimeout:   45 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := sess.BindTransceiver(ctx, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion34}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sess.Done():
	case <-time.After(time.Second):
		t.Fatal("inactivity timeout did not close session")
	}
	var timeout *TimeoutError
	if !errors.As(sess.Err(), &timeout) || timeout.Kind != TimeoutInactivity {
		t.Fatalf("terminal error=%T %v", sess.Err(), sess.Err())
	}
	if got := sess.Metrics().InactivityTimeouts; got != 1 {
		t.Fatalf("inactivity timeout metric=%d want 1", got)
	}
}

func TestResponseAndTimeoutBoundaryHasOneWinner(t *testing.T) {
	for i := 0; i < 50; i++ {
		done := make(chan struct{})
		table := newPendingTable(1)
		var releases atomic.Int64
		request := &pendingRequest{requestID: protocol.CommandSubmitSM, expectedID: protocol.CommandSubmitSMResp, done: make(chan requestResult, 1), releaseWindow: func() { releases.Add(1) }}
		manager := newDeadlineManager(done, func(item *deadlineItem) {
			table.completeError(item.sequence, &TimeoutError{Kind: item.kind, Command: item.command, Sequence: item.sequence, After: item.after})
		})
		go manager.run()
		if err := table.insert(7, request); err != nil {
			t.Fatal(err)
		}
		item := manager.schedule(3*time.Millisecond, TimeoutResponse, protocol.CommandSubmitSM, 7)
		request.attachDeadline(manager, item)
		go func() {
			time.Sleep(3 * time.Millisecond)
			if p, ok := table.takeResponse(7, protocol.CommandSubmitSMResp); ok {
				p.done <- requestResult{pdu: codec.DecodedPDU{Header: codec.Header{SequenceNumber: 7}}}
			}
		}()
		select {
		case <-request.done:
		case <-time.After(time.Second):
			t.Fatal("no terminal winner")
		}
		close(done)
		if got := releases.Load(); got != 1 {
			t.Fatalf("iteration %d releases=%d want 1", i, got)
		}
		if table.len() != 0 {
			t.Fatalf("iteration %d pending leaked", i)
		}
	}
}

// TestSessionStartsExactlyThreeGoroutines pins Task 2.4's acceptance
// criterion (Finding P5): liveness supervision no longer has its own ticker
// goroutine, so session.New starts exactly rxLoop, txLoop, and
// deadlines.run() — three, down from four. It also checks that every one of
// them actually exits on Close, since the point of a fixed goroutine count
// is defeated if any of them lingers past the session's own lifetime.
func TestSessionStartsExactlyThreeGoroutines(t *testing.T) {
	runtime.GC()
	time.Sleep(20 * time.Millisecond) // let any goroutine from an earlier test finish unwinding
	baseline := runtime.NumGoroutine()

	sessionConn, peerConn := net.Pipe()
	defer peerConn.Close()
	sess, err := New(sessionConn, Config{Role: RoleESME})
	if err != nil {
		t.Fatal(err)
	}

	// The three goroutines are started by `go` statements at the end of New;
	// give the scheduler a moment to actually begin running them before
	// counting.
	var withSession int
	deadline := time.Now().Add(time.Second)
	for {
		withSession = runtime.NumGoroutine()
		if withSession-baseline >= 3 || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if got := withSession - baseline; got != 3 {
		t.Fatalf("goroutines added by New() = %d, want exactly 3 (rxLoop, txLoop, deadlines.run(); Task 2.4 removed livenessLoop, the fourth)", got)
	}

	sess.Close()

	deadline = time.Now().Add(2 * time.Second)
	for {
		if runtime.NumGoroutine() <= baseline {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("goroutine count did not return to baseline after Close: now=%d baseline=%d", runtime.NumGoroutine(), baseline)
		}
		time.Sleep(time.Millisecond)
	}
}

// deadlineCountingConn counts SetWriteDeadline calls so a test can assert how
// often txLoop actually touches the conn's deadline.
type deadlineCountingConn struct {
	net.Conn
	writeDeadlines atomic.Int64
}

func (c *deadlineCountingConn) SetWriteDeadline(t time.Time) error {
	c.writeDeadlines.Add(1)
	return c.Conn.SetWriteDeadline(t)
}

// TestWriteDeadlineIsNotRearmedOnEveryBatch pins the amortized re-arming that
// replaced Task 1.4's set-and-clear around every batch (which measurably cost
// localhost TCP throughput — see the comment in txLoop). With a WriteTimeout
// far longer than the test, more than half of it always remains after the
// first write arms the deadline, so exactly one SetWriteDeadline call should
// happen no matter how many more batches follow.
func TestWriteDeadlineIsNotRearmedOnEveryBatch(t *testing.T) {
	rawConn, peerConn := net.Pipe()
	counting := &deadlineCountingConn{Conn: rawConn}
	registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	var stats livenessPeerStats
	go runLivenessPeer(peerConn, registry, &stats)

	sess, err := New(counting, Config{
		Role:                RoleESME,
		WriteTimeout:        10 * time.Second,
		EnquireLinkInterval: -1,
		InactivityTimeout:   -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := sess.BindTransceiver(ctx, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion34}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		if _, err := sess.SubmitSM(ctx, protocol.SubmitSM{SourceAddr: []byte("1"), DestinationAddr: []byte("2")}); err != nil {
			t.Fatalf("submit %d: %v", i, err)
		}
	}
	if got := counting.writeDeadlines.Load(); got != 1 {
		t.Fatalf("SetWriteDeadline called %d times across 51 batches, want exactly 1", got)
	}
}

// TestWriteTimeoutStillBoundsAStallAfterSuccessfulWrites proves amortizing did
// not weaken what the deadline is for. The deadline is armed by the first
// write, several more succeed, and then the peer stops reading. The stalled
// write must still fail with os.ErrDeadlineExceeded, no later than
// WriteTimeout after it began (the upper bound) and no earlier than about half
// of it (the lower bound this design trades for not re-arming every batch: a
// write never starts with less than WriteTimeout/2 remaining).
func TestWriteTimeoutStillBoundsAStallAfterSuccessfulWrites(t *testing.T) {
	clientConn, peerConn := net.Pipe()
	defer peerConn.Close()
	registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}

	const (
		writeTimeout = 200 * time.Millisecond
		okRequests   = 5
	)
	stall := make(chan struct{})
	go func() {
		serve := func() error {
			pdu, err := readOnePDU(peerConn, registry)
			if err != nil {
				return err
			}
			respID, body := protocol.CommandBindTransceiverResp, any(protocol.BindResponse{SystemID: []byte("smsc")})
			if pdu.Header.CommandID == protocol.CommandSubmitSM {
				respID, body = protocol.CommandSubmitSMResp, protocol.SubmitSMResp{MessageID: []byte("m")}
			}
			frame, err := codec.EncodePDU(nil, codec.ResponseHeader(pdu.Header, respID, protocol.StatusOK), body, registry)
			if err != nil {
				return err
			}
			return transport.WriteFull(peerConn, frame)
		}
		for i := 0; i < 1+okRequests; i++ { // the bind, then okRequests submits
			if serve() != nil {
				return
			}
		}
		<-stall // stop reading: the next write can never complete
	}()
	defer close(stall)

	sess, err := New(clientConn, Config{
		Role:                RoleESME,
		WriteTimeout:        writeTimeout,
		ResponseTimeout:     10 * time.Second,
		EnquireLinkInterval: -1,
		InactivityTimeout:   -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := sess.BindTransceiver(ctx, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion34}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < okRequests; i++ {
		if _, err := sess.SubmitSM(ctx, protocol.SubmitSM{SourceAddr: []byte("1"), DestinationAddr: []byte("2")}); err != nil {
			t.Fatalf("submit %d: %v", i, err)
		}
	}

	stallStart := time.Now()
	go func() {
		_, _ = sess.SubmitSM(ctx, protocol.SubmitSM{SourceAddr: []byte("1"), DestinationAddr: []byte("2")})
	}()
	select {
	case <-sess.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("stalled write was never cut off")
	}
	if !errors.Is(sess.Err(), os.ErrDeadlineExceeded) {
		t.Fatalf("terminal error = %v, want it to wrap os.ErrDeadlineExceeded", sess.Err())
	}
	elapsed := time.Since(stallStart)
	if elapsed > writeTimeout+250*time.Millisecond {
		t.Fatalf("stalled write took %v to fail, want at most about %v", elapsed, writeTimeout)
	}
	if elapsed < writeTimeout/2-40*time.Millisecond {
		t.Fatalf("stalled write failed after only %v, want at least about %v (a write should never start with less than WriteTimeout/2 left)", elapsed, writeTimeout/2)
	}
}
