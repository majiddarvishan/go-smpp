# go-smpp

High-performance SMPP stack for Go, designed for both ESME/client and SMSC/server roles.

The project targets SMPP 3.4 first while keeping the core architecture ready for SMPP 5.0 extensions. The network transport is TCP; optional TLS is layered over TCP.

## Current status

Phase 0 through Phase 14 are complete and verified. SMPP 3.4 command coverage is complete and the shared core now implements the planned SMPP 5.0 extensions; the next implementation phase is Phase 15: low-overhead observability.

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


## SMPP 5.0 extensions

The same protocol/session core now supports a profile-selected SMPP 5.0 registry and per-session capability negotiation. Successful bind responses automatically advertise `sc_interface_version` to SMPP 3.4/5.0 peers while legacy pre-3.4 peers are not sent TLVs. The negotiated capability view is bounded by both the configured local profile and the version actually advertised in the ESME bind request.

SMPP 5.0 coverage includes the six Cell Broadcast request/response command IDs (`broadcast_sm`, `query_broadcast_sm`, and `cancel_broadcast_sm`), the v5 command-status additions, `congestion_state`, billing, Cell Broadcast, number-portability, and source/destination network/node-identification TLVs. `congestion_state` is accepted on successful, failed, and header-only response PDUs and is surfaced through a lightweight `FlowController` callback; the hard outstanding-request window remains a separate safety bound. Unknown/duplicate TLVs continue to retain their ordered wire representation.


## Message encoding

The message layer supports GSM 03.38/GSM 7-bit (including the extension table and septet packing), strict UCS-2/BMP, UTF-16BE surrogate pairs for explicitly enabled emoji interoperability, binary payloads, 8-bit concatenation UDH, and SMPP SAR TLVs. Multipart thresholds are calculated from encoded septets/code units rather than Go rune count.
## SMPP 3.4 completeness

The shared codec registry covers all 27 SMPP 3.4 command/response identifiers and all 44 standard SMPP 3.4 TLV tag identifiers. The protocol package exposes the complete named SMPP 3.4 command-status set and query message states. `submit_multi` supports SME and Distribution List destinations plus per-destination unsuccessful results. `outbind` follows the SMPP 3.4 `Open -> Outbound -> bind_receiver -> Bound_RX` lifecycle; `alert_notification` and `outbind` are one-way and never consume request-window/pending correlation capacity.

