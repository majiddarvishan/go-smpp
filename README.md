# go-smpp

A high-performance SMPP stack for Go, designed for both ESME/client and SMSC/server use cases.

## Project goals

- Shared protocol core for client and server roles.
- SMPP 3.4 complete support first, with an architecture that is SMPP 5.0-aware from day one.
- Initial production hot path: `submit_sm` / `submit_sm_resp` and `deliver_sm` / `deliver_sm_resp`.
- Synchronous public API backed by an asynchronous, pipelined protocol engine.
- Automatic reconnect without hidden automatic resubmission of ambiguous requests.
- Extensible PDU/TLV registry, including vendor-specific TLVs.
- Encoding support in separate packages within this repository.
- Minimal external dependencies; prefer the Go standard library.
- Primary target: Linux/amd64.
- No `unsafe` in the initial implementation.

## Performance target

The reference target is **100,000 SMPP request PDUs per second aggregate, bidirectionally** (requests sent + requests received), while using the minimum practical number of SMPP connections/sessions. Mandatory SMPP responses are additional work and are included in end-to-end benchmark load.

Reference machine:

- CPU: 8 cores
- RAM: 10 GB
- OS/arch: Linux/amd64

Performance work starts with a single-session benchmark and scales to the smallest session count required to sustain the target. No fixed claim is made that every peer/network can achieve the target on one session; RTT, peer window limits, throttling, and network conditions are part of the benchmark contract.

## Protocol scope

The design is based on:

- SMPP v3.4, Issue 1.2, 12-Oct-1999
- SMPP v5.0, 19-Feb-2003

SMPP 3.4 is the first complete compatibility target. SMPP 5.0 features are added on the same core rather than as a separate stack.

## TLS strategy

The SMPP engine is transport-agnostic and works over a small `net.Conn`-compatible abstraction. Plain TCP and TLS are provided using the Go standard library (`net` and `crypto/tls`). TLS configuration stays in the transport layer and does not leak into PDU/session logic.

## Planning and project context

- [`PLAN.md`](PLAN.md) — phased implementation plan and progress checklist.
- [`AGENTS.md`](AGENTS.md) — entry point for coding agents.
- [`.codex/PROJECT_CONTEXT.md`](.codex/PROJECT_CONTEXT.md) — project goals and constraints.
- [`.codex/DECISIONS.md`](.codex/DECISIONS.md) — architectural decisions.
- [`.codex/ARCHITECTURE.md`](.codex/ARCHITECTURE.md) — target architecture and package boundaries.
- [`.codex/PERFORMANCE.md`](.codex/PERFORMANCE.md) — performance contract and benchmark strategy.
- [`.codex/BACKLOG.md`](.codex/BACKLOG.md) — deferred protocol/features backlog.
- [`.codex/SESSION.md`](.codex/SESSION.md) — handoff/current-state notes.

No protocol implementation has been started yet. The repository is currently in the planning/bootstrap stage.
