# Project decisions

Short records of choices that were considered and deliberately settled, so that
they are documented rather than implicit and a later contributor can see what
was weighed before reopening one. `docs/API_COMPATIBILITY.md` says an
incompatible API correction must be "recorded in project decisions" before a
release tag; this is the file for that. Newest last.

Each record has: status, context, decision, why, what it costs, and when to
revisit it.

---

## D1 — Typed request/response wrappers on `session.Session` stay explicit

- **Status:** accepted, 2026-10-05 (review finding C5, Task 4.5).
- **Applies to:** `session/operations.go`.

### Context

`Session` exposes 17 exported operation methods. They fall into groups that
are not all the same shape:

| Group | Methods | Shape |
| --- | --- | --- |
| Request, typed response | `SubmitSM`, `DeliverSM`, `DataSM`, `SubmitMulti`, `QuerySM`, `BroadcastSM`, `QueryBroadcastSM` | `Request`, then `responseError`, then assert the body type, else `ErrUnexpectedPDU` with a command-specific message. About 14 lines each, identical apart from the command ID, the two types and the message text. |
| Request, typed response, shared helper | `BindTransmitter`, `BindReceiver`, `BindTransceiver` | One line each, delegating to a single unexported `bind` that already holds the shape once. |
| Request, status only | `CancelSM`, `ReplaceSM`, `CancelBroadcastSM`, `EnquireLink`, `Unbind` | `Request`, then `responseError`; no response body is returned. |
| One-way | `AlertNotification`, `Outbind` | `SendOneWay`; no response exists. |

A generic helper such as
`func request[Req, Resp any](ctx, s *Session, id protocol.CommandID, req Req) (Resp, error)`
would cover only the first group, saving on the order of 60 to 80 lines.

### Decision

Keep the wrappers explicit. Do not introduce a generic request helper.

### Why

1. **The exported surface does not shrink.** Every wrapper is an exported
   method and a godoc entry, and the API is about to be frozen
   (`docs/API_COMPATIBILITY.md`). A generic helper would be unexported, so the
   seven methods, their signatures and their documentation all remain; the
   saving is internal only. (This is the strongest argument *for* the helper,
   and it is a modest one.)
2. **Each method states its own contract in one place.** The command ID, the
   request type, the response type and the error text sit together, and a
   search for `func (s *Session) SubmitSM` lands on the whole story. With a
   helper, the same information is spread across a call and a type
   instantiation on the most-read path in the package.
3. **The shape is not guaranteed to stay uniform.** Only a subset of the
   operations share it today, and the SMPP 5.0 family already breaks the
   one-response-type assumption: the codec can return `protocol.OptionalResponse`
   instead of a command's primary response type (see the types discovered by
   `session/body_coverage_test.go`). A helper with a single `Resp` type
   parameter cannot express "this or that body" without extra machinery, and
   would then be bypassed for exactly the commands that differ, leaving two
   styles to maintain instead of one.
4. **The duplication is mechanical and visible.** There is no logic in it to
   get subtly wrong: no locking, no state, no retries. Those live in
   `Request`, `SendOneWay` and the engine behind them, which are not duplicated.

What is *not* a reason: the project's stdlib-only policy (`AGENTS.md`). That
policy is about runtime dependencies; generics are a language feature and add
none, so it does not argue either way here.

### What it costs

About seven near-identical function bodies. A defect in the shape (for
example, how a wrong body type is reported) has to be fixed in each. Nothing
enforces that they stay in step beyond review.

### Revisit when

- a defect has to be fixed identically in more than one of these wrappers; or
- more than about ten operations share the typed-response shape; or
- the response-body variants above are handled in one place that a helper
  could reuse.

If revisited, prefer an unexported generic helper that keeps every exported
method and its documentation unchanged, and add a test that drives all of the
wrappers through the same success, error-status and wrong-body cases first, so
the change is provably behaviour-preserving.

---

## D2 — Declared minimum Go version

- **Status:** **decided and applied 2026-10-06.** The maintainer's decision is a floor of **Go 1.25**, with newer releases through **Go 1.27** supported, and on his explicit instruction `go.mod` now says `go 1.25`. (Review finding on `go 1.26.0`, Task 6.6.) It was applied in a scratch-verified but not directly compiled state: the environment used for this record has no Go 1.25 toolchain, so the first build of the real `go.mod` is the maintainer's; see "What was and was not verified after applying" below.
- **Applies to:** the `go` directive in `go.mod`, and the sentences that quote it (README, release checklist).

### Context

`go.mod` says `go 1.26.0`. A toolchain older than that either downloads a newer one (`GOTOOLCHAIN=auto`) or refuses to build ("requires go >= 1.26.0"), which on an air-gapped build host is a hard stop. The question the finding asks is whether any Go 1.26 feature is actually needed.

### Evidence

The whole module was built, vetted and tested, with the `go` directive in a scratch copy set to each value below and `GOTOOLCHAIN=local` so no toolchain was fetched. "Tests" means `go test -count=1 ./...` over all 10 packages, including the root package's architecture, API-contract and supply-chain tests.

| Toolchain | `go` line | Build | Vet | Tests |
| --- | --- | --- | --- | --- |
| 1.21.9 | 1.21 | ok | ok | ok |
| 1.22.2 | 1.22 | ok | ok | ok |
| 1.23.1 | 1.23 | ok | ok | ok |
| 1.24.13 | 1.24 | ok | ok | ok |
| 1.24.13 | 1.21 | ok | ok | ok |
| 1.24.13 | 1.22 | ok | ok | ok |

The race detector (`session`, `server`, `client`, `transport`) is also clean on 1.21.9 / line 1.21 and on 1.24.13 / line 1.21. The maintainer has separately run the full suite on the real Go 1.26 toolchain.

What this shows:

- **No Go 1.26 feature is load-bearing, and nothing newer than Go 1.21 is.** A 1.21 compiler accepts every package, test file, example and command in the module.
- **1.21 is the floor, and it is a hard one**: `log/slog`, used by `session` and `cmd/smpp-sim`, first shipped in Go 1.21. Go 1.20 was not tested (no toolchain available) and cannot work as the code stands.
- **The code does not depend on newer language semantics.** The `go` line selects language and runtime defaults: per-iteration loop variables arrive with a 1.22 line and the current timer-channel semantics with a 1.23 line. Rows 5 and 6 compile with a modern toolchain under the old semantics, and pass. So the library works under both old and new semantics, which matters because a consumer's own `go.mod` decides which apply to their binary.

### Decision

Declare **Go 1.25** as the minimum and support every release above it, which now includes 1.27. What backs this, and what does not:

- 1.21, 1.22, 1.23 and 1.24 were each tested directly (table above). The maintainer ran the suite on a Go 1.26 toolchain. The reference acceptance run's `environment.txt` records **go1.27.0**, and that run includes a race-detector pass of `session`, `client` and `server`, so 1.27 is confirmed on the maintainer's machine. Go 1.25 and 1.26 were not available in the environment used for this record, but the maintainer then ran **`GOTOOLCHAIN=go1.25.0 go test -count=1 ./...` against `go 1.25` and every package passed (2026-10-06)**, so the floor is confirmed on a real 1.25.0 compiler, the first release of that line, which is the worst case for missing standard-library APIs. Only 1.25.0 was run, not later 1.25.x patch releases; nothing suggests they differ.
- Go 1.25 is the maintainer's chosen floor, higher than the verified 1.21; it is not a limit of the code. One thing to weigh: if Go 1.27 is already released, upstream supports only 1.26 and 1.27, so a 1.25 floor means promising a toolchain that has just left upstream support. That is a legitimate choice, but it is the point of the trade-off above.
- **Supporting 1.27 means testing it.** CI should run a matrix of the floor and the newest release (`1.25.x` and `1.27.x`) once the CI stage opens, otherwise "supports 1.27" is a hope.
- The reference-acceptance toolchain is **Go 1.27.x**: `scripts/acceptance.sh` now requires `go1.27*` (changed on the maintainer's decision that 1.26 is not wanted as the reference), matching the recorded run on go1.27.0. It is a separate requirement from the module's minimum.

### Options

1. **Keep `go 1.26.0`.** No change, no new test burden. Cost: excludes any operator who cannot run a current toolchain.
2. **Lower to `go 1.21`**, the verified floor. Widest adoption. Cost: you now promise a toolchain that upstream stopped supporting long ago, so those users also run a standard library (`crypto/tls`, `crypto/x509`, `net`) without current security fixes. That is their choice, but it is part of what the library is then deployed on.
3. **Lower to an intermediate value** (for example `go 1.22`): the same evidence supports it. It narrows the promise to something less ancient but is otherwise arbitrary; the principled choices are 1 and 2.

### Recommendation

Option 2 **only together with a CI job that builds and tests on the floor**, plus one on the newest release. A declared minimum nobody tests is a claim that rots, and today CI installs only Go 1.26. If you do not want to maintain an old-toolchain job, choose option 1; that is a legitimate answer, not a failure.

### If you apply it

```sh
sed -i 's/^go 1.26.0$/go 1.25/' go.mod
```

and in the same commit:

- README: the sentence "(`go.mod` requires Go 1.26.0)" in the supply-chain section (change it to 1.25).
- `docs/RELEASE_CHECKLIST.md`: the minimum-Go-version item.
- CI (held until the CI stage): a matrix of the floor and the newest release; for example `go-version: ['1.25.x', '1.27.x']`.

Things that do **not** change, checked: `.github/workflows/ci.yml` and `reference-acceptance.yml` install Go with a literal `1.26.x`, not `go-version-file`, so lowering `go.mod` does not change which toolchain they use; and `scripts/acceptance.sh` requires a 1.26.x *toolchain* regardless of the `go` line, which is right, since the reference result should be taken on the current compiler.

### Revisit when

A feature newer than the floor is wanted for a good reason (then raise the floor deliberately and record it here), or when upstream's support window moves far enough from the floor that supporting it becomes a liability.

### What was and was not verified after applying

`go.mod` was changed with the one line above, and the README, `AGENTS.md` and `.codex/` statements of the minimum were updated with it; `.codex/DECISIONS.md` D-025 keeps its text and gains a "superseded" note. The environment this was done in cannot build a `go 1.25` module (its toolchains are 1.21 to 1.24, and `GOTOOLCHAIN=local` refuses a newer requirement), so the verification that matters is a run on a real 1.25 toolchain and on 1.27:

```sh
gofmt -l . && go build ./... && go vet ./... && go test -count=1 ./... && go test -race ./... && scripts/coverage.sh
```

CI: on `main`, `ci.yml` and `reference-acceptance.yml` still install Go `1.26.x`, so nothing there tests the 1.25 floor or 1.27, and `reference-acceptance.yml` would install a toolchain that `scripts/acceptance.sh` rejects. Pull request #1 fixes both: a `1.25.x` / `1.27.x` matrix in `ci.yml` (both legs passed) and `1.27.x` in `reference-acceptance.yml` (not exercised: it only runs on manual dispatch). It applies once the maintainer merges it (see the CI stage checklist in `.claude/TASKS.md`). `scripts/acceptance.sh` names the reference-acceptance environment (Go 1.27.x), not the module's minimum.

---

## D3 — Per-IP cap on unbound connections: configurable, off by default

- **Status:** decided and implemented 2026-10-11 (SEC1 remainder; the maintainer reversed the 2026-10-05 backlog decision and asked for it to be configurable).
- **Applies to:** `server.Config.MaxUnboundPerIP`, `server.Config.UnboundCapExempt`, `server.BindStats.RefusedUnbound`.

### Context

`MaxSessions` bounds how many sessions a server holds, and `BindRateLimiter` (Phase 5.1) throttles binds. Neither helps against a peer that connects and never sends a bind: each such connection holds a `MaxSessions` slot until `SessionInitTimeout` (30 s by default) closes it, and the peer reconnects as fast as it is closed. Measured with `MaxSessions: 3` and `SessionInitTimeout: 300 ms`, a flood of silent connections kept a legitimate peer out on 268 of 268 attempts in 1.5 s.

### Decision

- `Config.MaxUnboundPerIP int`: the most connections from one remote IP that may be open without having completed a bind. **Zero or negative, the default, means unlimited**, so nothing changes unless an operator opts in. A connection over the limit is closed on arrival, the same as one that arrives while `MaxSessions` is full, and counted in `BindStats.RefusedUnbound`.
- `Config.UnboundCapExempt []netip.Prefix`: networks the cap never applies to, for trusted peers or a NAT gateway.
- Only a session still in the open (unbound) state counts. Once a session binds it stops counting, so peers that have bound are never affected.
- The key is the IP alone: the port is ignored and an IPv4-mapped IPv6 address is the same as its IPv4 form. `BindThrottle` now uses the same key.

### Why this shape

1. **Configuration, not a hook.** The maintainer asked for it to be configurable, and the two real policy questions (how many, and who is trusted) are answered by two plain fields. A pluggable admission interface was the alternative; it can still be added later without breaking these.
2. **Off by default** because the right number depends on the deployment: many ESMEs behind one NAT address share a budget, and a default that quietly refuses legitimate peers is worse than one that does nothing. The cost is that a default-configured server is still exposed.
3. **Counting only unbound sessions** keeps legitimate, authenticated traffic out of the policy entirely.
4. **Cost follows one peer.** Sessions are indexed by remote IP, so deciding on a connection scans only that address's sessions, not the whole server.

### What it does not do

- **IPv6.** The key is a single address, so a client with a /64 can rotate through 2^64 addresses and never reach any one budget. A per-prefix key (for example /64) would address that; it is not implemented.
- **Distributed floods.** A flood spread over many source addresses is not stopped by a per-address limit; that needs network-level controls.
- **It is not on by default**, so the Phase 5 exit criterion ("unauthenticated peers cannot exhaust `MaxSessions`") holds for a server configured with it, not for one left at defaults.
- A peer that connects and binds slowly is counted as unbound until its bind completes; keep `SessionInitTimeout` consistent with the limit.
- A peer that drops an *unbound* connection and reconnects before the server has noticed the close is still counted once, so with a limit of 1 it can be refused for a few milliseconds. A connection that had already bound does not count, so this only affects drops during the bind phase. (Found by a flaky test of my own, not in production.)

### Choosing a value

A well-behaved peer holds one unbound connection at a time (it connects, binds, and is then bound). A value only slightly above the number of connections a legitimate peer opens at once is enough; raise it, or list the address in `UnboundCapExempt`, for NAT gateways.

### Revisit when

IPv6 clients are a real share of traffic (add a prefix key), or a flood from many addresses becomes a problem (consider an admission hook or global admission rate).

---

## D4 — A response with a non-zero command_status is written as a header plus optional TLVs

- **Status:** decided and implemented 2026-10-13 (option "A" of the F5 finding; the maintainer chose it).
- **Applies to:** `codec.EncodePDU`, in `codec/error_response.go`.

### Context

`FuzzDecodePDU50` found that the library could write a PDU its own decoder refused. For a response with a non-zero `command_status`, the decoder reads the body as optional TLVs only (SMPP 3.4 sends error responses as a bare header; SMPP 5.0 allows TLVs on them), and returns a typed, zero-valued body for a bare header. The encoder, though, wrote the typed body's mandatory fields whatever the status, so decode, encode, decode failed with "truncated TLV header". Reproduced in both registry modes. `session.Response` paths used `EmptyBody` for errors and were not affected; a caller returning a typed body with an error status was.

### Decision

When the command is a response and `command_status` is non-zero, `EncodePDU` writes the header plus the body's optional TLVs, and nothing else. The mandatory fields are not written. Requests, and responses with a zero status, are encoded exactly as before.

The body is still validated by the command's own encoder first, so a body of the wrong type, or with an invalid field, is an error as before. Only what reaches the wire changes.

### Why this option

The other options were: change the decoder to return `EmptyBody` (makes decode agree but leaves the encoder able to write a frame that no conforming peer expects); change both; or make the encoder fail on a typed body with an error status (the clearest, but breaking for anyone doing it today). Writing what the decoder reads removes the possibility of emitting an invalid frame at the source, and it is what the 3.4 text expects of an error response.

### What it costs

A caller who deliberately returned a typed body with an error status (say a `message_id` alongside an error) now has those mandatory fields silently left out of the frame. Nothing in this repository did that. If a peer needs them, send the response with a zero status or add a dedicated path; do not rely on the old behaviour, which this package's own decoder could not read back.

### Verified

`TestEveryRegisteredResponseSurvivesAnErrorStatus` runs every registered response command, in SMPP 3.4 and 5.0 and in both modes, with three error statuses: what is written decodes and re-encodes to the same bytes. The fuzz target no longer skips non-zero statuses, and a second 130-second run with that property (3.67 M executions) found nothing. Mutation-checked: removing the fix, applying it to success responses, applying it to requests with a stray status, dropping the TLVs, and dropping the validation are each caught.

---

## D5 — The library never panics; recovered panics cost the smallest unit

- **Status:** decided and implemented 2026-11-01. The maintainer required that the library not panic under any circumstances, and chose both policy options below.
- **Applies to:** every public package; the mechanism is in `session/panic.go`, `server/panic.go` and `codec/recover.go`.

### Context

An audit found 103 panicking entry points out of 316 exported functions and methods when called with zero values: nil and zero-value receivers on `Session`, `Client`, `Framer` and `StateMachine`, nil function adapters, and a nil `context` in `transport.DialTCP`/`DialTLS`. Live objects were already robust. There was also one deliberate `panic` in `session/own.go`, and no `recover` anywhere. So a panic in any application callback, in a vendor command decoder, or in a caller-supplied `net.Conn` ran on a session goroutine and crashed the whole process, taking every other session with it.

### Decision

1. **Callback panics are recovered and fail only that PDU or notification**, rather than closing the session, and are not left to crash the process. The details are in `docs/CONCURRENCY.md`, "Panics".
2. **Nil and zero-value receivers return a new `ErrNotInitialized`** (one each in `session`, `client` and `codec`), rather than reusing an existing error that would misdescribe the fault.
3. Anything else that panics on a session goroutine ends only that session, with a `*protocol.PanicError` as its error.
4. `own.go`'s size-mismatch panic becomes a fallback allocation. The invariant it guarded is now checked by a test (`assertOneArena`), not at run time.

### Why

An SMSC carries many sessions in one process, so one buggy callback or one malformed vendor PDU must not take them all down. Failing only the PDU keeps the peer answered and keeps the session's correlation state intact. That state is owned by the library, not the callback, so the library can keep it consistent across a callback panic. A distinct sentinel tells the caller what is actually wrong.

### What it costs

- A recover hides a bug that would otherwise be loud. This is mitigated by the metric, the event and the stack in the log, and the docs say to alarm on it.
- A panicking callback may have left its own state half-updated. The library cannot know, and still serves the next PDU. Callers who prefer to stop can close the session from an Observer on `EventPanicRecovered`.
- One open-coded `defer` per decode, encode and callback. Measured: no added allocations on the session hot path (9 allocs/op and 885 B/op before and after, `BenchmarkInMemorySessionBidirectional`). ns/op was within this host's noise, about ±12%. An `errors.As` that first cost one allocation per request was found by profiling and moved off the hot path.
- The API grows by additions only: `protocol.PanicError`, three `ErrNotInitialized`, `session.EventPanicRecovered` and `MetricsSnapshot.PanicsRecovered`.

### Not covered

- A callback that runs synchronously on the caller's own goroutine, such as `Framer.Feed`'s `emit` and `ScanTLVs`' `visit`, panics back to that caller, as `sort.Slice` does with its `less`. No library goroutine is involved.
- `transport.TLSClient`/`TLSServer` with a nil `net.Conn` return a `*tls.Conn` whose methods panic inside `crypto/tls`.
- A stuck callback (one that blocks rather than panics) still stalls its session. A recover cannot help with that.

### Verified

On go1.27.1 linux/amd64, `go test -race ./...`, lint and coverage all pass. These tests pin the behaviour:

- `session/panic_test.go`: Handler, one-way handler, bind, Observer, PacketTracer, WindowObserver, FlowController, Logger, decoder, response encoder and `net.Conn` panics;
- `server/panic_test.go`: Authenticator (bind delay kept), both `BindRateLimiter` methods (fail closed) and SubmitHandler;
- `codec/recover_test.go`;
- `nopanic_test.go`: 316 entry points, tied to the API snapshot.

Mutation-checked: removing one nil guard, dropping an API entry from the probe, or breaking `own.go`'s size pass each fails a test.

### Revisit when

Someone needs a callback panic to close the session instead. That would be a per-session option, not a change of default.

