package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"

	"github.com/majiddarvishan/go-smpp/protocol"
)

// The library never lets a panic escape into the host process. Each piece of
// caller-supplied code that runs on a session goroutine is called through a
// recover, and each long-lived session goroutine has a last-resort recover of
// its own. A recovered panic is counted in MetricsSnapshot.PanicsRecovered,
// logged with its stack and emitted as EventPanicRecovered, and its effect is
// kept as small as possible:
//
//   - a Handler panic fails only that PDU: a request is answered with
//     ESME_RSYSERR and the session stays up;
//   - a registry CommandDecoder or CommandEncoder panic also fails only that
//     PDU: an inbound one is answered with generic_nack ESME_RSYSERR, and an
//     outbound one returns the *protocol.PanicError to the caller;
//   - an Observer, PacketTracer, FlowController, WindowObserver or Logger panic
//     drops only that one notification;
//   - a panic anywhere else on a session goroutine (a caller-supplied net.Conn,
//     or a defect in this library) ends that session with a
//     *protocol.PanicError as its Err, and leaves every other session and the
//     process running.
//
// Each Source names what panicked.
const (
	sourceHandler        = "session.Handler"
	sourceObserver       = "session.Observer"
	sourcePacketTracer   = "session.PacketTracer"
	sourceFlowController = "session.FlowController"
	sourceWindowObserver = "session.WindowObserver"
	sourceLogger         = "session.Logger"
	sourceConn           = "session.Conn"
	sourceRXLoop         = "session.rxLoop"
	sourceTXLoop         = "session.txLoop"
	sourceDeadlines      = "session.deadlines"
	sourceEnquireLink    = "session.enquireLink"
)

// closedDone is what Done returns for a Session that was not created by New,
// so a caller selecting on it never blocks forever.
var closedDone = func() chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}()

// initialized reports whether s was created by New. Every exported method
// checks it first, so a nil or zero-value Session returns ErrNotInitialized
// or a zero value instead of panicking.
func (s *Session) initialized() bool { return s != nil && s.done != nil }

func newPanicError(source string, value any) *protocol.PanicError {
	return &protocol.PanicError{Source: source, Value: value, Stack: debug.Stack()}
}

// recoverCallback is deferred around a call into caller-supplied code whose
// panic should cost only that one call.
func (s *Session) recoverCallback(source string) {
	if r := recover(); r != nil {
		s.reportPanic(newPanicError(source, r))
	}
}

// recoverLoop is deferred at the top of every session goroutine as the last
// resort: whatever panicked there, that session ends with the panic as its
// error, and nothing else is affected.
func (s *Session) recoverLoop(source string) {
	if r := recover(); r != nil {
		p := newPanicError(source, r)
		s.reportPanic(p)
		s.terminate(p)
	}
}

// guard runs fn, recovering a panic from it. It is for the few rare-path calls
// into caller-supplied code that must not stop the caller from finishing, such
// as closing the connection during terminate.
func (s *Session) guard(source string, fn func()) {
	defer s.recoverCallback(source)
	fn()
}

// reportPanic records a recovered panic. It must not panic itself: the logger
// is wrapped by panicSafeHandler, and emitEvent recovers an Observer panic.
// An Observer that panicked is not told about its own panic, which would only
// recurse.
func (s *Session) reportPanic(p *protocol.PanicError) {
	s.metrics.panicsRecovered.Add(1)
	if p.Source != sourceLogger {
		s.logger.Error("recovered panic; the process keeps running",
			"event", "smpp_panic_recovered",
			"source", p.Source,
			"panic", fmt.Sprint(p.Value),
			"stack", string(p.Stack))
	}
	if p.Source != sourceObserver {
		s.emitEvent(Event{Kind: EventPanicRecovered, Reason: p.Error()})
	}
}

// reportIfPanic reports err when it carries a recovered panic, such as one
// from a registry's CommandEncoder or CommandDecoder. Call it only on error
// paths: the errors.As target is a heap allocation.
func (s *Session) reportIfPanic(err error) {
	var p *protocol.PanicError
	if errors.As(err, &p) {
		s.reportPanic(p)
	}
}

// isPanic reports whether err carries a recovered panic. Like reportIfPanic,
// it allocates, so the hot path guards it with err != nil.
func isPanic(err error) bool {
	var p *protocol.PanicError
	return errors.As(err, &p)
}

// callHandler runs the configured Handler. A panic is returned as a
// *protocol.PanicError, which the caller reports and answers like any other
// Handler error.
func (s *Session) callHandler(pdu InboundPDU) (response Response, err error) {
	defer func() {
		if r := recover(); r != nil {
			response, err = Response{}, newPanicError(sourceHandler, r)
		}
	}()
	return s.config.Handler.Handle(s.ctx, s, pdu)
}

// panicSafeHandler wraps the configured logger's handler so a panicking
// slog.Handler drops that one record instead of crashing the session goroutine
// that logged it.
type panicSafeHandler struct {
	inner   slog.Handler
	onPanic func(*protocol.PanicError)
}

func newPanicSafeLogger(logger *slog.Logger, onPanic func(*protocol.PanicError)) *slog.Logger {
	return slog.New(panicSafeHandler{inner: logger.Handler(), onPanic: onPanic})
}

func (h panicSafeHandler) recoverRecord() {
	if r := recover(); r != nil {
		h.onPanic(newPanicError(sourceLogger, r))
	}
}

func (h panicSafeHandler) Enabled(ctx context.Context, level slog.Level) (enabled bool) {
	defer h.recoverRecord()
	return h.inner.Enabled(ctx, level)
}

func (h panicSafeHandler) Handle(ctx context.Context, record slog.Record) (err error) {
	defer h.recoverRecord()
	return h.inner.Handle(ctx, record)
}

func (h panicSafeHandler) WithAttrs(attrs []slog.Attr) (out slog.Handler) {
	out = h
	defer h.recoverRecord()
	return panicSafeHandler{inner: h.inner.WithAttrs(attrs), onPanic: h.onPanic}
}

func (h panicSafeHandler) WithGroup(name string) (out slog.Handler) {
	out = h
	defer h.recoverRecord()
	return panicSafeHandler{inner: h.inner.WithGroup(name), onPanic: h.onPanic}
}
