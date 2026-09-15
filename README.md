# go-smpp

High-performance SMPP stack for Go, designed for both ESME/client and SMSC/server roles.

The project targets SMPP 3.4 first while keeping the core architecture ready for SMPP 5.0 extensions. The network transport is TCP; optional TLS is layered over TCP.

## Current status

Implementation is in progress. Phase 0 through Phase 4 are complete. Phases 5–7 are currently being implemented together: shared session state machine, TCP/TLS transport boundary, and asynchronous pipelined request/response engine with synchronous context-aware APIs.

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

The repository already includes:

- fixed 16-byte SMPP header encode/decode
- TCP stream framing with fragmented/coalesced PDU handling
- configurable maximum PDU size with a 1 MiB default
- fatal framing/body corruption detection with poisoned-framer behavior
- C-Octet String, Octet String and integer field helpers
- ordered TLV scanning/encoding with duplicate and unknown/vendor TLV preservation
- concurrency-safe registry builders and immutable frozen command/TLV lookup snapshots
- vendor-specific command/TLV registration
- SMPP 3.4 bind, unbind, enquire_link, generic_nack, submit_sm and deliver_sm request/response body codecs
- response sequence preservation through `0xffffffff`
- `short_message` / `message_payload` exclusivity checks
- specification-driven codec vectors and race-tested registry code

## Session and transport work

The active implementation work adds:

- shared ESME/SMSC state validation
- plain TCP dial/listen and TLS-over-TCP adapters
- caller-supplied `net.Conn` support
- independent long-lived RX/TX session paths
- bounded pending correlation
- synchronous `Bind*`, `SubmitSM`, `DeliverSM`, `EnquireLink`, and `Unbind` APIs
- race-safe concurrent requests and out-of-order response correlation
- fatal protocol diagnostic logging followed by connection close

Request response timers, Session Init, Enquire Link scheduling, inactivity timers, window/backpressure tuning, reconnect/rebind, and the full SMSC server policy remain in later phases.
