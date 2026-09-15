# Session Handoff

## Current state

Branch: `main`

Phase 0 through Phase 3 are complete. Phase 4 implementation is now in progress.

Network transport is TCP; X.25 is out of scope. Optional TLS will be layered over TCP.

## Phase 4 work started

The initial Phase 4 implementation adds the shared SMPP 3.4 PDU body model and command codecs for:

- `bind_transmitter` / response
- `bind_receiver` / response
- `bind_transceiver` / response
- `unbind` / response
- `enquire_link` / response
- `generic_nack`
- `submit_sm` / `submit_sm_resp`
- `deliver_sm` / `deliver_sm_resp`

It also adds an SMPP 3.4 registry builder so vendor-specific commands/TLVs can still be registered before freezing the immutable session registry.

Important compatibility behavior being preserved:

- responses echo inbound sequence numbers exactly, including values through `0xffffffff`
- optional TLVs remain ordered and duplicate-preserving
- unknown/vendor TLVs on supported message PDUs are preserved raw
- `short_message` and non-empty `message_payload` cannot both carry message data
- `sm_length=255` is rejected as invalid for SMPP 3.4
- malformed mandatory variable-length fields and invalid TLV framing remain structural/fatal errors; framing safety rules are not weakened by PDU support
- `deliver_sm_resp` accepts a header-only response for deployed-peer interoperability, while the encoder emits the C-Octet NULL body defined by SMPP 3.4

## Validation required before Phase 4 can be marked complete

Run the GitHub Actions Go 1.26 pipeline and require all of the following to pass:

```text
go test ./...
go test -race ./...
go test -run '^$' -bench=. -benchmem ./protocol ./codec
```

Do not mark Phase 4 `[x]` until the new specification-vector and round-trip tests pass in CI.

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

Complete and validate **Phase 4 — Essential SMPP 3.4 PDUs**. Fix any CI/test failures, then mark the Phase 4 checklist `[x]` and update this handoff with the verified commit and test results.
