# Session Handoff

## Current state

Branch: `main`

Phase 0 and Phase 1 are complete. Phase 2 has not started.

The project now has a Go module and package skeleton, protocol primitives, initial error taxonomy, tests/benchmarks, and Go 1.26 CI. Network transport is TCP; X.25 is out of scope. Optional TLS will be layered over TCP.

## Phase 1 completed

- module: `github.com/majiddarvishan/go-smpp`
- minimum Go version: 1.26
- package boundary skeleton: root, `protocol`, `codec`, `session`, `transport`, `client`, `server`, `encoding`, `message`
- automated dependency-direction test
- SMPP 3.4 command IDs and command-status constants
- `SequenceNumber`, TON, NPI, `DataCoding`, session state primitives
- shared SMPP 3.4 / 5.0 profile model (`0x34` / `0x50`)
- network-order uint32 primitive helpers
- recoverable semantic protocol error type
- fatal structural/framing error categories
- typed session timeout metadata (`response`, `session_init`, `enquire_link`, `inactivity`)
- primitive unit tests and allocation benchmarks
- CI workflow using Go 1.26 with unit/race tests and primitive benchmarks

## Validation performed

Local environment provides Go 1.23.2, so the code was smoke-tested by temporarily lowering only the local `go` directive to 1.23; repository `go.mod` remains Go 1.26. The code uses no language/API feature newer than that local compiler in Phase 1.

Commands passed locally:

```text
go test ./...
go test -race ./...
go test -bench=. -benchmem ./protocol
```

Primitive benchmark snapshot on the available Linux/amd64 EPYC environment:

```text
BenchmarkPutUint32           ~0.28 ns/op   0 B/op   0 allocs/op
BenchmarkReadUint32          ~0.29 ns/op   0 B/op   0 allocs/op
BenchmarkCommandResponseID   ~0.29 ns/op   0 B/op   0 allocs/op
```

These numbers are only a Phase 1 microbenchmark baseline, not a throughput claim.

## Important requirements to preserve

- 100k aggregate bidirectional request-PDU/s target on 8 cores / 10 GB RAM with minimum practical TCP connection count.
- synchronous/context-aware public API over an asynchronous pipelined engine.
- thread-safe active runtime APIs; no goroutine-per-message model.
- configurable request-response timeout; Session Init, Enquire Link and inactivity handling.
- auto-reconnect without hidden auto-resubmit.
- fatal malformed/framing PDU => mandatory structured error log + close offending connection; no stream resynchronization.
- SMPP 3.4 complete first, architecture 5.0-aware.
- no `unsafe` initially; minimal runtime dependencies.

## Exact next task

Start **Phase 2 — Binary framing and codec foundation** from `PLAN.md`.

Begin with the fixed 16-byte header and a TCP-stream framer driven by `command_length`. The framer must handle fragmented/coalesced reads and must classify unsafe structural framing errors as fatal so the owning session can log and close the connection later.

Do not start session networking or PDU business logic before the codec/framing foundation is tested and benchmarked.
