# CLAUDE.md — working notes for go-smpp

Module: `github.com/majiddarvishan/go-smpp` · Go 1.26.0 · **zero external dependencies**
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

Simulator:

```bash
go run ./cmd/smpp-sim -listen :2775 -window 4096 -tx-queue 4096
```

---

## Verification status on this host (go1.26.5 windows/amd64)

| Check | Result |
| --- | --- |
| `go build ./...` | clean |
| `go vet ./...` | clean |
| `go test ./...` | **all packages pass** |
| `gofmt -l .` | **12 files non-compliant** (see CODE_REVIEW.md §4) |
| `go test -race ./...` | **NOT RUN — could not run here** |

The race detector requires cgo and a C compiler. This host has neither:

```
go: -race requires cgo; enable cgo by setting CGO_ENABLED=1
cgo: C compiler "gcc" not found: exec: "gcc": executable file not found in %PATH%
```

Race-freedom is therefore **unverified locally**. CI (`.github/workflows/ci.yml`,
ubuntu-latest) does run `go test -race ./...`. Do not claim race cleanliness from a
Windows-only run.

---

## Repo rules that constrain any change (from AGENTS.md)

- Go 1.26 minimum. TCP only; TLS layered over TCP. No X.25.
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
- README claims phases 0–18 complete, Phase 19 in progress; the outstanding blockers are the
  Phase 17 reference-machine throughput result, publishing that evidence, and the first tag.

---

## Review deliverables in this folder

| File | Purpose |
| --- | --- |
| `ARCHITECTURE_OVERVIEW.md` | What the project is: package map, dependency DAG, session model |
| `CODE_REVIEW.md` | Full audit — bugs, performance, architecture, clean code, testing, security |
| `FINDINGS.md` | Prioritization view: tiers by impact × likelihood ÷ effort |
| `TASKS.md` | Execution view: phased plan with acceptance criteria and `[x]` tracking |

`TASKS.md` is where completion is registered. Mark a task `[x]` when it lands.
