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
	go b.Write([]byte{0, 0})
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
	go peer.Write(unfinishedPDU(8))
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
	go peer.Write(unfinishedPDU(8))
	waitTerminated(t, sess, readStallLimit)
	wantTimeout(t, sess, TimeoutInactivity)
}

// DOCUMENTED LIMITATION, pinned so it cannot change unnoticed. With
// enquire_link disabled, outbound traffic counts as activity (docs/
// TIMEOUTS_AND_LIVENESS.md: "inbound or outbound SMPP activity"). So a peer that
// drains our writes but has stopped sending anything back is not detected while
// we keep sending: every request ends in a response timeout, the session
// stays up. The default configuration (enquire_link every 30 s) is not affected,
// see the EnquireLink tests above. If a policy such as "terminate after N
// consecutive response timeouts" is ever added, this test should change with it.
func TestDisabledEnquireLinkLeavesAReadStalledPeerUndetectedWhileWeKeepSending(t *testing.T) {
	sess, peer := bindAgainstStallingPeer(t, Config{SessionInitTimeout: time.Second, EnquireLinkInterval: -1, InactivityTimeout: 100 * time.Millisecond, ResponseTimeout: 60 * time.Millisecond})
	go peer.Write(unfinishedPDU(8))

	stop := make(chan struct{})
	defer close(stop)
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
			time.Sleep(20 * time.Millisecond)
		}
	}()

	// five times the inactivity timeout
	select {
	case <-sess.Done():
		t.Fatalf("session terminated (%v); the documented limitation no longer holds, update this test and docs/TIMEOUTS_AND_LIVENESS.md", sess.Err())
	case <-time.After(500 * time.Millisecond):
	}
	if got := sess.Metrics().ResponseTimeouts; got < 3 {
		t.Fatalf("only %d response timeouts; the sender loop did not exercise the stall", got)
	}
}
