# Session Handoff

## Current state

Branch: `main`

Phase 0, Phase 1 and Phase 2 are complete. Phase 3 has not started.

Phase 2 implementation baseline commit: `690fd060f0bc507c3e38f3842884e005165126e8`.

Network transport is TCP; X.25 is out of scope. Optional TLS will be layered over TCP.

## Phase 2 completed

- fixed 16-octet SMPP header encode/decode
- receive-side sequence-number interoperability through `0xffffffff`; local outbound generation remains limited to `0x7fffffff`
- TCP byte-stream framer driven by `command_length`
- fragmented and coalesced stream handling
- zero-copy emission for complete PDUs already present in one input buffer
- bounded buffering for fragmented PDUs
- configurable maximum PDU size; default 1 MiB
- 1/2/4-octet integer field helpers
- C-Octet String and Octet String helpers
- ordered TLV scanning/encoding with duplicate tag preservation
- zero-allocation TLV scan API
- fatal malformed/framing classification for invalid lengths and structural corruption
- framer poisoning after fatal framing or fatal body-decode error; later bytes cannot be resynchronized/interpreted on that connection
- partial-frame detection on stream finalization
- malformed-frame, fragmentation/coalescing and high-sequence tests
- fuzz seeds for stream framing
- codec microbenchmarks

The codec does not own sockets or logging. The mandatory `fatal protocol error -> structured log -> close offending TCP connection` integration remains explicitly assigned to the later session/transport/observability phases.

## Validation performed

GitHub Actions ran with Go 1.26.8 on Linux/amd64 and passed:

```text
go test ./...
go test -race ./...
go test -run '^$' -bench=. -benchmem ./protocol ./codec
```

CI codec benchmark snapshot on the GitHub runner (AMD EPYC 7763):

```text
BenchmarkDecodeHeader          0.3131 ns/op     0 B/op   0 allocs/op
BenchmarkFramerCompletePDU    10.29 ns/op       0 B/op   0 allocs/op
                               7774.27 MB/s
BenchmarkScanTLVs             27.49 ns/op       0 B/op   0 allocs/op
                               2328.12 MB/s
```

These are microbenchmarks, not the end-to-end 100k request-PDU/s acceptance result.

## Important requirements to preserve

- 100k aggregate bidirectional request-PDU/s target on 8 cores / 10 GB RAM with minimum practical TCP connection count.
- locally generated sequence numbers: `1..0x7fffffff`.
- inbound sequence interoperability: accept `1..0xffffffff` and preserve the exact value in responses.
- synchronous/context-aware public API over an asynchronous pipelined engine.
- thread-safe active runtime APIs; no goroutine-per-message model.
- configurable request-response timeout; Session Init, Enquire Link and inactivity handling.
- auto-reconnect without hidden auto-resubmit.
- fatal malformed/framing PDU => mandatory structured error log + close offending connection; no stream resynchronization.
- SMPP 3.4 complete first, architecture 5.0-aware.
- no `unsafe` initially; minimal runtime dependencies.

## Exact next task

Start **Phase 3 — Extensible registries** from `PLAN.md`.

Implement explicit PDU-command and TLV registries, including vendor-specific registration. Prefer immutable/frozen registry snapshots for active-session hot paths. Registry behavior must be concurrency-safe and must not weaken the fatal framing rules already implemented in the codec.
