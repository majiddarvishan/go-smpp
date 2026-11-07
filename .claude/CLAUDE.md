# CLAUDE.md — working notes for go-smpp

Module: `github.com/majiddarvishan/go-smpp` · Go 1.25 minimum · **zero external dependencies**
Scope: SMPP 3.4 + SMPP 5.0 library providing both ESME (client) and SMSC (server) roles over a
single shared session core.

`AGENTS.md` in the repo root is the authoritative rule set. This file records how to build,
verify, and what was actually verified on this machine.

---

## Commands

```bash
go build ./...
go vet ./...
go test ./...
go test -race ./...            # requires cgo + a C toolchain
gofmt -l .                     # must print nothing
go test -run XXX -bench . ./protocol/ ./codec/
```

Fuzz smoke (matches CI):

```bash
go test ./codec/ -run Fuzz -fuzz FuzzFramer -fuzztime 2s
go test ./codec/ -run Fuzz -fuzz FuzzDecodePDU -fuzztime 2s
```

Phase 17 acceptance (opt-in, resource-bounded):

```bash
SMPP_ACCEPTANCE=1 go test ./internal/perflab/ -run TestPhase17ReferenceAcceptance -v
```

Env knobs for the acceptance harness: duration, sessions, callers, minRPS, maxHeapMB,
maxRetainedMB. It spawns `callers` goroutines alternating `esme.SubmitSM` / `smsc.DeliverSM` and
samples goroutine count, heap, pending depth, and window occupancy every 250 ms.

Generic SMSC peer (standalone development/performance tool; not a public library package):

```bash
go run ./cmd/smpp-sim -listen :2775 -window 4096 -tx-queue 4096
```

---

## Verification status

Latest full local Phase 21 validation archive: 2026-11-07, branch
`feature/product-neutral-library-dlr-completeness`, commit
`dff70d0968332e9fa78f4a4c80af744d00ab429a`. Host metadata recorded WSL2/Linux amd64,
default Go 1.27.0, and the pinned golangci-lint v2.14.0.

| Check | Result |
| --- | --- |
| Go 1.25.0 toolchain and gofmt | PASS |
| Go 1.25.0 unit/API/no-panic/race | PASS |
| Go 1.25.0 five fuzz smokes | PASS |
| Go 1.25.0 protocol/codec/session/perflab benchmark smokes | PASS |
| Go 1.25.0 no-unsafe and coverage floor | PASS |
| Go 1.27.0 toolchain and gofmt | PASS |
| Go 1.27.0 unit/API/no-panic/race | PASS |
| Go 1.27.0 five fuzz smokes | PASS |
| Go 1.27.0 protocol/codec/session/perflab benchmark smokes | PASS |
| Go 1.27.0 lint, no-unsafe and coverage floor | PASS |
| Overall local script | **37 PASS, 0 FAIL, 0 BLOCKED** |

The archive recorded total coverage of 84.4% on Go 1.25.0 and 84.3% on Go 1.27.0.
The untracked `.bench/` directory was preserved.

A follow-up parser correctness fix is on source commit
`ce14ae06a3d9096937c5c54a99a93aa8f695ece7`. Implementation-head GitHub Actions run
[42369084385](https://github.com/majiddarvishan/go-smpp/actions/runs/42369084385) passed
both Go 1.25.x and Go 1.27.x jobs, including DLR fuzz smoke. The subsequent
documentation-only PR-head run [42369351000](https://github.com/majiddarvishan/go-smpp/actions/runs/42369351000)
also passed both jobs. 
OBS-01 safe observability and packet-capture audit completed on source/test commit
`5e3f4678b1516c5eb4c5f84fd0af48d44ffe49a5`. Final source/test-head GitHub Actions run
[42371052716](https://github.com/majiddarvishan/go-smpp/actions/runs/42371052716) passed
both Go 1.25.x and Go 1.27.x jobs, including gofmt, unit, race, fuzz, benchmarks,
Phase 17 smoke and no-unsafe; Go 1.27.x also passed lint and coverage. Raw PDU capture
remains explicit opt-in; panic, decoder, callback and protocol Reason text are not
formatted into default logs/events/error strings.

The 2-second Phase 17 CI smoke is not the 60-second reference
acceptance; a current-HEAD 60-second run remains open before release.


---

## Repo rules that constrain any change (from AGENTS.md)

- Go 1.25 minimum (newer through 1.27 supported; `docs/DECISIONS.md` D2). TCP only; TLS layered over TCP. No X.25.
- One shared session core for client and server. **No goroutine-per-message.**
- Outbound sequence numbers in `0x1..0x7fffffff`; inbound accepted through `0xffffffff` and
  echoed back exactly.
- No unbounded queues anywhere.
- **Never auto-resubmit an ambiguous request** across reconnect/rebind.
- Framer is poisoned on fatal corruption; there is no stream resynchronization.
- Default max PDU size 1 MiB.
- `codec` and `protocol` must not depend on `session` or `transport`.
- No mutable global registry state.
- Per-PDU logging off by default.
- Performance work must be profile-driven.
- `unsafe` is banned; CI enforces it.
- **The library never panics** (D5, maintainer's requirement). Call user code on a session goroutine through a
  `recover` (`session/panic.go`), guard nil/zero receivers with `ErrNotInitialized`, and add new exported API to
  `nopanic_test.go`, which fails until you do. Keep `errors.As` targets off the hot path: each one is a heap allocation.

Performance contract: **100,000 aggregate bidirectional request PDUs/s** on the reference
machine (Linux/amd64, 8 cores, 10 GB) using the minimum practical session count.

---

## Layering (enforced by `architecture_test.go`, AST-based)

```
protocol  ← (nothing)
encoding  ← (nothing)
transport ← (nothing)
codec     ← protocol
message   ← protocol, encoding
session   ← protocol, codec, transport
client    ← protocol, session, transport, message
server    ← protocol, session, transport, message
```

Adding an import outside this table fails the test. Keep it that way.

---

## Session invariants worth re-reading before touching `session/`

- Exactly **three** long-lived goroutines per session: `rxLoop`, `txLoop`, `deadlines.run`.
  Liveness supervision (session-init, inactivity, enquire_link) is three fixed slots in
  `deadlines`' shared heap, not a goroutine (Task 2.4). The one exception: an idle enquire_link
  probe runs in a short-lived goroutine of its own — see `docs/CONCURRENCY.md`.
- Exactly **one** terminal completion per request, across all of: response, timeout,
  context cancellation, fatal error, session loss.
- `request()` ordering is load-bearing: window acquire → `machine.BeginOutbound` → completion
  chan → pending insert (retry on `ErrSequenceInUse`) → encode → enqueue → select.
- `txLoop` commits `machine.CompleteInbound` for responses **before** the write becomes visible.
- Response deadlines start only after the write is fully handed to the transport.

---

## Known local gaps

- No git tags yet. (`LICENSE`, `.gitignore`, and `.golangci.yml` landed in Phase 0.)
- Active Phase 21 work branch on 2026-11-07: `feature/product-neutral-library-dlr-completeness` (PR #8, Draft). Fetch current remote refs before reporting branch state.
- Left before `v1.0.0`: the maintainer reviews the interface-compatibility bullet in
  `docs/API_COMPATIBILITY.md`; Phase 17 acceptance is re-run on the commit to be tagged (the
  published run is from `c0f699c`); then the tag, which the maintainer is holding.

---

## Review deliverables in this folder

| File | Purpose |
| --- | --- |
| `ARCHITECTURE_OVERVIEW.md` | What the project is: package map, dependency DAG, session model |
| `CODE_REVIEW.md` | Full audit — bugs, performance, architecture, clean code, testing, security |
| `FINDINGS.md` | Prioritization view: tiers by impact × likelihood ÷ effort |
| `TASKS.md` | Execution view: phased plan with acceptance criteria and `[x]` tracking |

`TASKS.md` is where completion is registered. Mark a task `[x]` when it lands.
