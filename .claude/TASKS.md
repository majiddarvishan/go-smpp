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
- [x] **1.4** Add write deadlines (B2, closes SEC1)
  - New `Config.WriteTimeout` (default 30 s; `-1` disables). `txLoop` sets it before each write
    and clears it after. `os.ErrDeadlineExceeded` → `terminate()`, consistent with the existing
    fail-closed model.
  - Files: `session/session.go` (`Config`, `normalizeConfig`, `txLoop`), `transport/transport.go`
  - Acceptance: a test with a peer that accepts the connection, reads nothing, and lets the
    buffers fill — the session terminates within the configured timeout rather than hanging. Also
    document the new field.
  - Done in `4e6af29`. No special-case branch needed for the deadline error: `txLoop`'s existing
    `if err != nil { s.terminate(err) }` already treats every write error as fatal, so
    `os.ErrDeadlineExceeded` flows through the same path unchanged. Documented in
    `docs/TIMEOUTS_AND_LIVENESS.md`.
  - **Follow-up fix (found while assessing Phase 2's exit criteria):** that first version set the
    deadline before, and cleared it after, *every batch*, and I never benchmarked it. A Phase 2
    allocation profile showed `net.(*pipeDeadline).set` / `time.AfterFunc` on the hot path; an
    interleaved A/B/C on real localhost TCP (6 rounds, medians) then showed it cost about 7.5%:
    ~6371 ns/op vs ~5923 with deadlines disabled. Fixed by re-arming only when less than half of
    `WriteTimeout` remains and dropping the clear (every write re-arms itself, so it was never
    needed): ~5969 ns/op, indistinguishable from disabled. Trade-off: a slow-but-progressing write can
    be cut off as early as `WriteTimeout`/2 after it began (upper bound unchanged). Pinned by
    `TestWriteDeadlineIsNotRearmedOnEveryBatch` (exactly 1 arm across 51 batches — mutation-checked)
    and `TestWriteTimeoutStillBoundsAStallAfterSuccessfulWrites` (both bounds).
- [x] **1.5** Re-decide completion-channel pooling (B4)
  - Under `-race` (CI or a Linux box), stress the pool. Either delete the pool (allocate the
    channel per request) or add an atomic generation check so a stale send cannot be delivered to
    a recycled channel.
  - Files: `session/session.go`, `session/pending.go`
  - Acceptance: `go test -race ./...` green in CI; the chosen outcome documented in
    `docs/CONCURRENCY.md`. If the pool is deleted, record the measured allocation delta.
  - Done in `185941e`. Chose deletion, per the review's own stated preference. Measured delta on
    `BenchmarkInMemorySessionBidirectional`: 10→12 allocs/op, 873→1009 B/op; latency/throughput
    unchanged within noise. Documented in `docs/CONCURRENCY.md`.

**Exit criteria:** no `panic` reachable from any library path; a stalled peer cannot hold a
session open past the configured timeout; race suite green in CI with 1.5 resolved. **Met.**

---

## Phase 2 — Performance against the 100k PDU/s target
Goal: remove steady-state allocations on the send path. **Profile-driven** per `AGENTS.md`;
land each behind the `[profile]` CI gate with before/after numbers in the commit message.

- [x] **2.1** Fix the header-scratch allocation (P2) — trivial, measurable, do first
  - Replace `append(dst, make([]byte, HeaderSize)...)` with `slices.Grow`.
  - Files: `codec/pdu.go:76,87`
  - Acceptance: `codec` benchmarks show zero or reduced `B/op` per encode; `go test ./codec/`
    green.
  - Done. Landed anyway as a consistency fix, but the profile it was supposed to be driven by
    disproved the premise: go1.22's escape analysis already proved the original `make()` temporary
    non-escaping (`does not escape` in `-gcflags=-m`), and an alloc-object profile of
    `BenchmarkEncodeSubmitSM` attributes 100% of the 2 allocs/op to `reservePDUCapacity`'s
    `slices.Grow` (unavoidable — `dst` starts `nil`) and to `body any` interface-boxing at the call
    site, none to `pdu.go:76/:87`. `B/op`/`allocs/op` unchanged before/after (288 B, 2 allocs).
    Full writeup and the discovered `body any` boxing cost in `.claude/CODE_REVIEW.md`'s P2 entry.
    New candidate surfaced by this measurement, not yet a numbered task: removing the `any` box on
    the `Request`/`EncodePDU` body parameter would need an API-shape change and is real send-path
    allocation cost — worth a profile-driven look whenever 2.2/2.3 get here, since it's the same
    hot path.
- [x] **2.2** Pool outbound frames (P1) — the largest single win
  - `sync.Pool` of frame buffers per session; `request()`/`SendOneWay()`/response path acquire,
    `txLoop` returns after the batch is fully handed to the transport. Size from
    `encodedBodySizeHint`.
  - Files: `session/session.go:359,421,1143`, `codec/size_hint.go`, new `session/pool.go`
  - Acceptance: before/after `-benchmem` on the send path; Phase 17 acceptance run shows reduced
    `maxHeapMB`; the write-completion lifetime boundary is stated in `docs/CONCURRENCY.md`;
    `unsafe` ban unaffected.
  - Done. `codec.EncodedPDUSizeHint` exported (wraps `encodedBodySizeHint`) so `session` can size
    without a new `codec` dependency surface beyond one function. Pool is `*[]byte`-based (a raw
    `[]byte` boxed into `sync.Pool`'s `any` allocates on every `Put`, which would reintroduce
    exactly what this removes). Write-completion boundary (write returns, and on the success path
    specifically, after `tracePacket` — the only post-write reader of `item.frame`, and one that
    already never retains a bare reference) is written up in `docs/CONCURRENCY.md`, "Outbound frame
    pool lifetime". `unsafe` ban confirmed unaffected (`go list ... | grep unsafe` empty on the full
    module including tests).
    `-benchmem` on `BenchmarkInMemorySessionBidirectional/sessions_1`: 12→10 allocs/op, 1009→~905
    B/op — reproducible, matches expectation. The `maxHeapMB` half of the acceptance criterion did
    **not** show a clear result at sandbox scale (`TestPhase17ReferenceAcceptance` with
    `SMPP_ACCEPTANCE_DURATION=8s`, `SESSION_COUNT=4`, `CALLERS=128`): one comparison tied at 3 MiB,
    another showed a 1 MiB *increase* that tracked a slightly higher completed-request count rather
    than a regression — a ~100 B/op difference is under both this metric's 1 MiB granularity and
    single-sample noise at this scale. Recorded as an honest gap in `docs/CONCURRENCY.md` rather
    than claimed; confirming the heap effect needs the documented reference machine, not available
    here.
    Correctness evidence: `session/pool_content_test.go` — sequential (500 iterations) and
    concurrent (40 goroutines × 50) `SubmitSM` calls each carrying a globally unique,
    peer-verified `SourceAddr`, clean under `go test -race -count`. `session/pool_test.go` covers
    the pool's own logic (hint sizing, default capacity, oversized-buffer eviction, nil-safety,
    concurrent get/put) but deliberately does **not** assert object-identity reuse across a
    get/put/get: `sync.Pool`'s own docs disclaim any such relationship, an earlier version of that
    test asserted it anyway and flaked under `-race -count=10`, and the actual correctness property
    (no cross-request content corruption) is what the content tests prove instead, independent of
    whether reuse happens on a given run.
- [x] **2.3** Clone decoded responses into one arena (P4) — reuses 2.2's pool
  - Files: `session/session.go` (`ownDecodedPDU`)
  - Acceptance: one allocation per response instead of one per byte-slice field; no borrowed
    slice outlives dispatch (assert with a framer-buffer-reuse test).
  - Done. Not literally pooled from 2.2's `session.frames`: that pool's safety depends entirely on
    its write-completion boundary (txLoop returns a buffer once nothing will read it again), and a
    decoded response arena has the opposite lifetime — it's handed to the *caller* via
    `request.done`, who may hold onto its byte slices indefinitely, so there is no point at which
    this session could ever safely reclaim it. Implemented as a plain per-response allocation
    instead (`responseArena` in `session.go`): one `[]byte` sized by a first pass over the same
    decoded body, then every byte-slice field re-sliced out of it in a second pass, replacing
    `cloneBytes`/`cloneOptional` (now dead, removed). `[]protocol.OptionalParameter` /
    `[]protocol.UnsuccessfulSME` struct slices still allocate separately — not bytes, can't share
    the byte arena — matching the acceptance wording precisely ("one per byte-slice field", not
    "one, period").
    `take()` panics if the arena runs short — unreachable via `ownDecodedPDU` itself (its size pass
    and take pass read the same immutable local value, so peer-controlled content can't make them
    disagree), kept anyway to turn a hypothetical future size/take mismatch into an immediate test
    failure instead of a silently truncated clone; not the peer-influenced-input class of panic
    Task 1.1 removed from the window path. `session/arena_test.go`'s
    `TestResponseArenaTakePanicsWhenUndersized` proves it fires.
    "No borrowed slice outlives dispatch": `TestOwnDecodedPDUDoesNotAliasSourceBuffer` and the
    `SubmitMultiResp` variant build a body whose fields borrow from one scratch buffer, call
    `ownDecodedPDU`, overwrite every byte of the scratch buffer, and assert the returned copy is
    unaffected — a direct, deterministic proof, more so than trying to time a real framer-buffer
    reuse race. Exact-content and nil-preservation cases covered too.
    `BenchmarkOwnDecodedPDUWithOptionalTLVs` (5 TLVs + MessageID — the existing end-to-end
    benchmarks respond with a bare MessageID and no TLVs, so they wouldn't have shown this): 8→3
    allocs/op, 352→336 B/op. The remaining 3 (arena, the `[]OptionalParameter` slice, and boxing
    `body` back into `pdu.Body any`) match exactly what the design predicts; that last one is the
    same `any`-boxing cost Task 2.1 found on the encode side, unaddressed here too — same reason,
    would need a `DecodedPDU.Body` representation change out of this task's scope.
- [x] **2.4** Fold liveness into the shared deadline heap (P5)
  - Removes the 10 ms ticker floor and the fourth per-session goroutine.
  - Acceptance: goroutine count per session drops by one (assert via `runtime.NumGoroutine` in a
    session-count test); all timer tests still pass. **Note:** this changes the documented
    "exactly four goroutines" invariant — update `AGENTS.md`, `docs/CONCURRENCY.md`, and
    `ARCHITECTURE_OVERVIEW.md` in the same commit.
  - Done. `livenessLoop` and `livenessResolution` are deleted; `New` starts three goroutines.
    Session-init, inactivity, and enquire_link are three caller-owned `deadlineItem` slots on
    `Session`, scheduled in `New`, rescheduled in place via `scheduleItemAt` (zero allocation),
    routed in `expireDeadline` by pointer identity. Inactivity/enquire_link fire, re-derive idle from
    the live `lastActivityTime()`, and either act or reschedule for when idle would actually hit the
    threshold — so `noteActivityAt` stays one atomic store, never touching the heap.
    **A first implementation deadlocked**, caught by the existing
    `TestAutomaticEnquireLinkTimeoutClosesSession` (session never closed): calling `tryEnquireLink`
    synchronously from the enquire_link handler runs it on `deadlines.run`'s only goroutine, and it
    blocks on a response-timeout deadline that only `deadlines.run` can fire. A self-deadlock, not a
    slow tick. Fixed by running the probe in its own short-lived goroutine while the handler
    reschedules and returns immediately.
    **Decision for you:** that probe goroutine is a deviation from `AGENTS.md`'s literal rule "Do not
    create a goroutine … per timeout". I kept it narrow (rate follows idle time, not message volume;
    it essentially never fires under traffic) and documented it in `docs/CONCURRENCY.md`,
    `ARCHITECTURE_OVERVIEW.md`, and `CLAUDE.md`, but I did not edit `AGENTS.md`'s rule to bless my own
    deviation — that is your call. A goroutine-free alternative exists: dispatch the probe without
    waiting, flag its `pendingRequest`, and have `expireDeadline` terminate the session when a flagged
    entry times out. It needs a dispatch-only variant of `request()`, the most depended-on function
    in the package, so I didn't attempt it unasked. Also: `AGENTS.md` contains no "four goroutines"
    statement to update (only the general rule above); the literal claim lived in `CLAUDE.md` and
    `ARCHITECTURE_OVERVIEW.md`, both fixed, along with `CLAUDE.md`'s stale "no LICENSE / .gitignore /
    .golangci.yml" line and `ARCHITECTURE_OVERVIEW.md`'s stale `EncodePDU(nil, ...)` (stale since 2.2).
    Tests: `TestSessionStartsExactlyThreeGoroutines` (acceptance — verified to fail with "= 4" against
    the old `session.go`, and checks all three exit on Close);
    `TestInactivityDeadlineReschedulesInsteadOfTerminatingWhileActive`,
    `TestInactivityDeadlineFiringBeforeBindReschedulesAndAppliesAfterBind`, and
    `TestLivenessRescheduleKeepsHeapAndGoroutinesBounded` (heap ≤ 4 and goroutines bounded over 20+
    probe cycles). The first two were mutation-checked: disabling either reschedule branch makes them
    fail. All pre-existing timer tests pass unmodified; new + old timer tests stable at
    `-race -count=15` and `-cpu 1,2`.
    Measured, 300 idle unbound sessions over 3 s, CPU time, 3 runs each: with a 40 ms
    `EnquireLinkInterval` (hits the old 10 ms floor) ~165 ms → ~111 ms (about a third less); with
    default timers ~5.8–8.8 ms → ~0.1–0.18 ms (roughly 50×, since the old 2 Hz ticker per session is
    gone entirely).
- [x] **2.5** Flat command-ID fast-path table in `Registry` (P3)
  - Only if measured above a few percent; otherwise close as won't-fix with the profile evidence.
  - Acceptance: benchmark delta recorded either way, including the negative result.
  - Closed as won't-fix. Measured `BenchmarkRegistryCommandLookup` (the isolated
    `map[protocol.CommandID]CommandDefinition` lookup `ResolveCommand` wraps) against the full-pipeline
    `BenchmarkEncodeSubmitSM`/`BenchmarkDecodeSubmitSM` (`-count=3` each, this host): the lookup is a
    stable ~3.6 ns/op against a stable ~230 ns/op encode and ~210 ns/op decode — roughly 1.6% and 1.7%
    of total time respectively, both `EncodePDU`/decode call it exactly once. Below the task's own "a
    few percent" bar; not worth the extra fast-path-table structure and its own correctness surface
    (keeping it in sync with the map, handling out-of-band vendor IDs) for that.
- [x] **2.6** Extend size hints beyond submit/deliver (P6)
  - `bind`, `data_sm`, broadcast family.
  - Acceptance: hints covered by a table-driven test asserting the hint is ≥ actual encoded
    length for every registered command.
  - Done. Added hints for `bind_receiver`/`bind_transmitter`/`bind_transceiver` (+ their `_resp`s),
    `data_sm`/`data_sm_resp`, and the SMPP 5.0 broadcast family (`broadcast_sm`/`_resp`,
    `query_broadcast_sm`/`_resp`, `cancel_broadcast_sm`; `cancel_broadcast_sm_resp` is always empty —
    `encodeResponseEmpty` — so needs no hint). Every byte count was read off the actual `encode*`
    function for that command (not guessed from the struct's field list) — noted in each new size-hint
    function's doc comment so the two don't silently drift apart later.
    `codec/size_hint_test.go`'s `TestEncodedPDUSizeHintCoversRealEncodings` is the acceptance test: 22
    cases (every new command, several with realistic non-empty fields and TLVs, plus the `EmptyBody` and
    `OptionalResponse` response shapes) each actually call `EncodePDU` and assert the hint is `>=` the
    real encoded length. All pass on the first attempt — the manual wire-layout arithmetic matched
    every encoder exactly.

**Exit criteria:** send path allocation near zero at steady state; Phase 17 reference-machine
throughput measured and evidence published.

**Status: all six tasks done (2.5 as a measured won't-fix), exit criteria NOT met — stated plainly
so nobody reads the ticked boxes above as "Phase 2 is finished".**

- *Send-path allocation near zero at steady state — not met.* End-to-end round trip is at ~10-11
  allocs/op, ~885-930 B/op (down from 12 / 1009 at the end of Phase 1). An `-memprofilerate=1`
  `alloc_objects` profile of `BenchmarkInMemorySessionBidirectional` shows what is left:
  `Session.request` itself is ~40% (~4 per request: the `pendingRequest` struct, the completion
  channel, the `releaseWindow` closure, …); interface boxing is most of the rest — the `body any`
  argument at each `SubmitSM`/`DeliverSM` call (~1 per request, the finding Task 2.1 turned up),
  each decoded body boxed into `DecodedPDU.Body` (1 per decoded PDU, 2 per round trip), and
  `ownDecodedPDU` re-boxing the body after cloning (1). Not library cost: the benchmark's own
  handlers, and `net.Pipe`'s deadline timers (an in-memory-only artifact; real TCP does not
  allocate there). The two concrete next steps are folding `request()`'s per-call objects into
  fewer allocations (embedding the completion channel and `releaseWindow` state in
  `pendingRequest`), and the `any`-boxing family, which needs a body-representation or
  call-signature change and so is a public-API decision, not a drive-by.
- *Phase 17 reference-machine throughput measured and published — not done, and cannot be from
  the sandbox this was developed in.* It needs the documented Linux/amd64 8-core / 10 GB machine and
  a published result. The heap-effect half of 2.2's acceptance criterion is waiting on the same run.

---

## Phase 3 — Protocol correctness detail
Goal: make error reporting and state enforcement match the SMPP spec and the project's own
stricter rules.

- [x] **3.1** Map semantic decode errors to proper `command_status` (B6)
  - Add a `Status` field to `protocol.SemanticError`, set it at each decode site, map it in
    `processFrame`; fall back to `ESME_RINVMSGLEN` only when unset.
  - Files: `protocol/errors.go`, `codec/*.go`, `session/session.go:964,1023`
  - Acceptance: table-driven test per error class asserting the exact status code (0xC0 for a bad
    TLV stream, 0xC1 for a disallowed TLV, etc.); framer still not poisoned on semantic errors.
  - Done. `SemanticError.Status` already existed (unused); every decode-site error reachable via
    the semantic (non-fatal) path now sets it — `requireDone` (trailing body octets),
    `requestStatusMustBeZero`, `decodeBindResponse` (sc_interface_version wrong length),
    `decodeEmpty` (header-only command with a body), `decodeShortMessageBody`/`decodeReplaceSM`/
    `decodeSubmitMulti` (sm_length 255, short_message+message_payload conflict), `decodeSubmitMulti`
    (number_of_dests out of range), and `DecodePDU` itself (frame longer than its own
    command_length). `processFrame` extracts it via `errors.As`, falling back to
    `StatusInvalidMessageLength` only when unset. `session.go:1023`'s out-of-range
    sequence_number got `StatusSystemError` instead (no SMPP status fits "bad sequence_number"
    either).
    **Correction to this finding's own examples**, written up in `.claude/CODE_REVIEW.md`'s B6
    entry: 0xC0 (bad TLV stream) is not reachable here — every TLV-length violation is already a
    `*protocol.FatalError`, correctly, since a mis-parsed TLV boundary means the rest of the body
    can't be trusted; and 0xC1 (disallowed TLV) has no decode-time check behind it at all today —
    adding one would be new validation, not a status-mapping fix, so neither was touched.
    Acceptance test: `codec/semantic_status_test.go`, a table of 8 hand-built malformed frames (raw
    bytes an encoder would never produce), each asserting the exact `*protocol.SemanticError.Status`
    *and* that `errors.Is` against the original sentinel still succeeds. `session/semantic_status_test.go`
    covers the session-level half: a real generic_nack on the wire carrying the mapped status, and a
    valid submit_sm processed normally right after (framer not poisoned). Caught two real mistakes via
    mutation testing before landing: an early version of the session-level test picked a case
    (sm_length 255 → 0x01) that numerically coincides with the old fallback, so it passed even with the
    status-extraction code deleted entirely — fixed by switching to a case with a different mapped
    value; separately, the session test's own handler returned `StatusOK` for binds with a nil body,
    which produced a real but confusing fatal decode error unrelated to the code under test.
- [x] **3.2** Route vendor commands through the state machine (B10)
  - Record a direction + permitted-state mask per registered command (including vendor ones) in
    `RegistryBuilder`; delete the hand-maintained `isStandardSessionCommand` list.
  - Files: `codec/registry.go`, `session/state.go`, `session/session.go:1089`
  - Acceptance: a vendor command is refused in a state where the equivalent standard command is
    refused; a wrong-direction vendor request is refused; `architecture_test.go` still passes.
  - Done. `codec.CommandDefinition` gained `Actor`/`AllowedStates`; a new `codec.CommandActor` enum
    avoids referencing `session.Role` directly (would be an import cycle). `isStandardSessionCommand`
    deleted; `CanIssue`'s switch became a `standardRules` map, `IsStandardCommand` is map membership
    off the same data. `beginInbound` checks a vendor command's declared policy when set, else keeps
    the old permissive default (opt-in per command — not a breaking change).
    Translation correctness: `TestCanIssueMatchesOldSwitchExhaustively` (old switch copied verbatim
    as an independent oracle vs. the new map, 462 combinations, zero mismatches) — written because
    this is core, well-trusted logic and "I read the diff carefully" isn't the same evidence as an
    independent check catching a transcription slip on its own.
    Acceptance tests: `session/vendor_state_test.go` — `TestVendorCommandRefusedInWrongState`
    (BoundRX session, a vendor command sharing submit_sm's ESME/BoundTX-or-TRX policy, refused the
    same way submit_sm would be), `TestVendorCommandWrongDirectionRefused` (ActorSMSC vendor
    command from an ESME peer, refused regardless of state), and
    `TestVendorCommandAcceptedInCorrectStateAndDirection` (positive control — same command, right
    role and state, reaches the Handler). All three mutation-checked: dropping the registry-policy
    check entirely breaks the first two but not the control; dropping only the actor check breaks
    just the direction test. `architecture_test.go` (`TestPackageDependencyDirection`) passes.
    Docs: `docs/VENDOR_EXTENSIONS.md` gained a section on the new opt-in fields.
- [x] **3.3** Harden `message.Reassemble` (B9)
  - Switch to `slices.SortFunc`, error on duplicate `Sequence`, require an explicit non-zero
    `total`.
  - Files: `message/message.go:207,209`
  - Acceptance: duplicate-segment and incomplete-set tests return errors rather than silently
    succeeding or truncating.
  - Done — with a correction. Neither failure mode this finding describes was actually reachable:
    the existing post-sort position check (`part.Sequence != i+1`) already rejects every duplicate
    regardless of sort stability, and the `total == 0` fallback's own triggering segment always
    fails that same check against the newly-computed total. Verified by mutation-removing each new
    explicit check and re-running its test — both times the function still errored, just with a
    less specific message. Full reasoning and the mutation results are in `.claude/CODE_REVIEW.md`'s
    B9 entry. Landed the fix anyway: switched to `slices.SortFunc`, and added dedicated "duplicate
    sequence N" / "total must be specified" errors rather than leaving that correctness property to
    an unrelated check's side effect, which is real diagnosability value on its own. No existing
    caller depends on the old `total == 0` fallback — `Reassemble` has none in this repo yet outside
    its own tests. `slices.SortFunc` alone measured ~525ns → ~410ns and 7 → 4 allocs/op
    (`BenchmarkReassemble`, interleaved rounds); the full change keeps 4 allocs/op but the explicit
    duplicate-check loop's own CPU cost brings latency back to roughly the original figure — a wash
    on that axis, allocations are the real win.
    Tests: `TestReassembleRejectsDuplicateSequence`, `TestReassembleRejectsZeroTotal`,
    `TestReassembleRejectsIncompleteSet` (the acceptance criterion's named case, confirmed already
    covered). All three mutation-checked.
- [x] **3.4** Shrink the framer buffer after a large fragmented PDU (B7)
  - Acceptance: a test that feeds one 1 MiB fragmented PDU then asserts retained capacity drops
    below the read-buffer threshold.
  - Done. The finding's claim was verified first: before the change, one fragmented 1 MiB PDU
    left `cap(f.pending) == 1048576` after emit. Fix as the finding proposed — after the emit
    reset, `if cap(f.pending) > framerShrinkThreshold { f.pending = nil }` — with
    `framerShrinkThreshold = 64 << 10` (matches `session.DefaultReadBufferSize` / the outbound
    pool cap; a constant in `codec/framer.go` since `codec` can't import `session`).
    Tests (`codec/framer_retention_test.go`): `TestFramerReleasesLargeBufferAfterFragmentedPDU`
    (the acceptance test: 1 MiB PDU, chunk sizes 2/3/4096/65536 to cover both entry paths into
    `pending`; asserts one correct frame and `cap <= 64 KiB`), `TestFramerKeepsSmallBufferAfterFragmentedPDU`
    (positive control: small buffers are still reused), `TestFramerDecodesCorrectlyAfterShrink`,
    `TestFramerStartsCleanAfterShrinkWithShortFirstChunk`. Mutation-checked, 4 mutants all killed:
    remove the shrink; shrink always (`> 0`); threshold raised to 2 MiB; shrink without resetting
    `expected`. Two of those only died after I fixed my own first draft: the bound in the test
    was the production constant (so a raised threshold passed — tautological), now a literal
    `retentionBound`; and nothing exercised a short first chunk after a shrink, so the
    stale-`expected` mutant survived until `...ShortFirstChunk` was added.
    Cost, measured (`BenchmarkFramerFragmentedPDU`, interleaved rounds): fragmented PDUs *larger
    than 64 KiB* now allocate once per PDU — 100 KiB ~3.1 µs/0 allocs -> ~13-16 µs/1 alloc;
    1 MiB ~47-59 µs/0 -> ~150-163 µs/1; at or below the threshold, no change (0 allocs). A
    deliberate trade of sustained >64 KiB fragmented throughput for bounded idle memory.
    Not changed, documented in CODE_REVIEW.md's B7 entry: the buffer is still sized from the
    declared `command_length` up front (in-flight, deadline-bounded), and a non-fatal `emit` error
    leaves the frame buffered so it is re-emitted on the next `Feed` (unreachable via `Session`).
- [x] **3.5** Decide `splitBytes` empty-input behaviour (B8)
  - Either return `nil` or document the single-empty-segment choice explicitly.
  - Acceptance: the doc comment and the tests agree; no silent `short_message` of length zero.
  - Done — with a correction. The finding's premise (callers emit an empty `short_message` because
    of `splitBytes`) does not hold: the empty single-part segment comes from the "fits in one part"
    branches of the UDH functions (the SAR functions delegate to them), never from the split helpers.
    Probed on the old code, then pinned: `SegmentTextUDH("")`, `SegmentTextSAR("")`,
    `SegmentBinaryUDH(nil|empty)`, `SegmentBinarySAR(nil|empty)` all return exactly one segment (Total 1,
    Sequence 1, Units 0, empty body, no UDH/TLVs); that test passed on the old code and passes
    unchanged now, so the change is behaviour-preserving at the public surface.
    Chose the finding's first option: `splitBytes` returns `nil` for empty input. `splitUTF16` had the
    same shape (`[][]byte{nil}`) and is not named in the finding; changed too so the helpers agree
    (deviation from the finding's literal text). A caller missing its own empty guard now fails in
    `makeUDHSegments` (`ErrInvalidSegments`, zero parts) rather than emitting an empty multipart segment.
    The public empty-message behaviour is documented in doc comments on all four `Segment*` functions.
    Tests (`message/split_empty_test.go`): `TestEmptyMessageIsOneEmptySinglePartSegment` (6 entry
    points), `TestSplitHelpersReturnNoPartsForEmptyInput`, `TestSplitBytesNonEmptyUnchanged`,
    `TestMakeUDHSegmentsRejectsZeroChunks`. Mutation-checked, 5 mutants all killed: `splitBytes` back to
    one empty chunk; `splitUTF16` guard removed; `len(chunks) <= 1` -> `== 1` in `SegmentTextSAR` and in
    `SegmentBinarySAR`; `makeUDHSegments` zero-chunk check dropped.
    **Decision for you, not made here:** whether an empty message should be an *error* at the public API.
    `sm_length` 0 is valid SMPP and nothing in this repo submits one, but it is exported behaviour, so it
    stays as documented.

**Exit criteria:** peer-visible error codes match the spec; the operation matrix governs all
inbound commands; the retained-memory metric is bounded by traffic, not by history.

**Status: all five tasks done.** The first two exit criteria are covered by tests (3.1's status table,
3.2's vendor-state tests). The third is covered at unit level for the framer (3.4: capacity released
after a large fragmented PDU) but was **not** measured with the Phase 17 harness's `maxRetainedMB`, which
needs the reference machine; the in-flight allocation sized from the declared `command_length` is also
still open (see B7 in `CODE_REVIEW.md`). Not claiming more than that.

---

## Phase 4 — Structural cleanup
Goal: make each concern independently reviewable. Behaviour-preserving only.

- [x] **4.1** Split `session/session.go` (A5)
  - `tx.go` / `rx.go` / `liveness.go` / `negotiate.go` / `own.go`, with `session.go` keeping the
    type, config, `New`, the public request API, and `terminate`.
  - Acceptance: pure file movement — zero diff in behaviour; all tests green; no new package;
    `architecture_test.go` unchanged.
  - Done. `session.go` 1643 -> 544 lines, split into `tx.go` (240), `rx.go` (269), `liveness.go` (170),
    `negotiate.go` (81), `own.go` (177), `operations.go` (211). Largest non-test file in `session/` is now
    `session.go`; the ~700 LOC exit criterion holds with room to spare.
    Deviations from the finding's table, because the code had moved on since it was written (it
    predates Phase 2): `livenessLoop`/`livenessResolution` no longer exist (Task 2.4 folded liveness into
    the shared deadline heap), so `liveness.go` holds `expireDeadline`, `scheduleLivenessDeadlines`, the
    three `fire*Deadline` handlers and the activity tracking; `own.go` also holds `responseArena`
    (Task 2.3); `observeCongestion` went to `rx.go`; and the typed operation wrappers (`BindTransmitter`,
    `SubmitSM`, ... `Unbind`, `responseError`) went to a seventh file, `operations.go`, which the finding
    did not name — keeping them in `session.go` would have left it at ~750 lines. `Request`,
    `TryRequest`, `request`, `SendOneWay`, `waitCompleted`, accessors, `New` and `terminate` stay in `session.go`.
    How "pure movement" was verified rather than assumed: the move was done by a small go/ast program
    that copies each declaration's exact source text (doc comments included) and refuses to run if any
    planned name is missing or any comment would be dropped; an independent checker then compared the
    multiset of declaration texts before and after: 91 declarations, 0 mismatches. `go doc -all
    ./session` is byte-identical before and after (exported API unchanged), the list of tests is
    identical (86), and `gofmt`, `go vet`, `go test ./...` and `go test -race` on `session`, `client`,
    `server` are clean. No test file, `architecture_test.go`, or `go.mod` was touched. (The checker caught
    one thing in my first run: gofmt column alignment of adjacent one-line accessors had changed when
    they were emitted separated by blank lines; the splitter now preserves original adjacency, so the
    declaration texts match exactly.) Line numbers quoted in `FINDINGS.md`/`CODE_REVIEW.md` for
    `session/session.go` are historical and no longer point at the same lines.
- [x] **4.2** Collapse the duplicated type switches (C4)
  - `ownable` / `optionalCarrier` interfaces replacing `ownDecodedPDU` and
    `responseOptionalParameters` parallel switches.
  - Acceptance: a new body type that fails to implement the interface is caught by a compile
    error or a single loud test, not silently.
  - Done — by the acceptance's second branch, not by the proposed interfaces, and the switches were
    **not** collapsed. The interfaces cannot live in `session`: the body types are in `protocol`, so a
    session-side interface with unexported methods can't be satisfied by them, and exported methods would
    add public API to `protocol` (not behaviour-preserving). Instead one test closes the silent-failure
    hole: `TestEveryResponseBodyTypeIsOwnedAndExposesItsOptionals` (`session/body_coverage_test.go`). It
    discovers body types from the SMPP 5.0 registry (every registered response command, decoded from
    synthetic bodies with and without a trailing TLV; 11 types today including `OptionalResponse` and
    `codec.RawBody`), so a newly registered body type is covered with no edit to the test, and asserts
    for each: `ownDecodedPDU` leaves nothing aliased to the source after it is overwritten, and
    `responseOptionalParameters` returns exactly the `Optional` field (or nil). It refuses to pass
    vacuously (fewer than 10 types discovered, or a struct whose fields it could not fill, is a failure).
    Mutation-checked, 6 mutants all killed: dropping the `QueryBroadcastSMResp` or `OptionalResponse`
    case from `ownDecodedPDU`; dropping `DataSMResp` or `QueryBroadcastSMResp` from
    `responseOptionalParameters`; not copying `SubmitMultiResp.Unsuccessful`; not cloning `RawBody`.
    Production code untouched.
- [x] **4.3** Remove dead code (C3)
  - `Session.noteActivity` (`session.go:698`); move `deadlineManager.schedule` into a `_test.go`
    helper or document it as test-only.
  - Acceptance: `unused`/lint reports zero dead symbols.
  - Done. `Session.noteActivity` was already removed in `104403d` (forced by 0.5's new lint gate).
    `deadlineManager.schedule` moved out of `session/deadline.go` into `session/deadline_helpers_test.go`.
    One addition beyond the finding: `deadlineManager.scheduleItem` was test-only too (its only callers
    were `schedule` and one line of `deadline_test.go`; production uses `scheduleItemAt` directly from
    `tx.go` and `liveness.go`), so it moved with it; leaving it would have kept a dead production wrapper.
    Its old doc comment claimed the "session hot path" uses it, which was false for that wrapper, so the
    helper's comment now says what it is. Verified rather than assumed: `go build ./...` (non-test code
    only) passes with both functions gone from `deadline.go`, which proves no production caller; `go vet`
    and `go test` (which do compile the tests) pass with them in the helper; `deadline.go` retains
    `scheduleItemAt`, the single production entry point. Zero behaviour change by construction (no logic
    moved, only two one-line wrappers). `unused` does not flag identifiers used from `_test.go`, which is
    why this survived the lint gate; it is not run here (no golangci-lint download, per the working rules).
- [x] **4.4** Annotate deliberate duplication (A6)
  - Doc comments on `protocol.SubmitSM` and `protocol.DeliverSM` stating the identical layout is
    intentional per SMPP 3.4 and must not be merged.
  - Done. The finding's claim was checked first, by reflection: both structs have 18 fields, identical
    names, types and order, and are mutually convertible. Doc comments on both types now say the identical
    layout is intentional (SMPP 3.4 gives the two PDUs the same mandatory-parameter layout), that the
    types must not be merged or aliased, and why: the Go type carries the direction, so `Session` cannot be
    handed one for the other at compile time and the codec refuses a body of one type under the other's
    command ID. I did not want a comment to assert something nothing checks, so that last claim is pinned:
    `codec/submit_deliver_distinct_test.go` (`TestEncodeRejectsSubmitSMAndDeliverSMBodiesUnderTheWrongCommand`).
    Mutation-checked, 2 mutants killed: `encodeSubmitSM` also accepting a `DeliverSM`; and
    `type DeliverSM = SubmitSM` — which **compiles cleanly**, so only the test, not the build, catches an
    accidental merge. The one comment in the old `DeliverSM` doc about differing field semantics is kept.
    No production logic changed.
- [x] **4.5** Record the C5 decision
  - Typed operation wrappers stay explicit. Add the rationale to `docs/DECISIONS.md` so the
    choice is documented rather than implicit.
  - Done. `docs/DECISIONS.md` created (it did not exist) with record D1. Written from the code as it
    now is, not from the finding: of `Session`'s 17 operation methods only 7 (`SubmitSM`, `DeliverSM`,
    `DataSM`, `SubmitMulti`, `QuerySM`, `BroadcastSM`, `QueryBroadcastSM`) share the typed-response shape
    a generic helper could cover (the 3 `Bind*` already share one unexported `bind`; 3 return status only;
    2 are one-way), saving roughly 60-80 lines. Reasons recorded: the exported surface and godoc would not
    shrink; each method keeps its contract in one place; the shape is not guaranteed uniform (codec can
    already return `OptionalResponse` in place of a primary response type); and the duplicated code is
    mechanical, with no locking or state. One correction to the finding's rationale: "stdlib-only"
    is **not** a reason — it concerns runtime dependencies and generics add none — so D1 says so rather
    than repeating it. Revisit conditions and the safe way to revisit (unexported helper, with a test over
    all wrappers first) are written down. Documentation only; no code touched.

**Exit criteria:** no file above ~700 LOC in `session/`; behaviour and API byte-identical.

**Status: all five tasks done.** `session/session.go` 1643 -> 544 lines (largest non-test file in
`session/`); exported API of `session` byte-identical by `go doc -all` (4.1); no production logic changed
by 4.2-4.5 apart from moving two test-only wrappers out of `deadline.go` (4.3). Two tasks met their
acceptance by a different route than the finding proposed, and say so in their notes: 4.2 (a registry-driven
test instead of interfaces, which cannot live in `session`) and 4.5 (decision recorded, no code).

---

## Phase 5 — Security & operational hardening
Goal: close unauthenticated resource exposure and make operator foot-guns visible.

- [x] **5.1** Bind-attempt rate limiting (SEC2)
  - Optional `Config.BindRateLimiter` hook, fixed delay on authentication failure, and an alarm
    counter. Keep policy pluggable rather than baked in.
  - Files: `server/server.go`, new `server/ratelimit.go`
  - Acceptance: a test driving repeated failed binds shows the throttle engaging; legitimate bind
    throughput unaffected below the threshold.
  - Done. Added to `server`: the `BindRateLimiter` interface (`AllowBind` before the Authenticator,
    `RecordBind` after), `Config.BindRateLimiter`, `Config.BindFailureDelay`, `Server.BindStats()`
    (`Attempts`, `Failures`, `Throttled`) and a default policy `NewBindThrottle(BindThrottleConfig)`.
    A refused bind gets `ESME_RTHROTTLED` and the Authenticator is **not** invoked, so a lockout costs
    the operator's credential store nothing. All nil/zero by default: no behaviour change unless opted in.
    `BindAttempt` carries remote address, system_id and mode and **no password**; a test pins that no
    slice-typed field can be added unnoticed.
    Default throttle, each a choice you may replace: keyed by remote IP (port ignored; NAT'd peers share a
    budget); 5 failures per 1-minute window lock the IP out for 1 minute; a *successful* bind does not
    reset the count (otherwise anyone holding one valid credential gets fresh guesses); at most 10 000
    IPs tracked, oldest evicted first, so the table cannot be used to exhaust memory.
    Two more decisions to be aware of: (1) an Authenticator that returns an **error** is treated as an
    infrastructure fault and never counts toward a lockout — so an Authenticator that signals a bad
    password by returning an error would never be throttled; `Config.BindRateLimiter`'s doc says to use
    `BindResult.Status`, and a test pins the behaviour. (2) `BindFailureDelay` holds only the offending
    session's receive loop, applies to rejected *and* throttled binds, and ends when the session closes.
    It slows one connection only; the per-IP limiter is what stops a multi-connection guesser.
    Tests (`server/ratelimit_test.go`, 15): the throttle on a fake clock (engages at the threshold, only
    for that IP, success never resets, lockout and window expiry, bounded memory with oldest-first
    eviction, concurrent use), and through a real server and client (engages after repeated failed binds
    and refuses even the correct password while locked out without calling the Authenticator; 60
    legitimate binds from the same address unaffected below the threshold; no limiter means never
    throttled; delay on failures only, not on success; delay on throttled binds; a session in its delay
    does not block a bystander and is released when it closes; exact `BindStats`). Mutation-checked, 12
    mutants, all killed (never refusing; counting successes; calling the Authenticator when throttled;
    lockout never expiring; window never resetting; delay ignoring session close; unbounded table; delay
    on success; failure counter not incremented; counting Authenticator errors; evicting newest instead of
    oldest — which survived my first version of the eviction test and drove its fix; keying by port).
    `go test -race -count=3 ./server/` clean.
    **Open, not fixed here — this is why the Phase 5 exit criterion "unauthenticated peers cannot
    exhaust `MaxSessions`" is NOT yet met:** a flood of connections that never bind is untouched by bind
    rate limiting. Measured with a throwaway probe: `MaxSessions: 3`, `SessionInitTimeout: 300 ms`, an
    attacker keeping the server topped up with silent connections; a legitimate peer was refused on 268 of
    268 attempts in 1.5 s. `SessionInitTimeout` frees each slot eventually but the attacker reconnects.
    The remedy is a cap on concurrent unbound connections per remote address (or an accept-time hook):
    new public API and a policy call (NAT, trusted peers), so I have not added it. Decision for you.
- [x] **5.2** `PacketTracer` credential warning (SEC3)
  - Doc comment stating that tracing raw frames exposes bind credentials; verify no log path
    prints `BindRequest.Password`.
  - Acceptance: warning present; a test asserting the password never appears in emitted log
    output.
  - Done. The finding's premise was audited, not assumed. There are only two `slog` call sites in the
    library (`logFatal` in `session.go`, the one-way handler error in `rx.go`); neither formats a body or
    a frame. `FatalError.Reason` strings are fixed literals (the one dynamic `Reason` is `err.Error()` of a
    semantic decode error, also literal), and `Event` carries no payload field. Result: no library log,
    event or error path prints a password. The only way a password leaves the library is
    `PacketTrace.RawPDU`, which is opt-in (`Config.TraceRawPDU`), and the old doc comment already said "may
    contain credentials" but never said what that means. Warning added: a SECURITY paragraph on
    `PacketTracer` (bind/outbind carry the password in clear text, up to 8 octets; submit/deliver/data_sm
    carry content; what to do), a stronger `PacketTrace` comment, and a pointer in the `Config` doc comment for `TraceRawPDU` (on the
    type, not on the field: a comment inside the struct makes gofmt realign ten unrelated fields).
    Tests (`server/credential_exposure_test.go`): `TestPasswordNeverReachesLogsEventsOrErrors` drives an
    accepted bind, a rejected bind, two client-side encode errors (embedded NUL, over-long) and a hostile raw
    bind with an over-long unterminated password (this last one is the path that reaches the fatal-error
    log, and the test fails if it does not), with a Debug-level `slog` handler on both sides, an Observer,
    and `slog.Default()` captured too; it checks logs, every event rendered `%v`/`%+v`/`%#v`, and every
    returned error, for the password, any 5-octet run of it (a decoder echoing a field is bounded by the
    field length, so it would show a prefix) and its decimal byte list. Positive control
    `TestRawPacketTraceDoesExposeThePasswordWhenEnabled` proves the test harness can see a leak and keeps
    the warning honest; `TestPacketTraceCarriesNoPayloadByDefault` pins "no payload unless asked".
    Mutation-checked, 6 mutants all killed: debug-logging every decoded PDU with `%+v`; the unterminated
    C-string fatal reason echoing the bytes (**survived** my first version, which only searched for the
    whole password while the leak shows 9 octets; drove the 5-octet-run check); the server logging the
    request through the default logger on failed auth; `AppendCString`'s error embedding the value; the
    tracer always populating `RawPDU`; and never populating it.
    Limit worth knowing: this proves the library does not leak, not that an `Authenticator`,
    `SubmitHandler` or `PacketTracer` you write does not.
- [x] **5.3** Document the recommended TLS baseline (SEC4)
  - Package doc: `MinVersion: tls.VersionTLS12`, verified peer certificates. Keep configuration
    caller-supplied.
  - Done. `transport/doc.go` gains a `# TLS` section: why TLS (SMPP is clear text), that policy is
    caller-supplied and the package never alters it, a copy-pasteable server and client baseline
    (`MinVersion: tls.VersionTLS12`, `RootCAs`, `ServerName`, verified peers), and the traps. Short
    pointers added to `server/doc.go`, `client/doc.go` and the root `doc.go`. Configuration stays
    caller-supplied; no code changed.
    Two things the finding did not say, found by reading and then testing the code: (1) **a nil
    `TLSConfig` means plain TCP, not default TLS** (`server.Listen` and `client` take the TLS path only if
    it is non-nil), which is the real way to end up weaker than intended, so it is the first trap listed;
    (2) `DialTLS` does **not** infer `ServerName` from the address as `crypto/tls.Dial` does, so a bare
    config fails the handshake, which is documented rather than changed. I deliberately did not state any
    default `MinVersion` for either side: it varies by Go release and GODEBUG, so the doc says to set it.
    A doc that asserts behaviour nothing checks rots, so every statement is pinned in
    `transport/tls_baseline_test.go` (7 tests): the baseline connects and
    negotiates >= 1.2; a TLS 1.1 client is refused by a 1.2-floor server; verification is on (an
    unknown-authority error); `ServerName` is not inferred; mutual TLS works and refuses a client with no
    certificate; and the config is passed through untouched in both directions (a caller `MinVersion` of
    1.3 is honoured, a caller cap at 1.2 is honoured, neither is replaced). Mutation-checked, 5 mutants
    all killed: `DialTLS` skipping verification; `DialTLS` inferring `ServerName`; `ListenTLS` keeping only
    `Certificates`; `ListenTLS` dropping `ClientAuth`; and `ListenTLS` forcing `MinVersion` 1.3 (the last
    **survived** my first set because every client there also speaks 1.3, which drove
    `TestListenTLSHonoursCallerMaxVersion`). Race-clean.
    Not done, by design: no helper that builds a "secure default" `tls.Config`. That would be policy
    inside the library, which SEC4 itself calls the wrong design.
- [x] **5.4** Publish the zero-dependency / no-`unsafe` posture in the README
  - It is a real differentiator for telecom deployments and currently under-communicated.
  - Done. README gets a "Supply-chain posture" section above "Current status". Every claim was
    verified before being written: `go list -deps -test` shows no non-standard package other than the
    module itself; no file imports `unsafe` or `"C"`; `go.mod` has no `require` and there is no `go.sum`;
    `CGO_ENABLED=0 go build ./... && go vet ./...` passes; `cmd/smpp-sim` cross-compiles for
    linux/amd64, linux/arm64, windows/amd64 and darwin/arm64. I did **not** claim "static binary" (only
    that it builds and cross-compiles), and the section says what it does not cover: the standard
    library and runtime use `unsafe`/cgo internally, so the Go toolchain is still part of the trust base.
    Already present before this task and left alone: the CI step that bans direct `unsafe` imports
    (`.github/workflows/ci.yml`, not touched per the working rules) and `AGENTS.md`'s policy. What was
    missing was a check that runs under `go test`, so the posture now has one: `TestSupplyChainPosture`
    (`posture_test.go`) walks every Go file including tests, examples, `cmd` and `internal`, and checks
    imports for `unsafe`, `"C"` and any non-standard, non-module package, plus `go.mod` directives and
    `go.sum`. It refuses to pass vacuously (>= 50 files scanned, eleven named directories visited,
    `crypto/tls` seen). Mutation-checked, 7 mutants all killed: an `unsafe` import; a third-party import in
    a *test* file; `import "C"`; a third-party import under `examples/`; a `require` line; a `go.sum`; and
    the walker skipping `session/`.
    Limit: it reads import declarations, so it cannot see code generated or fetched at build time
    (`go:generate`, `-toolexec`), which this repo does not use.

**Exit criteria:** unauthenticated peers cannot exhaust `MaxSessions`; credential-exposure paths
are documented and tested; supply-chain posture is stated up front.

**Status: 5.1-5.4 done; the first exit criterion is NOT met.** Credential-exposure paths are documented
and tested (5.2, 5.3) and the supply-chain posture is stated up front and now enforced (5.4). But "unauthenticated
peers cannot exhaust `MaxSessions`" is not true: 5.1 rate-limits *binds*, and a flood of connections that never
bind is untouched by it (measured: 268 of 268 legitimate connection attempts refused in 1.5 s against
`MaxSessions: 3`, `SessionInitTimeout: 300 ms`). Closing it needs a cap on concurrent unbound
connections per remote address or an accept-time admission hook: new public API and a policy decision
(NAT, trusted peers). **Backlogged by maintainer decision on 2026-10-05; see "Explicitly deferred".**

---

## Phase 6 — Test coverage & release
Goal: measurable quality floor, then `v1.0.0`.

- [x] **6.1** Coverage measurement and floor (T1) — *CI wiring held*
  - Acceptance: `go test -coverprofile` in CI with a documented minimum that cannot silently
    regress.
  - Done, except the CI step itself, which is held by the working rule that CI changes wait for the final
    stage (`.github/workflows/` untouched). Delivered: `scripts/coverage.sh` (check and `--suggest`
    modes; honours `GOFLAGS`, so `-race` works), `.coverage-floor` (versioned, so lowering a floor is a
    visible diff), and `docs/COVERAGE.md` (floors, rules, and the exact one-line CI step to add later).
    Measured on 2026-10-05, Go 1.22, linux/amd64: client 61.1, codec 64.8, encoding 74.5, message 75.8,
    protocol 56.8, server 75.7, session 82.3-82.8, transport 80.4, module total 73.3 (counting
    cross-package coverage). Floors are each 2 points below. Variability was measured rather than guessed:
    five runs moved only `client` (61.1-61.5) and `session` (82.3-82.8), under 0.5 points, and the figures
    held under `-race -covermode=atomic`.
    The script is checked, not trusted: 9 scenarios on a scratch copy, all behaving: healthy run passes;
    deleting `session`'s tests fails (`session` 0.0 < 80); a floor raised above reality fails; a new package
    with no floor fails; a floor for a package that no longer exists fails; a total floor above reality
    fails; a failing test fails the check before any coverage is judged; a missing floor file fails; a bad
    argument exits 2.
    Honest limit: statement coverage shows a line ran, not that anything asserted on it. `protocol` is the
    lowest (56.8) and is mostly large encoder/decoder tables covered by round-trip and fuzz tests. This is a
    regression guard, not a correctness claim; the mutation checks in this file are what tested the tests.
    Until the CI step is added, nothing *enforces* the floor automatically: run `scripts/coverage.sh`
    before merging. You have not run it yet on your toolchain (Go 1.26); the numbers there may differ
    slightly.
- [x] **6.2** Commit the fuzz corpus (T4)
  - Acceptance: `codec/testdata/fuzz/` holds the crashers and interesting inputs found so far, so
    the CI smoke does not start cold each run.
  - Done. `codec/testdata/fuzz/FuzzFramer/` (13 files) and `.../FuzzDecodePDU/` (178 files), 17 KB in
    total, plus a `README.md` with provenance and the refresh command. Obtained by fuzzing each target from a
    cold cache for this task: `FuzzFramer` 120 s, 3.85 M executions; `FuzzDecodePDU` 150 s, 4.93 M
    executions (Go 1.22, one CPU). **No crashing input was found, so there are no crashers to commit** —
    the acceptance's "crashers" part is empty because none exist, not because they were left out. The
    project notes mention none either.
    Measured, not assumed, that it does what the finding asked: (1) a 2 s `FuzzDecodePDU` smoke now gathers
    baseline coverage over 181 inputs instead of the 3 inline seeds, so it no longer starts cold; (2) plain
    `go test` replays all 191 files as subtests (197 with the inline seeds), which is also why `codec`
    coverage rose from 64.8 to 76.0 and its floor, and the total floor, were raised in
    `.coverage-floor`/`docs/COVERAGE.md`.
    Does the corpus catch regressions the rest of the suite misses? A little, and I measured it with six
    plausible decoder bugs (TLV length check removed; C-string limit not clamped; `ReadUint32` off by one;
    framer accepting a length below the header; TLV header truncation off by one; C-string scanning one octet
    past its limit). Full suite without the corpus caught 4 of 6; the two fuzz targets with only their inline
    seeds caught 0 of 6; with the corpus 5 of 6. `ReadUint32` off by one was caught **only** by the corpus.
    The TLV-header off-by-one was missed by everything: the fuzz contract is "no panic", and that bug reads
    a byte too far inside the buffer without panicking, so it needs a targeted unit test, not more fuzzing.
    `TestFuzzCorpusIsCommitted` (`codec/fuzz_corpus_test.go`) fails if either directory is deleted or
    emptied (both cases tried), since a tidy-up would otherwise silently switch off the regression suite
    and the warm start.
    Limits, and what I did not do: the targets' contract is panic-freedom plus the framer's frame-length
    invariant, nothing about decoded values; `FuzzDecodePDU` covers the SMPP 3.4 registry in compatible
    mode only, so SMPP 5.0 command decoding is not fuzzed; and `FuzzFramer` feeds one slice per run, so it
    cannot reach the fragmentation paths (including Task 3.4's buffer release). A third target feeding the
    framer in random chunks and comparing with a one-shot feed would cover that. I did not add targets
    unasked; say if you want them. The CI step is unchanged (`.github/workflows/` untouched), and
    needs no edit to benefit: it will pick the corpus up from `testdata`.
- [x] **6.3** Stalled-peer and slow-client tests (T3)
  - Acceptance: covered by 1.4; add the read-side equivalent.
  - Done, and it found something. The write side (peer never reads) was Task 1.4. The read-side
    equivalent is a peer that stops *sending*: half a length word, an unfinished PDU, or a trickle that never
    completes. I probed the real code first and asserted afterwards. `session/stalled_read_test.go`
    (net.Pipe, 6 tests): half a length word before bind and a byte-per-5 ms trickle of a declared-1 MiB PDU are
    both closed by `SessionInitTimeout` (the trickle does not extend it); a bound peer stalled mid-PDU, and one
    trickling, are both closed by `EnquireLinkTimeout` at about interval + timeout (81 ms for 40 + 40);
    with enquire_link disabled and nothing sent, `InactivityTimeout` closes it. `server/stalled_read_test.go`
    (real TCP and a real server, 2 tests): a client that sends half a bind, and a bound client that starts a
    1 MiB PDU and stops, are both disconnected (the socket reaches EOF, not just the session list) and the
    server forgets the session. Race-clean (`-count=2`).
    Mutation-checked, 7 mutants all killed: init deadline never scheduled (both packages); bytes counting as
    liveness via a per-read activity stamp plus an idle-style init timer (the slow-loris shape; kills both
    trickle tests); outbound writes no longer counting as activity; an unanswered enquire_link not ending the
    session (both packages); inactivity never firing.
    **Finding, documented and pinned, not fixed:** with `EnquireLinkInterval` disabled, a peer that keeps
    reading but never sends anything back is *not detected while the local side keeps sending*. Outbound writes
    count as activity (as `docs/TIMEOUTS_AND_LIVENESS.md` already said), so `InactivityTimeout` never fires;
    each request ends in a response timeout and the session stays up. Measured: alive after 3 s against a
    200 ms inactivity timeout. The default configuration (enquire_link every 30 s) is not affected, so this is a
    consequence of opting out of the liveness probe, not a default-config hole. It is pinned by
    `TestDisabledEnquireLinkLeavesAReadStalledPeerUndetectedWhileWeKeepSending`, which will fail, on purpose,
    if behaviour changes. A possible fix is a policy such as "terminate after N consecutive response
    timeouts"; that is a behaviour change with its own design questions (what N, per-session or per-window,
    interaction with reconnect), so I did not make it. Decision for you; until then the docs say to keep
    enquire_link on or to watch `Metrics().ResponseTimeouts`.
    Also documented, as a deliberate corollary: a PDU that legitimately takes longer than
    `EnquireLinkInterval` + `EnquireLinkTimeout` to arrive is cut off like a stalled one.
    `docs/TIMEOUTS_AND_LIVENESS.md` gains a "Peers that stall while sending" section with the table.
    Coverage floors unchanged (the new tests exercise existing paths).
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
| Cap on concurrent unbound connections per remote address, or an accept-time admission hook (SEC1 remainder) | Maintainer decision 2026-10-05: not now, backlogged. Until then the Phase 5 exit criterion "unauthenticated peers cannot exhaust `MaxSessions`" is knowingly unmet. Measured: with `MaxSessions: 3` and `SessionInitTimeout: 300 ms`, a flood of silent connections refused 268 of 268 legitimate attempts in 1.5 s. Interim mitigation is outside the library: allow only known peer IPs at the firewall, and keep `SessionInitTimeout` short. Design questions to settle when picked up: NAT-shared addresses, trusted-peer exemptions, new public API (`server.Config`) and its compatibility cost. |
| `gocognit` / `funlen` lint rules | Would only generate noise before 4.1's file split; revisit after Phase 4. |

---

## Verification limits carried into implementation

`go test -race ./...` could not run during the review — the host has no cgo/C toolchain
(`-race requires cgo`, `gcc` not found). Findings B1–B4 are derived from reading the code. CI
runs the race detector on ubuntu-latest. **Task 1.5 must be resolved under `-race`, and any
concurrency claim in this plan must be re-confirmed on Linux before Phase 1 is closed.**
