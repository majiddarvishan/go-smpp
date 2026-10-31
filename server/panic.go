package server

import (
	"context"
	"runtime/debug"

	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/session"
)

// The server's own callbacks (Authenticator, BindRateLimiter, SubmitHandler)
// run on the session's receive goroutine. A panic in one fails only the PDU
// that triggered it: the bind or submit_sm is refused with ESME_RSYSERR, the
// bind failure delay still applies, and the session and process stay up. The
// *protocol.PanicError is returned to the session as the Handler error, which
// is where it is counted, logged and emitted as session.EventPanicRecovered.

func newPanicError(source string, value any) *protocol.PanicError {
	return &protocol.PanicError{Source: source, Value: value, Stack: debug.Stack()}
}

func authenticate(ctx context.Context, a Authenticator, sess *session.Session, mode session.BindMode, request protocol.BindRequest) (result BindResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			result, err = BindResult{Status: protocol.StatusSystemError}, newPanicError("server.Authenticator", r)
		}
	}()
	return a.Authenticate(ctx, sess, mode, request)
}

// allowBind fails closed: a limiter that panics refuses the bind.
func allowBind(ctx context.Context, l BindRateLimiter, attempt BindAttempt) (allowed bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			allowed, err = false, newPanicError("server.BindRateLimiter", r)
		}
	}()
	return l.AllowBind(ctx, attempt), nil
}

func recordBind(l BindRateLimiter, attempt BindAttempt, succeeded bool) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = newPanicError("server.BindRateLimiter", r)
		}
	}()
	l.RecordBind(attempt, succeeded)
	return nil
}

func submit(ctx context.Context, h SubmitHandler, sess *session.Session, request protocol.SubmitSM) (result SubmitResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			result, err = SubmitResult{Status: protocol.StatusSystemError}, newPanicError("server.SubmitHandler", r)
		}
	}()
	return h.SubmitSM(ctx, sess, request)
}

// panicOnly returns err if it is a recovered panic and nil otherwise. The
// dispatch handler answers an ordinary Authenticator or SubmitHandler error
// itself, with the status the callback chose; only a panic is passed up to
// the session, to be reported.
func panicOnly(err error) error {
	if p, ok := err.(*protocol.PanicError); ok {
		return p
	}
	return nil
}
