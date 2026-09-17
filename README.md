# go-smpp

High-performance SMPP stack for Go, designed for both ESME/client and SMSC/server roles.

The project targets SMPP 3.4 first while keeping the core architecture ready for SMPP 5.0 extensions. The network transport is TCP; optional TLS is layered over TCP.

## Current status

Phase 0 through Phase 16 are complete and verified. Phase 17 is in progress: the performance laboratory, minimal SMPP peer simulator, TCP/TLS bidirectional benchmarks, timer/window benchmarks, and profiling scripts are now established, and optimization work is driven by measured end-to-end profiles rather than microbenchmarks alone.

See `PLAN.md` for detailed implementation progress and `.codex/` for architecture decisions, performance targets, backlog, and session handoff notes.

## Design targets

- shared protocol/session core for client and server roles
- TCP transport with optional TLS-over-TCP
- SMPP 3.4 first, SMPP 5.0-aware architecture
- synchronous/context-aware application API over an asynchronous wire engine
- out-of-order response correlation by `sequence_number`
- locally generated sequence numbers in `1..0x7fffffff`
- inbound interoperability sequence range `1..0xffffffff`, echoed exactly in responses
- fatal malformed/framing input closes only the offending TCP session and is logged structurally
- bounded pending work; no goroutine-per-message architecture
- GSM 03.38/GSM 7-bit plus Unicode/emoji support in the message/encoding layer
- target: 100k aggregate bidirectional request PDUs/s on Linux/amd64, 8 cores / 10 GB RAM, using the minimum practical session count

## Implemented protocol foundation

The repository includes:

- fixed 16-byte SMPP header encode/decode
- TCP stream framing with fragmented/coalesced PDU handling
- configurable maximum PDU size with a 1 MiB default
- fatal framing/body corruption detection with poisoned-framer behavior
- C-Octet String, Octet String and integer field helpers
- ordered TLV scanning/encoding with duplicate and unknown/vendor TLV preservation
- concurrency-safe registry builders and immutable frozen command/TLV lookup snapshots
- vendor-specific command/TLV registration
- complete SMPP 3.4 command/body coverage, including bind, submit/deliver, data_sm, submit_multi, query/cancel/replace, alert_notification, outbind, enquire_link, unbind and generic_nack
- response sequence preservation through `0xffffffff`
- `short_message` / `message_payload` exclusivity checks
- specification-driven codec vectors and race-tested registry code

## Session and transport core

The shared runtime now includes:

- ESME/SMSC state validation for Open, Outbound, Bound_TX, Bound_RX, Bound_TRX, Unbound and Closed
- race-safe bind/unbind lifecycle
- plain TCP dial/listen and TLS-over-TCP adapters
- caller-supplied `net.Conn` support
- exactly two long-lived RX/TX paths per active session, not one goroutine per request
- bounded pending-request correlation
- session-local outbound sequence generation with wrap inside `1..0x7fffffff`
- inbound interoperability sequence handling through `0xffffffff`
- synchronous/context-aware typed APIs for SMPP 3.4 request/response operations plus explicit one-way APIs for `outbind` and `alert_notification` over the asynchronous engine
- out-of-order response correlation and concurrent request safety
- simultaneous inbound `deliver_sm` while outbound `submit_sm` remains outstanding
- exactly-once terminal request completion across response, caller cancellation, future timeout completion, fatal protocol failure, and session loss
- mandatory structured fatal-protocol logging followed by closing the offending TCP connection
- tests for fragmented reads, short writes, TLS-over-TCP, blocked TX interruption, high-concurrency correlation, invalid state operations, and race safety

The runtime now also includes configurable request windowing/backpressure, shared response deadlines and liveness timers, client reconnect/rebind without ambiguous request replay, and SMSC/server listener/authentication/submit/deliver support with per-connection isolation and configurable session limits.

Plain TCP sessions also use bounded opportunistic TX batching by default (`TXBatchItems=32`, `TXBatchBytes=64 KiB`). Already-queued PDUs can be sent with `net.Buffers` scatter/gather on `*net.TCPConn`; an isolated PDU is never delayed merely to fill a batch, and `TXBatchItems=1` disables batching.


## SMPP 5.0 extensions

The same protocol/session core now supports a profile-selected SMPP 5.0 registry and per-session capability negotiation. Successful bind responses automatically advertise `sc_interface_version` to SMPP 3.4/5.0 peers while legacy pre-3.4 peers are not sent TLVs. The negotiated capability view is bounded by both the configured local profile and the version actually advertised in the ESME bind request.

SMPP 5.0 coverage includes the six Cell Broadcast request/response command IDs (`broadcast_sm`, `query_broadcast_sm`, and `cancel_broadcast_sm`), the v5 command-status additions, `congestion_state`, billing, Cell Broadcast, number-portability, and source/destination network/node-identification TLVs. `congestion_state` is accepted on successful, failed, and header-only response PDUs and is surfaced through a lightweight `FlowController` callback; the hard outstanding-request window remains a separate safety bound. Unknown/duplicate TLVs continue to retain their ordered wire representation.

## Observability

Each session exposes lock-free snapshots for request/response counts, protocol and liveness timeouts, Enquire Link activity, decode/fatal failures, RTT samples, congestion feedback, and outstanding-window state. Dialed clients expose reconnect and reconnect-failure counters. Optional event and packet-trace callbacks are disabled by default; enabling raw packet tracing is an explicit opt-in because raw PDUs may contain credentials or message content. Normal per-PDU logging remains disabled, while fatal malformed/framing input always emits its structured diagnostic before the offending TCP session is closed.

## Message encoding

The message layer supports GSM 03.38/GSM 7-bit (including the extension table and septet packing), strict UCS-2/BMP, UTF-16BE surrogate pairs for explicitly enabled emoji interoperability, binary payloads, 8-bit concatenation UDH, and SMPP SAR TLVs. Multipart thresholds are calculated from encoded septets/code units rather than Go rune count.
## SMPP 3.4 completeness

The shared codec registry covers all 27 SMPP 3.4 command/response identifiers and all 44 standard SMPP 3.4 TLV tag identifiers. The protocol package exposes the complete named SMPP 3.4 command-status set and query message states. `submit_multi` supports SME and Distribution List destinations plus per-destination unsuccessful results. `outbind` follows the SMPP 3.4 `Open -> Outbound -> bind_receiver -> Bound_RX` lifecycle; `alert_notification` and `outbind` are one-way and never consume request-window/pending correlation capacity.




Phase 17 development validation has sustained more than 100k request PDUs/s for 60 seconds with a single localhost TCP session while keeping required SMPP responses enabled and outstanding work bounded. This is a development-run result, not the reference-machine acceptance result. The final acceptance workflow is available as `SMPP reference performance acceptance` and requires a self-hosted runner labeled `smpp-reference` that satisfies the documented 8-core / 10-GB Linux/amd64 contract.

## Performance laboratory

Phase 16 adds a minimal SMSC-side simulator plus repeatable Go benchmarks for codec-only, in-memory session, localhost TCP, TLS-over-TCP, timeout/window, and bidirectional traffic paths. The default end-to-end benchmark starts with one SMPP session; additional session counts are selected explicitly with `SMPP_BENCH_SESSIONS` so connection count is increased only when measurement requires it.

Run the simulator locally with:

```bash
go run ./cmd/smpp-sim -listen 127.0.0.1:2775
```

It accepts transmitter/receiver/transceiver binds and responds to `submit_sm`, `enquire_link`, and `unbind`. Optional TLS is enabled with `-tls-cert` and `-tls-key`. Per-PDU logs remain off; the simulator emits only periodic aggregate statistics and fatal protocol diagnostics.

Run the benchmark laboratory with:

```bash
SMPP_BENCH_SESSIONS=1,2,4 BENCHTIME=3s ./scripts/bench.sh .bench
```

The script records benchmark output plus CPU, heap, mutex, block, scheduler-trace, and GC artifacts. These development-machine results are diagnostic only; the Phase 17 100k acceptance result is reserved for the documented Linux/amd64 8-core / 10-GB reference machine.
