package server

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/session"
)

// BindAttempt describes one inbound bind for rate-limiting decisions. It
// deliberately carries no password: a pluggable limiter has no business
// seeing credentials, and cannot leak what it never receives.
type BindAttempt struct {
	RemoteAddr net.Addr
	SystemID   string
	Mode       session.BindMode
}

// BindRateLimiter is an optional policy hook consulted around every bind that
// would reach the Authenticator. The library ships one reasonable policy
// (NewBindThrottle) but deployments differ - per-IP, per-system_id, shared
// state across nodes, an upstream blocklist - so the policy is an interface.
//
// Implementations must be safe for concurrent use and must not block for long:
// AllowBind runs on the session's receive path.
type BindRateLimiter interface {
	// AllowBind is called before the Authenticator. Returning false rejects the
	// bind with ESME_RTHROTTLED without invoking the Authenticator.
	AllowBind(ctx context.Context, attempt BindAttempt) bool
	// RecordBind is called after the Authenticator returned a definite answer:
	// success is true for an accepted bind and false for a rejected one. It is
	// not called for throttled attempts, nor when the Authenticator itself
	// returned an error (an infrastructure fault is not evidence of a guess,
	// and must not lock out legitimate peers).
	RecordBind(attempt BindAttempt, success bool)
}

// BindStats is a snapshot of bind-attempt counters, intended for alarming: a
// rising Failures or Throttled rate is the signature of credential guessing.
type BindStats struct {
	Attempts  uint64 // binds that reached the server's bind path (authenticated or throttled)
	Failures  uint64 // binds the Authenticator rejected
	Throttled uint64 // binds rejected by the BindRateLimiter before authentication
}

// bindGuard is the server-side wiring shared by every session's handler.
type bindGuard struct {
	limiter BindRateLimiter
	delay   time.Duration

	attempts  atomic.Uint64
	failures  atomic.Uint64
	throttled atomic.Uint64
}

func (g *bindGuard) stats() BindStats {
	if g == nil {
		return BindStats{}
	}
	return BindStats{Attempts: g.attempts.Load(), Failures: g.failures.Load(), Throttled: g.throttled.Load()}
}

func attemptFor(sess *session.Session, mode session.BindMode, request protocol.BindRequest) BindAttempt {
	return BindAttempt{RemoteAddr: sess.RemoteAddr(), SystemID: string(request.SystemID), Mode: mode}
}

// wait pauses for the configured failure delay, returning early if the
// session is torn down. The delay holds only the offending session's receive
// loop: other sessions are unaffected.
func (g *bindGuard) wait(ctx context.Context) {
	if g == nil || g.delay <= 0 {
		return
	}
	timer := time.NewTimer(g.delay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}

// BindThrottleConfig configures the default per-remote-IP bind throttle.
type BindThrottleConfig struct {
	// MaxFailures is the number of rejected binds from one IP within Window
	// that triggers a lockout. Zero means 5.
	MaxFailures int
	// Window is how long a failure counts. Zero means one minute.
	Window time.Duration
	// Lockout is how long an IP is refused once it reaches MaxFailures. Zero
	// means one minute.
	Lockout time.Duration
	// MaxTracked bounds the number of distinct IPs remembered, so the table
	// cannot be used to exhaust memory. Zero means 10000. When full, the entry
	// whose last failure is oldest is evicted.
	MaxTracked int
	// Now overrides the clock. It exists for tests; leave it nil.
	Now func() time.Time
}

// NewBindThrottle returns a BindRateLimiter that locks out a remote IP after
// MaxFailures rejected binds within Window.
//
// Deliberate properties, each a policy choice you may want to replace:
//   - Keyed by remote IP only (port ignored). Peers behind one NAT share a
//     budget; a deployment with many ESMEs behind one address should raise
//     MaxFailures or supply its own limiter.
//   - A successful bind does not clear an IP's failures. Clearing on success
//     would let an attacker holding any one valid credential reset the counter
//     between guesses.
//   - Failures expire by Window; a lockout expires by Lockout. Nothing is
//     permanent.
//   - Legitimate throughput is untouched below the threshold: AllowBind is a
//     map lookup under one mutex.
func NewBindThrottle(config BindThrottleConfig) BindRateLimiter {
	if config.MaxFailures <= 0 {
		config.MaxFailures = 5
	}
	if config.Window <= 0 {
		config.Window = time.Minute
	}
	if config.Lockout <= 0 {
		config.Lockout = time.Minute
	}
	if config.MaxTracked <= 0 {
		config.MaxTracked = 10000
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &bindThrottle{config: config, entries: make(map[string]*throttleEntry)}
}

type throttleEntry struct {
	failures    int
	windowStart time.Time
	lastFailure time.Time
	lockedUntil time.Time
}

type bindThrottle struct {
	config  BindThrottleConfig
	mu      sync.Mutex
	entries map[string]*throttleEntry
}

func remoteHost(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	return host
}

func (t *bindThrottle) AllowBind(_ context.Context, attempt BindAttempt) bool {
	key := remoteHost(attempt.RemoteAddr)
	now := t.config.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.entries[key]
	if !ok {
		return true
	}
	if now.Before(e.lockedUntil) {
		return false
	}
	if !e.lockedUntil.IsZero() || now.Sub(e.windowStart) >= t.config.Window {
		// lockout served, or the failure window lapsed: forget this IP.
		delete(t.entries, key)
	}
	return true
}

func (t *bindThrottle) RecordBind(attempt BindAttempt, success bool) {
	if success {
		return
	}
	key := remoteHost(attempt.RemoteAddr)
	now := t.config.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.entries[key]
	if !ok {
		if len(t.entries) >= t.config.MaxTracked {
			t.evictLocked(now)
		}
		e = &throttleEntry{windowStart: now}
		t.entries[key] = e
	}
	if now.Sub(e.windowStart) >= t.config.Window {
		e.failures, e.windowStart = 0, now
	}
	e.failures++
	e.lastFailure = now
	if e.failures >= t.config.MaxFailures {
		e.lockedUntil = now.Add(t.config.Lockout)
	}
}

// evictLocked frees one slot: expired entries first, otherwise the entry with
// the oldest last failure. t.mu must be held.
func (t *bindThrottle) evictLocked(now time.Time) {
	var oldestKey string
	var oldest time.Time
	first := true
	for k, e := range t.entries {
		expired := !now.Before(e.lockedUntil) && now.Sub(e.windowStart) >= t.config.Window
		if expired {
			delete(t.entries, k)
			return
		}
		if first || e.lastFailure.Before(oldest) {
			oldestKey, oldest, first = k, e.lastFailure, false
		}
	}
	delete(t.entries, oldestKey)
}
