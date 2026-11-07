# Project Context

## Project identity

Repository: `github.com/majiddarvishan/go-smpp`

Purpose: a high-performance, general-purpose Go implementation of SMPP 3.4 with a negotiated SMPP 5.0 extension surface. It provides reusable ESME/client and SMSC/server APIs over one shared protocol/session core.

**Product-neutrality is mandatory:** this repository must not import, name, document or encode assumptions for any downstream application. See `.codex/DECISIONS.md` D-046 and `AGENTS.md`.

## Protocol and toolchain

Primary references:

- SMPP Protocol Specification v3.4, Issue 1.2, 12-Oct-1999.
- SMPP Protocol Specification v5.0, 19-Feb-2003.

Minimum Go version: **1.25**. CI validates Go 1.25.x and 1.27.x. The library uses only the Go standard library; no third-party runtime dependencies, cgo or direct `unsafe` imports.

Transport: TCP, with optional TLS-over-TCP. X.25 is out of scope.

## Public architecture

- `protocol`: command/status vocabulary, PDU bodies, TLVs, sequence/address/data-coding types and negotiated capabilities.
- `codec`: SMPP PDU encoding/decoding, stream framing and immutable command/TLV registries.
- `transport`: TCP/TLS and caller-supplied connection primitives.
- `session`: shared ESME/SMSC state machine, bounded request correlation/window, response/liveness deadlines and observability.
- `client`: ESME dial/bind/reconnect facade.
- `server`: SMSC listener, bind authentication, inbound request dispatch and server-originated operations.
- `encoding` and `message`: GSM 03.38, UCS-2/UTF-16BE, binary, UDH/SAR segmentation/reassembly and optional receipt parsing.
- `internal/perflab`: private performance acceptance/benchmark harness.
- `cmd/smpp-sim`: standalone generic SMSC peer for local development/performance measurements only; it is not a public package or a dependency of the library.
- `examples/`: generic ESME and SMSC usage examples.

The public dependency direction is enforced by `architecture_test.go`. Codec/protocol must not depend on session/network packages. Message encoding stays outside the low-level PDU codec.

## Core contracts

- One shared protocol/session engine for ESME and SMSC roles.
- SMPP 3.4 command and standard TLV coverage; SMPP 5.0 features are enabled only after capability negotiation.
- Synchronous/context-aware public request API over asynchronous pipelined RX/TX paths.
- No goroutine per PDU/request and no unbounded queues or pending work.
- Local sequence numbers use 1..0x7fffffff; inbound non-zero interoperability sequence values through 0xffffffff are accepted and echoed exactly.
- Response deadlines begin only after complete transport dispatch.
- Exactly-once local completion/window release across response, timeout, cancellation, fatal failure and session loss.
- Reconnect creates a fresh session and never silently replays ambiguous requests.
- Fatal structural/framing corruption is fail-closed and isolated to the offending TCP session.
- GSM 03.38/GSM7, strict UCS-2 and UTF-16BE surrogate handling remain explicit, separate choices.
- Raw PDU tracing is opt-in; default observability does not expose credentials or message bodies.

## Verified project status

- Phases 0–18 and 20 are implemented and have corresponding tests/documentation.
- Phase 17 reference acceptance passed on the documented Linux/amd64 reference host: **713,932 aggregate bidirectional request PDUs/s for 60 seconds, minimum passing session count 1**, with required responses enabled. Exact environment and caveats are in `docs/PERFORMANCE.md`.
- Phase 19 release readiness is open for the first production-ready version tag. A fresh reference run on the combined current `main` after later library additions remains a release regression gate.
- Phase 21 product-neutrality cleanup and generic DLR/protocol helper completeness are implemented and verified on `feature/product-neutral-library-dlr-completeness` (PR #8; source fix `ce14ae06a3d9096937c5c54a99a93aa8f695ece7`). The final Phase 21 source/test head and subsequent documentation heads passed Go 1.25.x/1.27.x CI; PR #8 remains Draft pending maintainer review.
- INT-01 offline fixture framework is implemented in test-only code at PR head `9fc5ed44830365f8c9b24f5e1c2f3a4c9c261b40`. CI run `42375378458` passed Go 1.25.x/1.27.x. The current manifest contains only five synthetic SMPP 3.4 PDUs; real-peer evidence and the representative-peer behavior matrix remain open.

## Recent generic library improvements

- PR #3: bounded, opt-in suppressed/deferred inbound response handling; Session-scoped one-shot reply ownership.
- PR #4: GSM7 extension ESC-pair-safe multipart splitting across UDH/SAR boundaries.
- PR #6: optional reconnect jitter and stop on explicitly permanent bind credential rejection; legacy defaults unchanged.
- PR #5: additive 16-bit UDH and unpacked-GSM7/binary segmentation helpers.
- PR #7: optional generic DLR parser with independent text-first identifier and TLV-first standard-state selection, conflict diagnostics, API snapshot and no-panic tests.

The above are reusable protocol/library capabilities; no downstream product workflow is part of this repository.

## Current work and next steps

Phase 21 P0 work and OBS-01 safe observability/packet-capture audit are complete. Remaining work is intentionally P1/release-scoped:

1. Add reviewed, anonymized real-peer exchanges to the new offline fixture harness and build the representative-peer strict/compatible behavior matrix from observed behavior only. Synthetic fixtures do not close INT-01.
2. Re-run the 60-second Phase 17 reference acceptance on the combined current HEAD after post-acceptance library additions; a 2-second CI smoke is not a substitute.
3. Create the first production-ready version tag only after the reference regression and release checklist pass.

PR #8 remains Draft and has not been merged.