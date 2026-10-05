package server_test

import (
	"context"
	"errors"
	"net"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/majiddarvishan/go-smpp/client"
	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/server"
	"github.com/majiddarvishan/go-smpp/session"
)

// ---- the default throttle, driven directly with a fake clock ----

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func attemptFrom(ip string, port int) server.BindAttempt {
	return server.BindAttempt{RemoteAddr: &net.TCPAddr{IP: net.ParseIP(ip), Port: port}, SystemID: "esme", Mode: session.BindTRX}
}

func newThrottle(c *fakeClock, cfg server.BindThrottleConfig) server.BindRateLimiter {
	cfg.Now = c.now
	return server.NewBindThrottle(cfg)
}

func fail(l server.BindRateLimiter, a server.BindAttempt, n int) {
	for i := 0; i < n; i++ {
		l.RecordBind(a, false)
	}
}

func TestBindThrottleEngagesAtMaxFailuresAndOnlyForThatIP(t *testing.T) {
	c := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	l := newThrottle(c, server.BindThrottleConfig{MaxFailures: 3, Window: time.Minute, Lockout: time.Minute})
	bad := attemptFrom("203.0.113.9", 40001)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if !l.AllowBind(ctx, bad) {
			t.Fatalf("attempt %d refused before reaching the threshold", i+1)
		}
		l.RecordBind(bad, false)
	}
	if l.AllowBind(ctx, bad) {
		t.Fatal("throttle did not engage after MaxFailures rejected binds")
	}
	if l.AllowBind(ctx, attemptFrom("203.0.113.9", 51234)) {
		t.Fatal("a new source port from the same IP escaped the lockout")
	}
	if !l.AllowBind(ctx, attemptFrom("198.51.100.7", 40001)) {
		t.Fatal("an unrelated IP was locked out")
	}
}

func TestBindThrottleBelowThresholdNeverRefusesAndSuccessDoesNotCount(t *testing.T) {
	c := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	l := newThrottle(c, server.BindThrottleConfig{MaxFailures: 3})
	a := attemptFrom("203.0.113.9", 1)
	ctx := context.Background()
	for i := 0; i < 10_000; i++ {
		if !l.AllowBind(ctx, a) {
			t.Fatalf("legitimate bind %d refused", i)
		}
		l.RecordBind(a, true)
	}
	// two failures stay below the threshold of three, whatever successes surround them
	fail(l, a, 2)
	l.RecordBind(a, true)
	if !l.AllowBind(ctx, a) {
		t.Fatal("locked out below the threshold")
	}
}

func TestBindThrottleSuccessDoesNotResetFailureCount(t *testing.T) {
	c := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	l := newThrottle(c, server.BindThrottleConfig{MaxFailures: 3})
	a := attemptFrom("203.0.113.9", 1)
	// An attacker holding one valid credential must not be able to buy
	// themselves fresh guesses by interleaving it.
	l.RecordBind(a, false)
	l.RecordBind(a, false)
	l.RecordBind(a, true)
	l.RecordBind(a, false)
	if l.AllowBind(context.Background(), a) {
		t.Fatal("a successful bind reset the failure count")
	}
}

func TestBindThrottleLockoutExpires(t *testing.T) {
	c := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	l := newThrottle(c, server.BindThrottleConfig{MaxFailures: 2, Window: time.Minute, Lockout: 30 * time.Second})
	a := attemptFrom("203.0.113.9", 1)
	fail(l, a, 2)
	if l.AllowBind(context.Background(), a) {
		t.Fatal("not locked")
	}
	c.advance(29 * time.Second)
	if l.AllowBind(context.Background(), a) {
		t.Fatal("lockout ended early")
	}
	c.advance(2 * time.Second)
	if !l.AllowBind(context.Background(), a) {
		t.Fatal("lockout did not expire")
	}
	// and the slate is clean: it takes a full MaxFailures again
	l.RecordBind(a, false)
	if !l.AllowBind(context.Background(), a) {
		t.Fatal("one failure after a served lockout re-locked immediately")
	}
}

func TestBindThrottleFailuresExpireWithTheWindow(t *testing.T) {
	c := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	l := newThrottle(c, server.BindThrottleConfig{MaxFailures: 3, Window: time.Minute, Lockout: time.Minute})
	a := attemptFrom("203.0.113.9", 1)
	fail(l, a, 2)
	c.advance(61 * time.Second) // the two failures are now stale
	fail(l, a, 2)               // would be 4 total if the window did not reset
	if !l.AllowBind(context.Background(), a) {
		t.Fatal("stale failures were counted toward the lockout")
	}
}

func TestBindThrottleMemoryIsBounded(t *testing.T) {
	c := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	const max = 50
	l := newThrottle(c, server.BindThrottleConfig{MaxFailures: 1, Lockout: time.Hour, Window: time.Hour, MaxTracked: max})
	ctx := context.Background()
	// An attacker rotating through many source addresses must not grow the
	// table without bound. Observable consequence: the oldest locked-out IPs
	// are forgotten once more than MaxTracked have failed.
	for i := 0; i < max*4; i++ {
		c.advance(time.Second)
		l.RecordBind(attemptFrom("10.0."+itoa(i/250)+"."+itoa(i%250+1), 1), false)
	}
	locked := 0
	for i := 0; i < max*4; i++ {
		if !l.AllowBind(ctx, attemptFrom("10.0."+itoa(i/250)+"."+itoa(i%250+1), 1)) {
			locked++
		}
	}
	if locked > max {
		t.Fatalf("%d IPs remembered, want at most MaxTracked=%d", locked, max)
	}
	if locked == 0 {
		t.Fatal("nothing remembered at all; eviction is dropping everything")
	}
	// Eviction must take the OLDEST entry: the last MaxTracked offenders are
	// exactly the ones still remembered, and the very first is forgotten.
	for i := max * 3; i < max*4; i++ {
		if l.AllowBind(ctx, attemptFrom("10.0."+itoa(i/250)+"."+itoa(i%250+1), 1)) {
			t.Fatalf("recent offender #%d was evicted ahead of older ones", i)
		}
	}
	if !l.AllowBind(ctx, attemptFrom("10.0.0.1", 1)) {
		t.Fatal("the oldest offender is still remembered; eviction is not oldest-first")
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestBindThrottleIsSafeForConcurrentUse(t *testing.T) {
	l := server.NewBindThrottle(server.BindThrottleConfig{MaxFailures: 5, MaxTracked: 16})
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 2000; i++ {
				a := attemptFrom("192.0.2."+itoa((g*7+i)%40+1), i)
				l.AllowBind(context.Background(), a)
				l.RecordBind(a, i%3 == 0)
			}
		}(g)
	}
	wg.Wait() // meaningful under -race
}

// A pluggable limiter must never be handed a credential.
func TestBindAttemptCarriesNoCredentials(t *testing.T) {
	typ := reflect.TypeOf(server.BindAttempt{})
	for i := 0; i < typ.NumField(); i++ {
		if k := typ.Field(i).Type.Kind(); k == reflect.Slice || k == reflect.Array {
			t.Fatalf("BindAttempt.%s is a %s; byte-like fields could carry the password", typ.Field(i).Name, k)
		}
	}
	if _, ok := typ.FieldByName("Password"); ok {
		t.Fatal("BindAttempt has a Password field")
	}
}

// ---- through a real server and client ----

type bindFixture struct {
	srv        *server.Server
	ctx        context.Context
	authCalls  atomic.Uint64
	goodSecret string
}

func newBindFixture(t *testing.T, cfg server.Config) *bindFixture {
	t.Helper()
	f := &bindFixture{goodSecret: "secret"}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	f.ctx = ctx
	cfg.Address = "127.0.0.1:0"
	if cfg.SessionConfig.ResponseTimeout == 0 {
		cfg.SessionConfig = quietSessionConfig()
	}
	if cfg.Authenticator == nil {
		cfg.Authenticator = server.AuthenticatorFunc(func(_ context.Context, _ *session.Session, _ session.BindMode, r protocol.BindRequest) (server.BindResult, error) {
			f.authCalls.Add(1)
			if string(r.Password) != f.goodSecret {
				return server.BindResult{Status: protocol.StatusInvalidPassword}, nil
			}
			return server.BindResult{Status: protocol.StatusOK, SystemID: []byte("smsc")}, nil
		})
	}
	srv, err := server.Listen(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	startServer(t, ctx, srv)
	f.srv = srv
	return f
}

// bindOnce dials a fresh connection and binds, returning the peer's status
// (StatusOK on success) and how long the bind took.
func (f *bindFixture) bindOnce(t *testing.T, password string) (protocol.CommandStatus, time.Duration) {
	t.Helper()
	cli, err := client.Dial(f.ctx, client.Config{Address: f.srv.Addr().String(), SessionConfig: quietSessionConfig()})
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	start := time.Now()
	_, err = cli.BindTransceiver(f.ctx, protocol.BindRequest{SystemID: []byte("esme"), Password: []byte(password), InterfaceVersion: protocol.InterfaceVersion34})
	took := time.Since(start)
	if err == nil {
		return protocol.StatusOK, took
	}
	var re *session.ResponseError
	if !errors.As(err, &re) {
		t.Fatalf("bind failed with a non-protocol error: %v", err)
	}
	return re.Status, took
}

func TestServerThrottleEngagesAfterRepeatedFailedBinds(t *testing.T) {
	f := newBindFixture(t, server.Config{BindRateLimiter: server.NewBindThrottle(server.BindThrottleConfig{MaxFailures: 3, Lockout: time.Minute})})

	for i := 0; i < 3; i++ {
		if st, _ := f.bindOnce(t, "wrong"); st != protocol.StatusInvalidPassword {
			t.Fatalf("guess %d: status %v, want the authenticator's rejection", i+1, st)
		}
	}
	callsAtLockout := f.authCalls.Load()
	if callsAtLockout != 3 {
		t.Fatalf("authenticator called %d times, want 3", callsAtLockout)
	}

	// Now locked out: even the CORRECT password is refused, and the
	// authenticator is not consulted at all, so a guess costs the attacker the
	// lockout and costs the operator's credential store nothing.
	for i := 0; i < 5; i++ {
		if st, _ := f.bindOnce(t, f.goodSecret); st != protocol.StatusThrottled {
			t.Fatalf("locked-out bind %d: status %v, want ESME_RTHROTTLED", i+1, st)
		}
	}
	if got := f.authCalls.Load(); got != callsAtLockout {
		t.Fatalf("authenticator was invoked %d more times while locked out", got-callsAtLockout)
	}

	stats := f.srv.BindStats()
	if stats.Attempts != 8 || stats.Failures != 3 || stats.Throttled != 5 {
		t.Fatalf("BindStats = %+v, want {Attempts:8 Failures:3 Throttled:5}", stats)
	}
}

func TestServerLegitimateBindsUnaffectedBelowThreshold(t *testing.T) {
	f := newBindFixture(t, server.Config{BindRateLimiter: server.NewBindThrottle(server.BindThrottleConfig{MaxFailures: 5})})
	// two typos, then a long run of good binds from the same address
	for i := 0; i < 2; i++ {
		if st, _ := f.bindOnce(t, "typo"); st != protocol.StatusInvalidPassword {
			t.Fatalf("status %v", st)
		}
	}
	for i := 0; i < 60; i++ {
		if st, _ := f.bindOnce(t, f.goodSecret); st != protocol.StatusOK {
			t.Fatalf("legitimate bind %d refused with %v below the threshold", i+1, st)
		}
	}
	if s := f.srv.BindStats(); s.Throttled != 0 || s.Failures != 2 || s.Attempts != 62 {
		t.Fatalf("BindStats = %+v", s)
	}
}

func TestServerWithoutLimiterNeverThrottles(t *testing.T) {
	f := newBindFixture(t, server.Config{})
	for i := 0; i < 20; i++ {
		if st, _ := f.bindOnce(t, "wrong"); st != protocol.StatusInvalidPassword {
			t.Fatalf("status %v, want the authenticator's rejection every time", st)
		}
	}
	if s := f.srv.BindStats(); s.Throttled != 0 || s.Failures != 20 {
		t.Fatalf("BindStats = %+v", s)
	}
}

// Policy pinned: an Authenticator *error* is an infrastructure fault, not a
// guess, so it never counts toward a lockout. (Authenticators should report a
// bad credential through BindResult.Status, not an error.)
func TestServerAuthenticatorErrorsDoNotCountTowardLockout(t *testing.T) {
	f := newBindFixture(t, server.Config{
		BindRateLimiter: server.NewBindThrottle(server.BindThrottleConfig{MaxFailures: 2}),
		Authenticator: server.AuthenticatorFunc(func(context.Context, *session.Session, session.BindMode, protocol.BindRequest) (server.BindResult, error) {
			return server.BindResult{}, errors.New("credential store unavailable")
		}),
	})
	for i := 0; i < 6; i++ {
		if st, _ := f.bindOnce(t, "anything"); st != protocol.StatusSystemError {
			t.Fatalf("bind %d: status %v, want ESME_RSYSERR and no throttling", i+1, st)
		}
	}
	if s := f.srv.BindStats(); s.Throttled != 0 {
		t.Fatalf("BindStats = %+v", s)
	}
}

func TestServerFailureDelayAppliesToFailuresOnly(t *testing.T) {
	const delay = 400 * time.Millisecond
	f := newBindFixture(t, server.Config{BindFailureDelay: delay})
	if st, took := f.bindOnce(t, "wrong"); st != protocol.StatusInvalidPassword || took < delay {
		t.Fatalf("failed bind: status %v after %v, want a rejection no sooner than %v", st, took, delay)
	}
	if st, took := f.bindOnce(t, f.goodSecret); st != protocol.StatusOK || took >= delay {
		t.Fatalf("good bind: status %v after %v, want success well under %v", st, took, delay)
	}
}

func TestServerFailureDelayAlsoAppliesToThrottledBinds(t *testing.T) {
	const delay = 300 * time.Millisecond
	f := newBindFixture(t, server.Config{
		BindFailureDelay: delay,
		BindRateLimiter:  server.NewBindThrottle(server.BindThrottleConfig{MaxFailures: 1, Lockout: time.Minute}),
	})
	f.bindOnce(t, "wrong") // locks the IP
	if st, took := f.bindOnce(t, "wrong"); st != protocol.StatusThrottled || took < delay {
		t.Fatalf("throttled bind: status %v after %v, want ESME_RTHROTTLED no sooner than %v", st, took, delay)
	}
}

func TestServerFailureDelayDoesNotBlockOtherSessionsOrOutliveTheSession(t *testing.T) {
	f := newBindFixture(t, server.Config{BindFailureDelay: 30 * time.Second})

	// A guesser sends a bad bind and waits out the delay; meanwhile the SMSC
	// must keep serving everyone else.
	cli, err := client.Dial(f.ctx, client.Config{Address: f.srv.Addr().String(), SessionConfig: quietSessionConfig()})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		_, _ = cli.BindTransceiver(f.ctx, protocol.BindRequest{SystemID: []byte("esme"), Password: []byte("wrong"), InterfaceVersion: protocol.InterfaceVersion34})
	}()
	waitSessions(t, f.srv, 1, time.Second)
	for f.authCalls.Load() == 0 { // the delayed handler is now parked
		time.Sleep(time.Millisecond)
	}

	if st, took := f.bindOnce(t, f.goodSecret); st != protocol.StatusOK || took > 2*time.Second {
		t.Fatalf("a bystander's bind: status %v after %v while another session sat in its failure delay", st, took)
	}

	// Closing the guesser's connection must release the parked handler and
	// the session promptly, not after the 30s delay.
	_ = cli.Close()
	waitSessions(t, f.srv, 0, 3*time.Second)
}
