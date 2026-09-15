# Session Handoff

## Current state

Branch: `main`

Phases 0 through 3 are complete. Phase 4 has not started.

Phase 3 implementation commit: `2efd8d8ae5c554db11640f95be689655153c4848`.

Network transport is TCP; X.25 is out of scope. Optional TLS will be layered over TCP.

## Phase 3 completed

- central command registry in `codec`
- central TLV registry in `codec`
- vendor-specific command registration
- vendor-specific TLV registration
- callback slots for command body encode/decode and typed TLV encode/decode
- concurrency-safe `RegistryBuilder`
- immutable frozen `Registry` snapshots for active hot paths
- idempotent `Freeze`
- explicit duplicate command/TLV registration errors
- strict vs compatible unknown command/TLV handling
- compatible unknown TLVs can remain in their raw ordered representation
- registry policy applies only after framing/TLV structural validation; it cannot weaken fatal framing rules
- concurrent configuration/read tests and race-detector coverage
- zero-allocation registry lookup benchmarks

## Encoding requirements added

Message/encoding work remains scheduled for Phase 12, but the required scope is now explicit:

- GSM 03.38/GSM 7-bit default alphabet
- GSM 7-bit extension table and septet packing/unpacking
- strict UCS-2/BMP helpers
- UTF-16BE with surrogate-pair support for supplementary characters such as emoji
- strict UCS-2 remains distinct from UTF-16BE-with-surrogates because peer/carrier emoji support varies
- higher-level encoding selection may prefer GSM 7-bit when representable and use configured Unicode fallback otherwise
- multipart sizing uses encoded septets/code units and UDH overhead, not Go rune count

## Validation performed

GitHub Actions validated the Phase 3 implementation on Go 1.26.8 / Linux amd64:

```text
go test ./...
go test -race ./...
go test -run '^$' -bench=. -benchmem ./protocol ./codec
```

All passed.

Phase 3 registry benchmark snapshot on the GitHub runner (AMD EPYC 7763):

```text
BenchmarkRegistryCommandLookup   3.441 ns/op   0 B/op   0 allocs/op
BenchmarkRegistryTLVLookup      23.23  ns/op   0 B/op   0 allocs/op
```

Existing Phase 2 hot paths remained allocation-free in the same run.

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
- active hot paths use frozen immutable registries, not mutable global registration state.
- GSM 7-bit and Unicode/emoji support remains a required Phase 12 deliverable.
- SMPP 3.4 complete first, architecture 5.0-aware.
- no `unsafe` initially; minimal runtime dependencies.

## Exact next task

Start **Phase 4 — Essential SMPP 3.4 PDUs** from `PLAN.md`.

Implement bind transmitter/receiver/transceiver, unbind, enquire_link, generic_nack, submit_sm/submit_sm_resp and deliver_sm/deliver_sm_resp on top of the existing codec and frozen registry infrastructure. Preserve optional TLVs and preserve inbound sequence numbers exactly, including values above `0x7fffffff` through `0xffffffff`.

Do not begin session networking before the essential PDU encode/decode layer and specification-driven vectors are complete.
