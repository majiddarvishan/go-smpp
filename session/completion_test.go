package session

import (
	"errors"
	"sync"
	"testing"

	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/protocol"
)

func TestPendingCompletionExactlyOnceWithSessionLoss(t *testing.T) {
	for i := 0; i < 100; i++ {
		table := newPendingTable(1)
		request := &pendingRequest{
			requestID:  protocol.CommandSubmitSM,
			expectedID: protocol.CommandSubmitSMResp,
			done:       make(chan requestResult, 1),
		}
		if err := table.insert(7, request); err != nil {
			t.Fatal(err)
		}

		var wg sync.WaitGroup
		wg.Add(4)
		go func() {
			defer wg.Done()
			if pending, ok := table.takeResponse(7, protocol.CommandSubmitSMResp); ok {
				pending.done <- requestResult{pdu: codec.DecodedPDU{Header: codec.Header{CommandID: protocol.CommandSubmitSMResp, SequenceNumber: 7}}}
			}
		}()
		go func() {
			defer wg.Done()
			_, _ = table.completeError(7, &TimeoutError{Kind: TimeoutResponse, Command: protocol.CommandSubmitSM, Sequence: 7})
		}()
		go func() {
			defer wg.Done()
			_, _ = table.completeError(7, errors.New("caller cancelled"))
		}()
		go func() {
			defer wg.Done()
			table.failAll(&LossError{Cause: &protocol.FatalError{Kind: protocol.FatalInvalidCommandLength}})
		}()
		wg.Wait()

		select {
		case <-request.done:
		default:
			t.Fatal("no terminal completion won")
		}
		select {
		case extra := <-request.done:
			t.Fatalf("request completed more than once: %+v", extra)
		default:
		}
		if table.len() != 0 {
			t.Fatalf("pending=%d after terminal race", table.len())
		}
	}
}
