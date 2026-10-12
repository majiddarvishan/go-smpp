package session

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/transport"
)

// T3 / Task 6.3. Task 1.4 covers the write side: a peer that accepts the
// connection and never reads. These are the read-side equivalents: a peer that
// sends part of a PDU, or trickles one, and never finishes. In every case the
// question is the same: does the session end on a bound, or hang?
//
// What counts as liveness is stated by these tests, not assumed: bytes of an
// unfinished PDU do not. Activity is recorded per complete inbound frame and per
// completed write (rx.go, tx.go), so a peer cannot hold a session open by
// dribbling bytes.

const readStallLimit = 3 * time.Second // generous: every timeout below is 40-200 ms

// declaring 1 MiB with only a few bytes behind it: the PDU can never complete.
func unfinishedPDU(extra int) []byte {
	b := make([]byte, 4+extra)
	binary.BigEndian.PutUint32(b, 1<<20)
	return b
}

// bindAgainstStallingPeer returns a bound ESME session over net.Pipe and the
// peer's end. The peer has answered the bind and, from then on, drains
// whatever the session writes without ever replying: it has stopped
// *sending*, not stopped reading.
func bindAgainstStallingPeer(t *testing.T, cfg Config) (*Session, net.Conn) {
	t.Helper()
	clientConn, peerConn := net.Pipe()
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
		_, _ = io.Copy(io.Discard, peerConn)
	}()
	cfg.Role = RoleESME
	sess, err := New(clientConn, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Close(); _ = peerConn.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := sess.BindTransceiver(ctx, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion34}); err != nil {
		t.Fatal(err)
	}
	return sess, peerConn
}

func waitTerminated(t *testing.T, sess *Session, within time.Duration) time.Duration {
	t.Helper()
	start := time.Now()
	select {
	case <-sess.Done():
		return time.Since(start)
	case <-time.After(within):
		t.Fatalf("session still alive after %v; it should have terminated", within)
		return 0
	}
}

func wantTimeout(t *testing.T, sess *Session, kind TimeoutKind) {
	t.Helper()
	var timeout *TimeoutError
	if !errors.As(sess.Err(), &timeout) || timeout.Kind != kind {
		t.Fatalf("terminal error = %T %v, want a %v timeout", sess.Err(), sess.Err(), kind)
	}
}

func TestPeerSendingHalfALengthWordBeforeBindIsClosedByInitTimeout(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	sess, err := New(a, Config{Role: RoleSMSC, SessionInitTimeout: 100 * time.Millisecond, EnquireLinkInterval: -1, InactivityTimeout: -1})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	go func() { _, _ = b.Write([]byte{0, 0}) }() // the write fails once the session closes; that is the point
	took := waitTerminated(t, sess, readStallLimit)
	wantTimeout(t, sess, TimeoutSessionInit)
	if took < 50*time.Millisecond {
		t.Fatalf("closed after only %v, before the 100 ms init timeout", took)
	}
}

// The slow-loris case: bytes keep arriving, so a "no data" timer would never
// fire, but the PDU never completes. The init deadline is absolute.
func TestTricklingAnUnfinishedPDUDoesNotExtendTheInitDeadline(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	sess, err := New(a, Config{Role: RoleSMSC, SessionInitTimeout: 150 * time.Millisecond, EnquireLinkInterval: -1, InactivityTimeout: -1})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	go func() {
		if _, err := b.Write(unfinishedPDU(0)); err != nil {
			return
		}
		for {
			if _, err := b.Write([]byte{1}); err != nil {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	took := waitTerminated(t, sess, readStallLimit)
	wantTimeout(t, sess, TimeoutSessionInit)
	if took > time.Second {
		t.Fatalf("a trickling peer held the session for %v against a 150 ms init timeout", took)
	}
}

func TestBoundPeerThatStallsMidPDUIsClosedByEnquireLinkTimeout(t *testing.T) {
	sess, peer := bindAgainstStallingPeer(t, Config{SessionInitTimeout: time.Second, EnquireLinkInterval: 40 * time.Millisecond, EnquireLinkTimeout: 40 * time.Millisecond, InactivityTimeout: 10 * time.Second})
	go func() { _, _ = peer.Write(unfinishedPDU(8)) }() // fails when the session closes; ignored
	waitTerminated(t, sess, readStallLimit)
	wantTimeout(t, sess, TimeoutEnquireLink)
}

// Stated behaviour, and a deliberate one: a peer that keeps bytes flowing but
// never completes a PDU is not "alive". Our enquire_link goes unanswered
// because the stream is stuck inside the unfinished PDU, so the probe times
// out. The corollary is that a PDU which legitimately takes longer than
// EnquireLinkInterval + EnquireLinkTimeout to arrive will also be cut off.
func TestBoundPeerTricklingAnUnfinishedPDUIsNotTreatedAsAlive(t *testing.T) {
	sess, peer := bindAgainstStallingPeer(t, Config{SessionInitTimeout: time.Second, EnquireLinkInterval: 40 * time.Millisecond, EnquireLinkTimeout: 40 * time.Millisecond, InactivityTimeout: 10 * time.Second})
	go func() {
		if _, err := peer.Write(unfinishedPDU(0)); err != nil {
			return
		}
		for {
			if _, err := peer.Write([]byte{1}); err != nil {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	waitTerminated(t, sess, readStallLimit)
	wantTimeout(t, sess, TimeoutEnquireLink)
}

func TestReadStallWithEnquireLinkDisabledAndNothingSentIsClosedByInactivity(t *testing.T) {
	sess, peer := bindAgainstStallingPeer(t, Config{SessionInitTimeout: time.Second, EnquireLinkInterval: -1, InactivityTimeout: 200 * time.Millisecond})
	go func() { _, _ = peer.Write(unfinishedPDU(8)) }() // fails when the session closes; ignored
	waitTerminated(t, sess, readStallLimit)
	wantTimeout(t, sess, TimeoutInactivity)
}

// Decision (maintainer, 2026-10-11): when InactivityTimeout is reached, disconnect.
// InactivityTimeout is measured from the last complete inbound PDU, so our own
// writes cannot keep a session alive. Before this, outbound writes counted as
// activity, and a peer that kept reading but never answered was held open for as
// long as we kept sending, with enquire_link disabled (the first test below) and
// also with it enabled (the second: our writes kept the idle check from ever
// reaching the enquire_link interval).
func stallWhileSending(t *testing.T, cfg Config) (*Session, time.Time) {
	t.Helper()
	sess, peer := bindAgainstStallingPeer(t, cfg)
	go func() { _, _ = peer.Write(unfinishedPDU(8)) }() // fails when the session closes; ignored
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-sess.Done():
				return
			default:
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			_, _ = sess.SubmitSM(ctx, protocol.SubmitSM{SourceAddr: []byte("1"), DestinationAddr: []byte("2"), ShortMessage: []byte("x")})
			cancel()
			time.Sleep(10 * time.Millisecond)
		}
	}()
	return sess, time.Now()
}

func TestReadStalledPeerIsClosedByInactivityWhileWeKeepSendingWithEnquireLinkDisabled(t *testing.T) {
	const inactivity = 200 * time.Millisecond
	sess, started := stallWhileSending(t, Config{SessionInitTimeout: time.Second, EnquireLinkInterval: -1, InactivityTimeout: inactivity, ResponseTimeout: 30 * time.Millisecond})
	waitTerminated(t, sess, readStallLimit)
	wantTimeout(t, sess, TimeoutInactivity)
	if took := time.Since(started); took < inactivity-50*time.Millisecond {
		t.Fatalf("closed after %v, before the %v inactivity timeout", took, inactivity)
	}
}

func TestReadStalledPeerIsClosedByInactivityWhileWeKeepSendingWithEnquireLinkEnabled(t *testing.T) {
	// The enquire_link interval (1 s) is far above both the write cadence (~40 ms) and the
	// inactivity timeout (300 ms), so on a slow or busy machine no scheduling hiccup can let
	// an enquire_link out first: only the inbound-based inactivity rule can end this, and
	// the test can say so exactly.
	sess, _ := stallWhileSending(t, Config{SessionInitTimeout: time.Second, EnquireLinkInterval: time.Second, EnquireLinkTimeout: time.Second, InactivityTimeout: 300 * time.Millisecond, ResponseTimeout: 30 * time.Millisecond})
	waitTerminated(t, sess, readStallLimit)
	wantTimeout(t, sess, TimeoutInactivity)
}

// Positive control: a peer that answers keeps the session alive for many times
// the inactivity timeout, with enquire_link off and on. Without this, the two
// tests above would also pass if inbound PDUs were simply never counted.
func TestAnsweringPeerKeepsASendingSessionAlivePastTheInactivityTimeout(t *testing.T) {
	for _, el := range []time.Duration{-1, 80 * time.Millisecond} {
		clientConn, peerConn := net.Pipe()
		registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
		if err != nil {
			t.Fatal(err)
		}
		var stats livenessPeerStats
		go runLivenessPeer(peerConn, registry, &stats)
		// 300 ms against a ~20 ms cadence leaves room for a scheduling stall on a busy CI machine
		// without a false inactivity timeout.
		const inactivity = 300 * time.Millisecond
		sess, err := New(clientConn, Config{Role: RoleESME, SessionInitTimeout: time.Second, EnquireLinkInterval: el, EnquireLinkTimeout: 80 * time.Millisecond, InactivityTimeout: inactivity})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if _, err := sess.BindTransceiver(ctx, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion34}); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(4 * inactivity)
		for time.Now().Before(deadline) {
			if _, err := sess.SubmitSM(ctx, protocol.SubmitSM{SourceAddr: []byte("1"), DestinationAddr: []byte("2")}); err != nil {
				t.Fatalf("enquire_link=%v: session ended while the peer was answering: %v (session err: %v)", el, err, sess.Err())
			}
			time.Sleep(20 * time.Millisecond)
		}
		cancel()
		_ = sess.Close()
		_ = peerConn.Close()
	}
}
