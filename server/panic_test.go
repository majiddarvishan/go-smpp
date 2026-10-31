package server_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/majiddarvishan/go-smpp/client"
	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/server"
	"github.com/majiddarvishan/go-smpp/session"
)

// A panic in the server's own callbacks (Authenticator, BindRateLimiter,
// SubmitHandler) fails only the PDU that triggered it, keeps the bind failure
// delay, and is reported once through the session's panic channel.

type panicEvents struct {
	mu      sync.Mutex
	reasons []string
}

func (p *panicEvents) OnEvent(e session.Event) {
	if e.Kind != session.EventPanicRecovered {
		return
	}
	p.mu.Lock()
	p.reasons = append(p.reasons, e.Reason)
	p.mu.Unlock()
}

func (p *panicEvents) waitFor(t *testing.T, source string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		for _, r := range p.reasons {
			if strings.Contains(r, source) {
				p.mu.Unlock()
				return
			}
		}
		p.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	t.Fatalf("no EventPanicRecovered naming %s; got %q", source, p.reasons)
}

func panicTestConfig(events *panicEvents) server.Config {
	cfg := quietSessionConfig()
	cfg.Observer = events
	cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	return server.Config{SessionConfig: cfg}
}

type panickingLimiter struct{ onAllow, onRecord bool }

func (l panickingLimiter) AllowBind(context.Context, server.BindAttempt) bool {
	if l.onAllow {
		panic("limiter AllowBind exploded")
	}
	return true
}

func (l panickingLimiter) RecordBind(server.BindAttempt, bool) {
	if l.onRecord {
		panic("limiter RecordBind exploded")
	}
}

func TestAuthenticatorPanicRefusesOnlyThatBindAndKeepsTheDelay(t *testing.T) {
	const delay = 80 * time.Millisecond
	events := &panicEvents{}
	cfg := panicTestConfig(events)
	cfg.BindFailureDelay = delay
	cfg.Authenticator = server.AuthenticatorFunc(func(context.Context, *session.Session, session.BindMode, protocol.BindRequest) (server.BindResult, error) {
		panic("authenticator exploded")
	})
	f := newBindFixture(t, cfg)

	status, took := f.bindOnce(t, "secret")
	if status != protocol.StatusSystemError {
		t.Fatalf("bind status 0x%08x, want ESME_RSYSERR", uint32(status))
	}
	if took < delay {
		t.Fatalf("bind refused after %v; a panicking authenticator must not skip the %v failure delay", took, delay)
	}
	events.waitFor(t, "server.Authenticator")
	// The server is still serving.
	if status, _ := f.bindOnce(t, "secret"); status != protocol.StatusSystemError {
		t.Fatalf("second bind status 0x%08x, want ESME_RSYSERR", uint32(status))
	}
}

func TestBindRateLimiterPanicFailsClosed(t *testing.T) {
	for name, limiter := range map[string]panickingLimiter{
		"AllowBind":  {onAllow: true},
		"RecordBind": {onRecord: true},
	} {
		t.Run(name, func(t *testing.T) {
			events := &panicEvents{}
			cfg := panicTestConfig(events)
			cfg.BindRateLimiter = limiter
			f := newBindFixture(t, cfg)
			// The right password: only the limiter's panic can refuse it.
			if status, _ := f.bindOnce(t, f.goodSecret); status != protocol.StatusSystemError {
				t.Fatalf("bind status 0x%08x, want ESME_RSYSERR", uint32(status))
			}
			events.waitFor(t, "server.BindRateLimiter")
		})
	}
}

func TestSubmitHandlerPanicFailsOnlyThatSubmit(t *testing.T) {
	events := &panicEvents{}
	cfg := panicTestConfig(events)
	cfg.SubmitHandler = server.SubmitHandlerFunc(func(context.Context, *session.Session, protocol.SubmitSM) (server.SubmitResult, error) {
		panic("submit handler exploded")
	})
	f := newBindFixture(t, cfg)

	cli, err := client.Dial(f.ctx, client.Config{Address: f.srv.Addr().String(), SessionConfig: quietSessionConfig()})
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	if _, err := cli.BindTransceiver(f.ctx, protocol.BindRequest{SystemID: []byte("esme"), Password: []byte(f.goodSecret), InterfaceVersion: protocol.InterfaceVersion34}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		_, err := cli.SubmitSM(f.ctx, protocol.SubmitSM{DestinationAddr: []byte("1"), ShortMessage: []byte("x")})
		var re *session.ResponseError
		if !errors.As(err, &re) || re.Status != protocol.StatusSystemError {
			t.Fatalf("submit %d: err = %v, want ESME_RSYSERR", i, err)
		}
	}
	if err := cli.EnquireLink(f.ctx); err != nil {
		t.Fatalf("session unusable after submit handler panics: %v", err)
	}
	events.waitFor(t, "server.SubmitHandler")
}

func TestNilServerCallbacksAndReceiverDoNotPanic(t *testing.T) {
	ctx := context.Background()
	var auth server.AuthenticatorFunc
	if result, err := auth.Authenticate(ctx, nil, session.BindTRX, protocol.BindRequest{}); err != nil || result.Status != protocol.StatusBindFailed {
		t.Errorf("nil AuthenticatorFunc = %+v, %v; want ESME_RBINDFAIL", result, err)
	}
	var submit server.SubmitHandlerFunc
	if result, err := submit.SubmitSM(ctx, nil, protocol.SubmitSM{}); err != nil || result.Status != protocol.StatusSystemError {
		t.Errorf("nil SubmitHandlerFunc = %+v, %v; want ESME_RSYSERR", result, err)
	}
	var srv *server.Server
	if _, err := srv.DeliverSM(ctx, &session.Session{}, protocol.DeliverSM{}); !errors.Is(err, server.ErrUnknownSession) {
		t.Errorf("nil Server DeliverSM = %v, want ErrUnknownSession", err)
	}
}
