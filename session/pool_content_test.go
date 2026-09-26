package session

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/transport"
)

// TestFramePoolSequentialRequestsDoNotCrossContaminate is the direct
// correctness proof for the frame pool (Finding P1 / Task 2.2): if a pooled
// buffer were ever handed to a new request before the previous request's
// bytes had actually left txLoop's write call, the peer would observe the
// wrong content for some request. Each of many sequential SubmitSM calls
// carries a SourceAddr unique to its iteration; the peer asserts an exact
// match before replying, so any cross-contamination fails the test
// immediately rather than only showing up as a rare flake.
func TestFramePoolSequentialRequestsDoNotCrossContaminate(t *testing.T) {
	clientConn, peerConn := net.Pipe()
	registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}

	const iterations = 500
	peerDone := make(chan error, 1)
	go func() {
		defer peerConn.Close()
		bind, err := readOnePDU(peerConn, registry)
		if err != nil {
			peerDone <- fmt.Errorf("read bind: %w", err)
			return
		}
		frame, err := codec.EncodePDU(nil, codec.ResponseHeader(bind.Header, protocol.CommandBindTransceiverResp, protocol.StatusOK), protocol.BindResponse{SystemID: []byte("smsc")}, registry)
		if err != nil {
			peerDone <- err
			return
		}
		if err := transport.WriteFull(peerConn, frame); err != nil {
			peerDone <- err
			return
		}
		for i := 0; i < iterations; i++ {
			pdu, err := readOnePDU(peerConn, registry)
			if err != nil {
				peerDone <- fmt.Errorf("iteration %d: read: %w", i, err)
				return
			}
			body, ok := pdu.Body.(protocol.SubmitSM)
			if !ok {
				peerDone <- fmt.Errorf("iteration %d: body type %T", i, pdu.Body)
				return
			}
			want := sourceAddrForIteration(i)
			if string(body.SourceAddr) != want {
				peerDone <- fmt.Errorf("iteration %d: source_addr = %q, want %q (pooled frame buffer contains the wrong request's bytes)", i, body.SourceAddr, want)
				return
			}
			resp := protocol.SubmitSMResp{MessageID: []byte(fmt.Sprintf("%d", i))}
			respFrame, err := codec.EncodePDU(nil, codec.ResponseHeader(pdu.Header, protocol.CommandSubmitSMResp, protocol.StatusOK), resp, registry)
			if err != nil {
				peerDone <- err
				return
			}
			if err := transport.WriteFull(peerConn, respFrame); err != nil {
				peerDone <- fmt.Errorf("iteration %d: write response: %w", i, err)
				return
			}
		}
		peerDone <- nil
	}()

	sess, err := New(clientConn, Config{Role: RoleESME, WindowSize: 8, MaxPending: 8, TXQueueSize: 8})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := sess.BindTransceiver(ctx, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion34}); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < iterations; i++ {
		body := protocol.SubmitSM{SourceAddr: []byte(sourceAddrForIteration(i)), DestinationAddr: []byte("1")}
		if _, err := sess.SubmitSM(ctx, body); err != nil {
			t.Fatalf("iteration %d: SubmitSM: %v", i, err)
		}
	}

	select {
	case err := <-peerDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("peer did not finish")
	}
}

func sourceAddrForIteration(i int) string {
	return fmt.Sprintf("SRC-%06d", i)
}

// TestFramePoolConcurrentRequestsDoNotCrossContaminate is the concurrent
// counterpart: many goroutines call SubmitSM on the same session at once,
// each with a globally unique SourceAddr. request() encodes into a pooled
// buffer in the caller's own goroutine before txLoop ever sees the item, so
// this exercises the pool's Get/Put path under real concurrent access, not
// just txLoop's single-threaded write side. The peer records every
// SourceAddr it sees; duplicates would mean two different requests ended up
// sharing (and one overwriting) the same buffer content, and a garbled value
// would mean a partial/overlapping write into a buffer still in use
// elsewhere. Run with -race.
func TestFramePoolConcurrentRequestsDoNotCrossContaminate(t *testing.T) {
	clientConn, peerConn := net.Pipe()
	registry, err := codec.NewSMPP34Registry(codec.RegistryCompatible)
	if err != nil {
		t.Fatal(err)
	}

	const (
		callers        = 40
		perCaller      = 50
		totalRequests  = callers * perCaller
		submitTimeout  = 15 * time.Second
		overallTimeout = 20 * time.Second
	)

	seen := make(map[string]bool, totalRequests)
	var seenMu sync.Mutex
	peerDone := make(chan error, 1)
	go func() {
		defer peerConn.Close()
		bind, err := readOnePDU(peerConn, registry)
		if err != nil {
			peerDone <- fmt.Errorf("read bind: %w", err)
			return
		}
		frame, err := codec.EncodePDU(nil, codec.ResponseHeader(bind.Header, protocol.CommandBindTransceiverResp, protocol.StatusOK), protocol.BindResponse{SystemID: []byte("smsc")}, registry)
		if err != nil {
			peerDone <- err
			return
		}
		if err := transport.WriteFull(peerConn, frame); err != nil {
			peerDone <- err
			return
		}
		for i := 0; i < totalRequests; i++ {
			pdu, err := readOnePDU(peerConn, registry)
			if err != nil {
				peerDone <- fmt.Errorf("request %d: read: %w", i, err)
				return
			}
			body, ok := pdu.Body.(protocol.SubmitSM)
			if !ok {
				peerDone <- fmt.Errorf("request %d: body type %T", i, pdu.Body)
				return
			}
			addr := string(body.SourceAddr)
			seenMu.Lock()
			duplicate := seen[addr]
			seen[addr] = true
			seenMu.Unlock()
			if duplicate {
				peerDone <- fmt.Errorf("request %d: source_addr %q seen twice (pooled buffer reused while still referenced, or two requests collided on one buffer)", i, addr)
				return
			}
			resp := protocol.SubmitSMResp{MessageID: []byte(fmt.Sprintf("%d", i))}
			respFrame, err := codec.EncodePDU(nil, codec.ResponseHeader(pdu.Header, protocol.CommandSubmitSMResp, protocol.StatusOK), resp, registry)
			if err != nil {
				peerDone <- err
				return
			}
			if err := transport.WriteFull(peerConn, respFrame); err != nil {
				peerDone <- fmt.Errorf("request %d: write response: %w", i, err)
				return
			}
		}
		peerDone <- nil
	}()

	sess, err := New(clientConn, Config{Role: RoleESME, WindowSize: callers, MaxPending: callers, TXQueueSize: callers, TXBatchItems: 8})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	bindCtx, cancelBind := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelBind()
	if _, err := sess.BindTransceiver(bindCtx, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion34}); err != nil {
		t.Fatal(err)
	}

	var (
		counter int
		mu      sync.Mutex
		wg      sync.WaitGroup
		callErr = make(chan error, totalRequests)
	)
	next := func() int {
		mu.Lock()
		defer mu.Unlock()
		v := counter
		counter++
		return v
	}
	ctx, cancel := context.WithTimeout(context.Background(), submitTimeout)
	defer cancel()
	wg.Add(callers)
	for c := 0; c < callers; c++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perCaller; j++ {
				n := next()
				body := protocol.SubmitSM{SourceAddr: []byte(sourceAddrForIteration(n)), DestinationAddr: []byte("1")}
				if _, err := sess.SubmitSM(ctx, body); err != nil {
					callErr <- fmt.Errorf("request %d: %w", n, err)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(callErr)
	for err := range callErr {
		t.Fatal(err)
	}

	select {
	case err := <-peerDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(overallTimeout):
		t.Fatal("peer did not finish")
	}

	if len(seen) != totalRequests {
		t.Fatalf("peer saw %d distinct source addrs, want %d", len(seen), totalRequests)
	}
}
