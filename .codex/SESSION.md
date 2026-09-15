# Session Handoff

## Current state

Branch: `main`

Phase 0 through Phase 4 are complete. Phase 5 has not started.

Phase 4 implementation commit: `60719fb8cc2a08402491bc3d31e5ffe73470650c`.

Network transport is TCP; X.25 is out of scope. Optional TLS will be layered over TCP.

## Phase 4 completed

Typed SMPP 3.4 body models and encode/decode support are implemented for:

- `bind_transmitter` / response
- `bind_receiver` / response
- `bind_transceiver` / response
- `unbind` / response
- `enquire_link` / response
- `generic_nack`
- `submit_sm` / `submit_sm_resp`
- `deliver_sm` / `deliver_sm_resp`

The phase also adds:

- generic single-frame `DecodePDU` / `EncodePDU` dispatch through the frozen command registry
- an SMPP 3.4 registry builder that remains extensible for vendor commands/TLVs before freeze
- ordered optional-TLV preservation, including duplicate and unknown/vendor tags
- raw borrowed optional-parameter values with explicit lifetime documentation
- `ResponseHeader` sequence echoing without truncating receive-side interoperability values through `0xffffffff`
- `sm_length` validation with 255 rejected and `short_message` bounded to 254 octets
- `short_message` versus non-empty `message_payload` exclusivity checks
- bind response handling for `sc_interface_version`
- specification-driven bind/submit vectors, header-only PDU tests, deliver round trips, duplicate-TLV preservation, high-sequence response tests and malformed-body tests

Interoperability note: SMPP 3.4 specifies an empty C-Octet `message_id` body in `deliver_sm_resp`. The encoder emits that form. The decoder also accepts a header-only `deliver_sm_resp` because this is seen in deployed peers; a present body is still structurally validated.

Fatal structural errors remain distinct from recoverable semantic PDU errors. Phase 4 does not weaken the existing `fatal framing/body corruption -> poisoned framer -> later session closes TCP connection` rule.

## Validation performed

GitHub Actions run `34983301401` used Go 1.26.8 on Linux/amd64 and passed all required checks:

```text
go test ./...
go test -race ./...
go test -run '^$' -bench=. -benchmem ./protocol ./codec
```

The Phase 4 codec tests passed under both normal and race builds.

CI benchmark snapshot on the GitHub runner (AMD EPYC 9V74):

```text
BenchmarkDecodeHeader            0.2740 ns/op     0 B/op   0 allocs/op
BenchmarkFramerCompletePDU       6.867 ns/op      0 B/op   0 allocs/op
                                11649.48 MB/s
BenchmarkScanTLVs                22.31 ns/op      0 B/op   0 allocs/op
                                 2868.29 MB/s
BenchmarkRegistryCommandLookup   2.732 ns/op      0 B/op   0 allocs/op
BenchmarkRegistryTLVLookup       18.57 ns/op      0 B/op   0 allocs/op
```

These remain microbenchmarks and are not the end-to-end 100k request-PDU/s acceptance result.

## Important requirements to preserve

- 100k aggregate bidirectional request-PDU/s target on 8 cores / 10 GB RAM with minimum practical TCP connection count.
- locally generated sequence numbers: `1..0x7fffffff`.
- inbound sequence interoperability: accept `1..0xffffffff` and preserve the exact value in responses.
- synchronous/context-aware public API over an asynchronous pipelined engine.
- thread-safe active runtime APIs; no goroutine-per-message model.
- configurable request-response timeout; Session Init, Enquire Link and inactivity handling.
- auto-reconnect without hidden auto-resubmit.
- fatal malformed/framing PDU => mandatory structured error log + close offending connection; no stream resynchronization.
- GSM 03.38/GSM 7-bit, strict UCS-2, and UTF-16BE surrogate-pair/emoji support remain required in Phase 12.
- SMPP 3.4 complete first, architecture 5.0-aware.
- no `unsafe` initially; minimal runtime dependencies.

## Exact next task

Start **Phase 5 — Shared session state machine** from `PLAN.md`.

Implement race-free shared client/server session states and legal-operation validation first. Keep codec independent of session state. Bind/unbind lifecycle and fatal decoder/framing failure transitions must be idempotent and safe when concurrent send/receive/close/timeout activity races.
