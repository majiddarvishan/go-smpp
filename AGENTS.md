# Agent Instructions

Before changing code or plans, read in order: `PLAN.md`, `.codex/PROJECT_CONTEXT.md`, `.codex/DECISIONS.md`, `.codex/ARCHITECTURE.md`, `.codex/PERFORMANCE.md`, `.codex/BACKLOG.md`, `.codex/SESSION.md`.

## Mandatory working rules

- `PLAN.md` is the implementation progress source of truth; completed and verified steps become `[x]`.
- Minimum supported Go version is 1.26.
- The network transport is TCP. Do not implement X.25. TLS is optional and, when enabled, is layered over TCP.
- SMPP 3.4 is the first complete target; the core stays SMPP 5.0-aware.
- Client/ESME and server/SMSC share protocol/session core logic.
- Public API is synchronous/context-aware; the engine is asynchronous and pipelined.
- Do not create a goroutine per message/request or per timeout.
- Every outbound request expecting a response has a configurable protocol response timeout after full transport dispatch.
- Session Init, Enquire Link and inactivity timeout are first-class features.
- Response, timeout, cancellation, fatal protocol failure and session loss may race, but local completion/window release happens exactly once.
- Public active runtime APIs documented as concurrent-safe must be safe for multiple goroutines without caller serialization.
- Run `go test -race` as concurrency code is introduced.
- No unbounded queues or pending-request growth.
- Never silently auto-resubmit ambiguous requests after connection loss.
- Fatal structural/framing corruption closes the offending TCP connection/session after structured diagnostic logging. Never attempt heuristic stream resynchronization.
- In server mode, malformed input must not terminate the listener or unrelated sessions.
- Prefer the Go standard library. Document any runtime dependency before adding it.
- Do not use `unsafe` unless profiling proves a need and the decision is recorded/reviewed first.
- Codec/protocol packages must not depend on session/network packages.
- Vendor-specific TLVs/commands must be extensible through registries; avoid mutable global hot-path registry state.
- Per-PDU logging is disabled by default; mandatory fatal-protocol diagnostic logging is an exception.
- Performance optimizations must be profile-driven and benchmarked.

## Performance contract

- 100,000 aggregate bidirectional request PDUs/s (`requests sent + requests received`).
- Required responses are extra processing and remain enabled in end-to-end tests.
- Reference machine: Linux/amd64, 8 CPU cores, 10 GB RAM.
- Find the minimum practical TCP session/connection count; benchmark one session first.

## Scope discipline

The first production path is bind/session management plus `submit_sm` and `deliver_sm` request/response. Other SMPP 3.4 operations remain required and tracked in the plan/backlog.

At the end of a meaningful implementation session, update `.codex/SESSION.md` with branch/commit, completed work, tests/benchmarks, unresolved issues, and exact next task.
