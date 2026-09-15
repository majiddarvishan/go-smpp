package session

import (
	"errors"
	"sync"
	"testing"

	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/protocol"
)

func TestPendingOutOfOrderCompletion(t *testing.T) {
	table := newPendingTable(4)
	one := &pendingRequest{requestID: protocol.CommandSubmitSM, expectedID: protocol.CommandSubmitSMResp, done: make(chan requestResult, 1)}
	two := &pendingRequest{requestID: protocol.CommandSubmitSM, expectedID: protocol.CommandSubmitSMResp, done: make(chan requestResult, 1)}
	if err := table.insert(1, one); err != nil { t.Fatal(err) }
	if err := table.insert(2, two); err != nil { t.Fatal(err) }

	p, ok := table.takeResponse(2, protocol.CommandSubmitSMResp)
	if !ok || p != two { t.Fatal("sequence 2 did not correlate") }
	p.done <- requestResult{pdu: codec.DecodedPDU{Header: codec.Header{SequenceNumber: 2}}}
	p, ok = table.takeResponse(1, protocol.CommandSubmitSMResp)
	if !ok || p != one { t.Fatal("sequence 1 did not correlate") }
	p.done <- requestResult{pdu: codec.DecodedPDU{Header: codec.Header{SequenceNumber: 1}}}
	if got := (<-two.done).pdu.Header.SequenceNumber; got != 2 { t.Fatalf("got %d", got) }
	if got := (<-one.done).pdu.Header.SequenceNumber; got != 1 { t.Fatalf("got %d", got) }
}

func TestPendingExactlyOneTerminalWinner(t *testing.T) {
	for i := 0; i < 100; i++ {
		table := newPendingTable(1)
		request := &pendingRequest{requestID: protocol.CommandSubmitSM, expectedID: protocol.CommandSubmitSMResp, done: make(chan requestResult, 1)}
		if err := table.insert(7, request); err != nil { t.Fatal(err) }
		var wg sync.WaitGroup
		wg.Add(3)
		wins := make(chan string, 3)
		go func() {
			defer wg.Done()
			if p, ok := table.takeResponse(7, protocol.CommandSubmitSMResp); ok {
				p.done <- requestResult{pdu: codec.DecodedPDU{Header: codec.Header{SequenceNumber: 7}}}
				wins <- "response"
			}
		}()
		go func() {
			defer wg.Done()
			if _, ok := table.completeError(7, &TimeoutError{Kind: TimeoutResponse, Command: protocol.CommandSubmitSM, Sequence: 7}); ok {
				wins <- "timeout"
			}
		}()
		go func() {
			defer wg.Done()
			if _, ok := table.completeError(7, errors.New("cancelled")); ok {
				wins <- "cancel"
			}
		}()
		wg.Wait()
		close(wins)
		count := 0
		for range wins { count++ }
		if count != 1 { t.Fatalf("terminal winners=%d want 1", count) }
		if table.len() != 0 { t.Fatal("pending entry leaked") }
		select {
		case <-request.done:
		default:
			t.Fatal("winner did not complete caller")
		}
	}
}

func TestPendingBound(t *testing.T) {
	table := newPendingTable(1)
	request := &pendingRequest{done: make(chan requestResult, 1)}
	if err := table.insert(1, request); err != nil { t.Fatal(err) }
	if err := table.insert(2, &pendingRequest{done: make(chan requestResult, 1)}); !errors.Is(err, ErrPendingLimit) {
		t.Fatalf("expected ErrPendingLimit, got %v", err)
	}
}
