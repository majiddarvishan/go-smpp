package session

import "testing"

func TestRequestCompletionPoolReusesAndDrainsChannels(t *testing.T) {
	s := &Session{completions: make(chan chan requestResult, 1)}
	first := s.acquireRequestCompletion()
	first <- requestResult{err: ErrSessionClosed}
	s.releaseRequestCompletion(first)

	second := s.acquireRequestCompletion()
	if second != first {
		t.Fatal("completion channel was not reused")
	}
	select {
	case <-second:
		t.Fatal("reused completion channel retained a stale result")
	default:
	}
	s.releaseRequestCompletion(second)
}
