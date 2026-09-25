# Implementation plan — phased task tracker

Convention: `[x]` = done, `[ ]` = open. Every finished task is registered with `x`.
Finding IDs (B*, P*, C*, T*, SEC*, A*) map to `CODE_REVIEW.md`; detail and fixes live there.

No implementation code is written by this document. Each phase lists acceptance criteria that
must hold before the phase is marked complete.

---

## Phase dependency graph

```
P0 hygiene ──┐
             ├─→ P1 availability ──┐
P1a allocs ──┘                     ├─→ P3 protocol correctness ─→ P4 structure ─→ P6 release
             P2 perf pool ─────────┘                                    ↑
                                              P5 hardening ──────────────┘
```

`P0` is independent and trivial. `P1` must land before `P4` so the large file split in `P4` does
not force rebasing correctness fixes. `P2` (pooling) precedes `P4b` (arena) because the arena
reuses the pool.

---

## Phase 0 — Legal & repository hygiene
Goal: make the project usable and CI-enforced on formatting. No behavioural change.
Estimate: trivial. Blocks everything else only in the sense that it blocks any release.

- [x] **0.1** Add `LICENSE` file (C6a) — *release blocker*
  - Decide MIT vs Apache-2.0 (Apache-2.0 if a patent grant matters for telecom deployments).
  - Acceptance: file present at repo root; README references it; `go.mod` untouched.
  - Done in `16d9b84`. Chose Apache-2.0 per the note above (telecom/patent grant).
- [x] **0.2** Add `.gitignore` (C6b)
  - Acceptance: covers `*.test`, `*.out`, `*.prof`, `/dist/`; `git status` stays clean after a
    local benchmark run.
  - Done in `ec72e0b`.
- [x] **0.3** Format the whole tree: `gofmt -w .` (C1)
  - Acceptance: `gofmt -l .` prints nothing; `go test ./...` still passes; the diff is
    formatting-only (no logic changes) and lands as its own commit.
  - Done in `190483f`.
- [x] **0.4** Add a CI format gate (C1, T2)
  - Acceptance: a step fails on non-empty `gofmt -l .` output; verified by a deliberately
    unformatted branch.
  - Done in `7b691dc` (`.github/workflows/ci.yml`, "Enforce gofmt" step).
- [x] **0.5** Add `.golangci.yml` + lint CI step (C2)
  - Start with the default set plus `staticcheck`. Defer `gocognit`/`funlen` until Phase 4.
  - Acceptance: lint passes with zero findings, or a documented `//nolint` with a reason for each
    suppression.
  - Done in `104403d`, pinned to `golangci-lint-action@v7` / `golangci-lint v1.62.2` (v8+ needs
    golangci-lint v2's different config schema, not attempted here). Zero findings after removing
    `Session.noteActivity` (see 4.3 below).

**Exit criteria:** clean `gofmt -l .`, clean lint, LICENSE present, gates wired in CI. **Met.**

---

## Phase 1 — Correctness & availability
Goal: eliminate paths where the library can panic a host process or hang indefinitely.
Estimate: small–medium. Highest priority after Phase 0.

- [x] **1.1** Remove the panic from `requestWindow.release()` (B1)
  - Make it total: return `bool`, add an underflow counter, emit an observer event. Keep the
    panic only behind a test-only strict-invariants flag.
  - Files: `session/window.go`, `session/observability.go`, `session/pending.go`, `session/session.go`
  - Acceptance: a unit test that double-releases and asserts no panic plus a non-zero counter;
    existing window tests still pass; `AGENTS.md` invariant (no unbounded growth) untouched.
  - Done in `564fca6`.
- [x] **1.2** Copy-before-append in `ensureSCInterfaceVersion` (B5)
  - Files: `session/session.go:1301-1313`
  - Acceptance: a test that supplies a `BindTransceiverResp` with a spare-capacity `Optional`
    slice, applies the injection twice, and asserts the first response is unmodified.
  - Done in `6a9c0ed`.
- [x] **1.3** De-couple liveness supervision from window saturation (B3)
  - Issue the keepalive with `wait=false`, skip the tick on `ErrWindowFull`, and escalate only if
    no response of any kind was observed for the interval. Optionally reserve one lifecycle slot.
  - Files: `session/session.go` (`livenessLoop`, `EnquireLink`), `session/window.go`
  - Acceptance: a test with a saturated window where an expired `InactivityTimeout` still
    terminates the session within one resolution tick.
  - Done in `ff9f8d1` (started, but left non-building, in `3c7c4ce`). The "escalate only if no
    response of any kind was observed" acceptance is satisfied structurally: `ErrWindowFull`
    causes the tick to be skipped so it cannot itself escalate, and the pre-existing, independent
    `InactivityTimeout` check (based on activity of any kind, checked earlier in the same tick)
    remains the backstop that fires once the window frees up or the interval genuinely elapses.
    Any other `tryEnquireLink` outcome (timeout, rejection, session loss) still terminates
    immediately, unchanged from the prior blocking behaviour, so `TestAutomaticEnquireLinkTimeoutClosesSession`
    still passes. The optional "reserve one lifecycle slot" was not done — left as a follow-up if
    `ErrWindowFull` skips prove too slow to detect a truly dead peer in practice.
- [ ] **1.4** Add write deadlines (B2, closes SEC1)
  - New `Config.WriteTimeout` (default 30 s; `-1` disables). `txLoop` sets it before each write
    and clears it after. `os.ErrDeadlineExceeded` → `terminate()`, consistent with the existing
    fail-closed model.
  - Files: `session/session.go` (`Config`, `normalizeConfig`, `txLoop`), `transport/transport.go`
  - Acceptance: a test with a peer that accepts the connection, reads nothing, and lets the
    buffers fill — the session terminates within the configured timeout rather than hanging. Also
    document the new field.
- [ ] **1.5** Re-decide completion-channel pooling (B4)
  - Under `-race` (CI or a Linux box), stress the pool. Either delete the pool (allocate the
    channel per request) or add an atomic generation check so a stale send cannot be delivered to
    a recycled channel.
  - Files: `session/session.go`, `session/pending.go`
  - Acceptance: `go test -race ./...` green in CI; the chosen outcome documented in
    `docs/CONCURRENCY.md`. If the pool is deleted, record the measured allocation delta.

**Exit criteria:** no `panic` reachable from any library path; a stalled peer cannot hold a
session open past the configured timeout; race suite green in CI with 1.5 resolved.

---

## Phase 2 — Performance against the 100k PDU/s target
Goal: remove steady-state allocations on the send path. **Profile-driven** per `AGENTS.md`;
land each behind the `[profile]` CI gate with before/after numbers in the commit message.

- [ ] **2.1** Fix the header-scratch allocation (P2) — trivial, measurable, do first
  - Replace `append(dst, make([]byte, HeaderSize)...)` with `slices.Grow`.
  - Files: `codec/pdu.go:76,87`
  - Acceptance: `codec` benchmarks show zero or reduced `B/op` per encode; `go test ./codec/`
    green.
- [ ] **2.2** Pool outbound frames (P1) — the largest single win
  - `sync.Pool` of frame buffers per session; `request()`/`SendOneWay()`/response path acquire,
    `txLoop` returns after the batch is fully handed to the transport. Size from
    `encodedBodySizeHint`.
  - Files: `session/session.go:359,421,1143`, `codec/size_hint.go`, new `session/pool.go`
  - Acceptance: before/after `-benchmem` on the send path; Phase 17 acceptance run shows reduced
    `maxHeapMB`; the write-completion lifetime boundary is stated in `docs/CONCURRENCY.md`;
    `unsafe` ban unaffected.
- [ ] **2.3** Clone decoded responses into one arena (P4) — reuses 2.2's pool
  - Files: `session/session.go` (`ownDecodedPDU`)
  - Acceptance: one allocation per response instead of one per byte-slice field; no borrowed
    slice outlives dispatch (assert with a framer-buffer-reuse test).
- [ ] **2.4** Fold liveness into the shared deadline heap (P5)
  - Removes the 10 ms ticker floor and the fourth per-session goroutine.
  - Acceptance: goroutine count per session drops by one (assert via `runtime.NumGoroutine` in a
    session-count test); all timer tests still pass. **Note:** this changes the documented
    "exactly four goroutines" invariant — update `AGENTS.md`, `docs/CONCURRENCY.md`, and
    `ARCHITECTURE_OVERVIEW.md` in the same commit.
- [ ] **2.5** Flat command-ID fast-path table in `Registry` (P3)
  - Only if measured above a few percent; otherwise close as won't-fix with the profile evidence.
  - Acceptance: benchmark delta recorded either way, including the negative result.
- [ ] **2.6** Extend size hints beyond submit/deliver (P6)
  - `bind`, `data_sm`, broadcast family.
  - Acceptance: hints covered by a table-driven test asserting the hint is ≥ actual encoded
    length for every registered command.

**Exit criteria:** send path allocation near zero at steady state; Phase 17 reference-machine
throughput measured and evidence published.

---

## Phase 3 — Protocol correctness detail
Goal: make error reporting and state enforcement match the SMPP spec and the project's own
stricter rules.

- [ ] **3.1** Map semantic decode errors to proper `command_status` (B6)
  - Add a `Status` field to `protocol.SemanticError`, set it at each decode site, map it in
    `processFrame`; fall back to `ESME_RINVMSGLEN` only when unset.
  - Files: `protocol/errors.go`, `codec/*.go`, `session/session.go:964,1023`
  - Acceptance: table-driven test per error class asserting the exact status code (0xC0 for a bad
    TLV stream, 0xC1 for a disallowed TLV, etc.); framer still not poisoned on semantic errors.
- [ ] **3.2** Route vendor commands through the state machine (B10)
  - Record a direction + permitted-state mask per registered command (including vendor ones) in
    `RegistryBuilder`; delete the hand-maintained `isStandardSessionCommand` list.
  - Files: `codec/registry.go`, `session/state.go`, `session/session.go:1089`
  - Acceptance: a vendor command is refused in a state where the equivalent standard command is
    refused; a wrong-direction vendor request is refused; `architecture_test.go` still passes.
- [ ] **3.3** Harden `message.Reassemble` (B9)
  - Switch to `slices.SortFunc`, error on duplicate `Sequence`, require an explicit non-zero
    `total`.
  - Files: `message/message.go:207,209`
  - Acceptance: duplicate-segment and incomplete-set tests return errors rather than silently
    succeeding or truncating.
- [ ] **3.4** Shrink the framer buffer after a large fragmented PDU (B7)
  - Acceptance: a test that feeds one 1 MiB fragmented PDU then asserts retained capacity drops
    below the read-buffer threshold.
- [ ] **3.5** Decide `splitBytes` empty-input behaviour (B8)
  - Either return `nil` or document the single-empty-segment choice explicitly.
  - Acceptance: the doc comment and the tests agree; no silent `short_message` of length zero.

**Exit criteria:** peer-visible error codes match the spec; the operation matrix governs all
inbound commands; the retained-memory metric is bounded by traffic, not by history.

---

## Phase 4 — Structural cleanup
Goal: make each concern independently reviewable. Behaviour-preserving only.

- [ ] **4.1** Split `session/session.go` (A5)
  - `tx.go` / `rx.go` / `liveness.go` / `negotiate.go` / `own.go`, with `session.go` keeping the
    type, config, `New`, the public request API, and `terminate`.
  - Acceptance: pure file movement — zero diff in behaviour; all tests green; no new package;
    `architecture_test.go` unchanged.
- [ ] **4.2** Collapse the duplicated type switches (C4)
  - `ownable` / `optionalCarrier` interfaces replacing `ownDecodedPDU` and
    `responseOptionalParameters` parallel switches.
  - Acceptance: a new body type that fails to implement the interface is caught by a compile
    error or a single loud test, not silently.
- [ ] **4.3** Remove dead code (C3) — *partially done*
  - `Session.noteActivity` (`session.go:698`); move `deadlineManager.schedule` into a `_test.go`
    helper or document it as test-only.
  - Acceptance: `unused`/lint reports zero dead symbols.
  - `Session.noteActivity` removed in `104403d` (forced by 0.5's new lint gate). Still open:
    `deadlineManager.schedule` is real dead-code-in-production-file — it's called only from
    `_test.go` files, which `unused` correctly doesn't flag, but it still belongs in a test helper
    per the acceptance note; left for this phase's file-split work.
- [ ] **4.4** Annotate deliberate duplication (A6)
  - Doc comments on `protocol.SubmitSM` and `protocol.DeliverSM` stating the identical layout is
    intentional per SMPP 3.4 and must not be merged.
- [ ] **4.5** Record the C5 decision
  - Typed operation wrappers stay explicit. Add the rationale to `docs/DECISIONS.md` so the
    choice is documented rather than implicit.

**Exit criteria:** no file above ~700 LOC in `session/`; behaviour and API byte-identical.

---

## Phase 5 — Security & operational hardening
Goal: close unauthenticated resource exposure and make operator foot-guns visible.

- [ ] **5.1** Bind-attempt rate limiting (SEC2)
  - Optional `Config.BindRateLimiter` hook, fixed delay on authentication failure, and an alarm
    counter. Keep policy pluggable rather than baked in.
  - Files: `server/server.go`, new `server/ratelimit.go`
  - Acceptance: a test driving repeated failed binds shows the throttle engaging; legitimate bind
    throughput unaffected below the threshold.
- [ ] **5.2** `PacketTracer` credential warning (SEC3)
  - Doc comment stating that tracing raw frames exposes bind credentials; verify no log path
    prints `BindRequest.Password`.
  - Acceptance: warning present; a test asserting the password never appears in emitted log
    output.
- [ ] **5.3** Document the recommended TLS baseline (SEC4)
  - Package doc: `MinVersion: tls.VersionTLS12`, verified peer certificates. Keep configuration
    caller-supplied.
- [ ] **5.4** Publish the zero-dependency / no-`unsafe` posture in the README
  - It is a real differentiator for telecom deployments and currently under-communicated.

**Exit criteria:** unauthenticated peers cannot exhaust `MaxSessions`; credential-exposure paths
are documented and tested; supply-chain posture is stated up front.

---

## Phase 6 — Test coverage & release
Goal: measurable quality floor, then `v1.0.0`.

- [ ] **6.1** Coverage measurement and floor (T1)
  - Acceptance: `go test -coverprofile` in CI with a documented minimum that cannot silently
    regress.
- [ ] **6.2** Commit the fuzz corpus (T4)
  - Acceptance: `codec/testdata/fuzz/` holds the crashers and interesting inputs found so far, so
    the CI smoke does not start cold each run.
- [ ] **6.3** Stalled-peer and slow-client tests (T3)
  - Acceptance: covered by 1.4; add the read-side equivalent.
- [ ] **6.4** Publish Phase 17 reference-machine evidence
  - Acceptance: throughput result and resource bounds recorded in `docs/PERFORMANCE.md`.
- [ ] **6.5** Document the compatibility promise
  - `COMPATIBILITY.md` (or README section): SemVer covers exported identifiers in `protocol`,
    `codec`, `message`, `session`, `client`, `server`; `internal/` explicitly excluded.
  - Acceptance: cross-referenced with `api_contract_test.go`.
- [ ] **6.6** Reconsider the declared minimum Go version
  - `go 1.26.0` requires the newest toolchain, which narrows adoption among conservative telecom
    operators. Lower it if no 1.26-only feature is load-bearing.
- [ ] **6.7** Tag `v1.0.0`
  - Acceptance: all Phase 0–3 items closed; first git tag pushed.

---

## Explicitly deferred

| Item | Reason |
| --- | --- |
| C5 generic operation wrappers | Reduces line count but hurts greppability and godoc; current explicit style matches the repo's stdlib-only philosophy. Decision recorded by 4.5. |
| `gocognit` / `funlen` lint rules | Would only generate noise before 4.1's file split; revisit after Phase 4. |

---

## Verification limits carried into implementation

`go test -race ./...` could not run during the review — the host has no cgo/C toolchain
(`-race requires cgo`, `gcc` not found). Findings B1–B4 are derived from reading the code. CI
runs the race detector on ubuntu-latest. **Task 1.5 must be resolved under `-race`, and any
concurrency claim in this plan must be re-confirmed on Linux before Phase 1 is closed.**
