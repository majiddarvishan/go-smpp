package session

import (
	"context"
	"errors"
	"net"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/transport"
)

// Tests here cover behaviors specific to folding the liveness checks into the
// shared deadline heap (Task 2.4, Finding P5) that the older timer tests in
// timers_test.go don't reach. Those tests already prove each timer fires for
// a dead or idle peer; these prove the parts that are new: a check that fires
// while it is not yet time to act reschedules itself instead of terminating
// (because activity landed after it was scheduled, or the session isn't bound
// yet), and that doing so doesn't accumulate heap entries or goroutines.

type livenessPeerStats struct {
	enquireLinks atomic.Int64
}

// runLivenessPeer answers the bind, then every SubmitSM and EnquireLink it
// receives, until the connection closes or it reads anything it doesn't
// expect.
func runLivenessPeer(conn net.Conn, registry *codec.Registry, stats *livenessPeerStats) {
	defer conn.Close()
	for {
		pdu, err := readOnePDU(conn, registry)
		if err != nil {
			return
		}
		var (
			respID protocol.CommandID
			body   any
		)
		switch pdu.Header.CommandID {
		case protocol.CommandBindTransceiver:
			respID, body = protocol.CommandBindTransceiverResp, protocol.BindResponse{SystemID: []byte("smsc")}
		case protocol.CommandSubmitSM:
			respID, body = protocol.CommandSubmitSMResp, protocol.SubmitSMResp{MessageID: []byte("m")}
		case protocol.CommandEnquireLink:
			stats.enquireLinks.Add(1)
			respID, body = protocol.CommandEnquireLinkResp, protocol.EmptyBody{}
		default:
			return
		}
		frame, err := codec.EncodePDU(nil, codec.ResponseHeader(pdu.Header, respID, protocol.StatusOK), body, registry)
		if err != nil {
			return
		}
		if err := transport.WriteFull(conn, frame); err != nil {
			return
		}
	}
}

// TestInactivityDeadlineReschedulesInsteadOfTerminatingWhileActive keeps a
// bound session busy for more than three full InactivityTimeout windows. The
// heap slot's first scheduled fire lands well inside that busy period, at a
// moment activity was recorded much more recently than InactivityTimeout ago,
// so it must reschedule rather than terminate. It then stops all traffic and
// checks the session still closes, at about InactivityTimeout after the last
// activity — proving the rescheduled deadline is measured from real activity,
// not from when the slot was first armed.
func TestInactivityDeadlineReschedulesInsteadOfTerminatingWhileActive(t *testing.T) {
	clientConn, peerConn := net.Pipe()
	registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	var stats livenessPeerStats
	go runLivenessPeer(peerConn, registry, &stats)

	const inactivity = 120 * time.Millisecond
	sess, err := New(clientConn, Config{
		Role:                RoleESME,
		SessionInitTimeout:  5 * time.Second,
		EnquireLinkInterval: -1,
		InactivityTimeout:   inactivity,
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

	var lastActivity time.Time
	for i := 0; i < 8; i++ {
		time.Sleep(50 * time.Millisecond)
		if _, err := sess.SubmitSM(ctx, protocol.SubmitSM{SourceAddr: []byte("1"), DestinationAddr: []byte("2")}); err != nil {
			t.Fatalf("round %d: session ended while still active: %v (session err: %v)", i, err, sess.Err())
		}
		lastActivity = time.Now()
	}

	select {
	case <-sess.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("session never closed after traffic stopped")
	}
	var timeoutErr *TimeoutError
	if !errors.As(sess.Err(), &timeoutErr) || timeoutErr.Kind != TimeoutInactivity {
		t.Fatalf("terminal error = %v, want a TimeoutInactivity TimeoutError", sess.Err())
	}
	// The test's lastActivity is stamped after SubmitSM returns, slightly later
	// than the session's own last-activity record, so allow for that when
	// checking it did not close early.
	if elapsed := time.Since(lastActivity); elapsed < inactivity-30*time.Millisecond {
		t.Fatalf("closed %v after last activity, want about %v — the slot terminated on its original fire time instead of rescheduling", elapsed, inactivity)
	}
}

// TestInactivityDeadlineFiringBeforeBindReschedulesAndAppliesAfterBind sits
// unbound across more than two firings of the inactivity slot. InactivityTimeout
// only applies to a bound session, so those firings must reschedule without
// terminating. Then it binds late and checks the timeout still applies from
// there, measured from the bind exchange's activity rather than from session
// creation.
func TestInactivityDeadlineFiringBeforeBindReschedulesAndAppliesAfterBind(t *testing.T) {
	clientConn, peerConn := net.Pipe()
	registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	var stats livenessPeerStats
	go runLivenessPeer(peerConn, registry, &stats)

	const inactivity = 80 * time.Millisecond
	sess, err := New(clientConn, Config{
		Role:                RoleESME,
		SessionInitTimeout:  5 * time.Second,
		EnquireLinkInterval: -1,
		InactivityTimeout:   inactivity,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	select {
	case <-sess.Done():
		t.Fatalf("session closed before bind by a timer that only applies once bound: %v", sess.Err())
	case <-time.After(220 * time.Millisecond):
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := sess.BindTransceiver(ctx, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion34}); err != nil {
		t.Fatal(err)
	}
	boundAt := time.Now()

	select {
	case <-sess.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("bound idle session never closed on inactivity")
	}
	var timeoutErr *TimeoutError
	if !errors.As(sess.Err(), &timeoutErr) || timeoutErr.Kind != TimeoutInactivity {
		t.Fatalf("terminal error = %v, want a TimeoutInactivity TimeoutError", sess.Err())
	}
	if elapsed := time.Since(boundAt); elapsed < inactivity-30*time.Millisecond {
		t.Fatalf("closed %v after bind, want about %v — inactivity was measured from session creation, not from the bind's activity", elapsed, inactivity)
	}
}

// TestLivenessRescheduleKeepsHeapAndGoroutinesBounded runs many EnquireLink
// probe cycles against a healthy peer and checks nothing accumulates: the
// shared heap holds only the three fixed liveness slots plus at most one
// in-flight probe's own response deadline, and the short-lived per-probe
// goroutine fireEnquireLinkDeadline spawns has not piled up (each must finish
// once its response arrives).
func TestLivenessRescheduleKeepsHeapAndGoroutinesBounded(t *testing.T) {
	runtime.GC()
	time.Sleep(20 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	clientConn, peerConn := net.Pipe()
	registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}
	var stats livenessPeerStats
	go runLivenessPeer(peerConn, registry, &stats)

	sess, err := New(clientConn, Config{
		Role:                RoleESME,
		SessionInitTimeout:  5 * time.Second,
		EnquireLinkInterval: 10 * time.Millisecond,
		EnquireLinkTimeout:  500 * time.Millisecond,
		InactivityTimeout:   10 * time.Second,
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

	deadline := time.Now().Add(3 * time.Second)
	for stats.enquireLinks.Load() < 20 {
		if time.Now().After(deadline) {
			t.Fatalf("only %d automatic enquire_links in 3s, want at least 20", stats.enquireLinks.Load())
		}
		time.Sleep(5 * time.Millisecond)
	}

	select {
	case <-sess.Done():
		t.Fatalf("healthy session closed during probing: %v", sess.Err())
	default:
	}
	if got := sess.deadlines.len(); got > 4 {
		t.Fatalf("deadline heap holds %d entries after 20+ probe cycles, want at most 4 (3 liveness slots + 1 in-flight probe)", got)
	}
	// 3 session goroutines + the test's peer goroutine + at most one in-flight
	// probe goroutine; the margin above that would still catch 20 leaked ones.
	if got := runtime.NumGoroutine() - baseline; got > 8 {
		t.Fatalf("%d goroutines above baseline after 20+ probes, want a small bounded number — per-probe goroutines are accumulating", got)
	}
}
