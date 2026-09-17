package session

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/majiddarvishan/go-smpp/protocol"
)

type txBatchGateConn struct {
	net.Conn
	writes     atomic.Int64
	blockWrite int64
	entered    chan struct{}
	release    chan struct{}
	enterOnce  sync.Once
}

func (c *txBatchGateConn) Write(p []byte) (int, error) {
	writeNumber := c.writes.Add(1)
	if writeNumber == c.blockWrite {
		c.enterOnce.Do(func() { close(c.entered) })
		<-c.release
	}
	return c.Conn.Write(p)
}

func TestTXLoopOpportunisticallyBatchesQueuedPDUs(t *testing.T) {
	rawClient, serverConn := net.Pipe()
	gatedClient := &txBatchGateConn{
		Conn:       rawClient,
		blockWrite: 2, // bind is write #1; block the first submit write
		entered:    make(chan struct{}),
		release:    make(chan struct{}),
	}

	serverHandler := HandlerFunc(func(_ context.Context, _ *Session, pdu InboundPDU) (Response, error) {
		switch pdu.Header.CommandID {
		case protocol.CommandBindTransceiver:
			return Response{Status: protocol.StatusOK, Body: protocol.BindResponse{SystemID: []byte("batch-smsc")}}, nil
		case protocol.CommandSubmitSM:
			return Response{Status: protocol.StatusOK, Body: protocol.SubmitSMResp{MessageID: []byte("ok")}}, nil
		default:
			return Response{Status: protocol.StatusSystemError, Body: protocol.EmptyBody{}}, nil
		}
	})

	serverSession, err := New(serverConn, Config{
		Role:                RoleSMSC,
		Handler:             serverHandler,
		EnquireLinkInterval: -1,
		InactivityTimeout:   -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()

	clientSession, err := New(gatedClient, Config{
		Role:                RoleESME,
		TXQueueSize:         128,
		TXBatchItems:        32,
		TXBatchBytes:        64 << 10,
		ResponseTimeout:     5 * time.Second,
		EnquireLinkInterval: -1,
		InactivityTimeout:   -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := clientSession.BindTransceiver(ctx, protocol.BindRequest{InterfaceVersion: protocol.InterfaceVersion34}); err != nil {
		t.Fatalf("bind: %v", err)
	}

	const requests = 64
	errs := make(chan error, requests)
	for i := 0; i < requests; i++ {
		go func() {
			_, err := clientSession.SubmitSM(ctx, protocol.SubmitSM{DestinationAddr: []byte("12025550101"), ShortMessage: []byte("batch")})
			errs <- err
		}()
	}

	select {
	case <-gatedClient.entered:
	case <-ctx.Done():
		t.Fatalf("first submit write did not block: %v", ctx.Err())
	}

	deadline := time.Now().Add(2 * time.Second)
	for clientSession.Pending() < requests && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := clientSession.Pending(); got != requests {
		close(gatedClient.release)
		t.Fatalf("pending before release=%d want=%d", got, requests)
	}
	close(gatedClient.release)

	for i := 0; i < requests; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("submit: %v", err)
		}
	}

	// Without batching this path requires one client-side Write per submit plus
	// the bind. With a batch size of 32, the queued burst should collapse to only
	// a handful of writes. Keep the assertion loose enough to avoid coupling the
	// test to scheduler timing while still proving coalescing occurred.
	if writes := gatedClient.writes.Load(); writes >= requests/2 {
		t.Fatalf("client writes=%d; expected opportunistic batching for %d requests", writes, requests)
	}
}
