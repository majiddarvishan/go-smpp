# Session Handoff

## Current state

Repository: `github.com/majiddarvishan/go-smpp`
Base: `main` at `8effc6317d8141e36976b0a21bdbe190e2761116`
Branch: `feature/product-neutral-library-dlr-completeness`
Latest verified source/test commit: `5e3f4678b1516c5eb4c5f84fd0af48d44ffe49a5`
Latest INT-01 fixture framework/test commit with full Go CI: `9fc5ed44830365f8c9b24f5e1c2f3a4c9c261b40`
Current PR head at handoff: `5cc4a7add9c171b70ced186a0095ecf936b35369` (tracking-only follow-up to the tested fixture framework commit above).
PR: [#8 — feat: complete product-neutral DLR and registered_delivery helpers](https://github.com/majiddarvishan/go-smpp/pull/8)

The repository is a general-purpose, product-neutral SMPP 3.4 / 5.0 Go library. No downstream application or product may be imported, named, or used to define public APIs. See D-046, D-047, D-049, D-050, D-051, `AGENTS.md`, and `.codex/ARCHITECTURE.md`.

PR #8 is **open and Draft**. It has not been merged.

## Phase 21 — completed P0 work

- Removed the downstream-product integration review document and consumer-specific references from maintained docs/handoff files.
- Recorded the product-neutral dependency/documentation boundary in `AGENTS.md`, `.codex/DECISIONS.md`, and `.codex/ARCHITECTURE.md`.
- Completed generic DLR text extraction, raw counts/dates/error values, ID/state precedence and conflict evidence.
- Added strict IA5/Latin-1/UCS-2 handling and explicit unsupported-data-coding errors; packed GSM7 is not guessed.
- Added standard `deliver_sm` receipt TLV parsing and strict duplicate/length checks.
- Corrected response-only TLV semantics: `delivery_failure_reason` and `additional_status_info_text` are retained when peer-supplied but do not identify a `deliver_sm` receipt by themselves. Added regression tests.
- Added the typed `protocol.MessageState` accessor.
- Added lossless `registered_delivery` decode/encode helpers and exhaustive 256-octet round-trip coverage.
- Documented `SMSCReceiptModeSuccessOnly` as a peer-specific alias for SMPP 3.4 reserved value `0b11`; the library does not infer peer semantics.
- Reconciled historical backlog and Phase 12–17 status.

## OBS-01 — completed

- Confirmed `PacketTracer` is absent by default. When configured, it emits metadata only unless `TraceRawPDU=true`; the raw option has no effect without a tracer.
- Confirmed raw mode supplies an owned copy of the complete unredacted PDU. The library has no built-in PCAP/file writer or universal redactor; unknown/vendor TLVs make generic sanitization unsafe.
- Documented raw-frame sensitivity (clear-text bind/outbind credentials, system IDs, addresses, message bodies, IDs and arbitrary TLVs), metadata-only preference, peer-aware allowlist sanitization, full-frame dropping, isolated/restricted temporary capture and synchronous callback constraints.
- Prevented caller-controlled panic values from entering `PanicError.Error()`, session logs or Observer events. `PanicError.Value` remains available for explicit inspection and `Unwrap` behavior.
- Prevented custom decoder error text and one-way handler error text from entering default Observer events/logs.
- Prevented arbitrary `SemanticError.Reason` and `FatalError.Reason` from entering default error strings, fatal logs or fatal Observer events. The fields remain available for explicit programmatic inspection.
- Added regression coverage for authenticator panic containing the bind password, one-way handler error text, custom decoder error text, fatal decoder Reason and protocol error formatting.

## INT-01 — offline fixture framework implemented; real-peer evidence pending

- Added the test-only manifest `testdata/interop/manifest.json` (schema version 1) and root external test harness `interop_fixtures_test.go`; no public API, production code, or runtime dependency changed.
- The manifest distinguishes `synthetic` from `real-peer-anonymized` evidence. Strict JSON decoding rejects unknown fields/trailing data; validation checks fixture IDs, protocol version, opaque peer aliases, capture provenance, expected header/body/decode outcomes, and real-peer sanitization attestations.
- Bind receiver/transmitter/transceiver request/response and outbind frames are rejected from persistent fixtures. Real-peer entries require a reviewed peer-aware allowlist declaration, bind/outbind exclusion, replacement of sensitive values, and removal or explicit review of unknown TLVs.
- Deterministic offline replay feeds each direction as an independent TCP byte stream using repeatable fragmented/coalesced chunk sizes, decodes each PDU, asserts expected header/body, supports reject expectations, and optionally checks exact re-encoding.
- Five synthetic SMPP 3.4 PDUs are present: enquire_link, enquire_link_resp, submit_sm, submit_sm_resp, and a synthetic deliver_sm receipt. They validate the harness only; no live-peer/vendor compatibility is claimed.
- Focused fixture tests are included in the repository-wide `go test ./...` CI. No real peer evidence was available in this session, so INT-01 remains open.

## Verification evidence — 2026-11-07

- Local `scripts/phase21-validation.sh` archive on commit `dff70d0968332e9fa78f4a4c80af744d00ab429a`: **37 PASS, 0 FAIL, 0 BLOCKED**, covering Go 1.25.0 and Go 1.27.0. Coverage was 84.4% / 84.3%; `golangci-lint` reported 0 issues; untracked `.bench/` was preserved.
- Follow-up DLR correctness fix `ce14ae06a3d9096937c5c54a99a93aa8f695ece7` prevents response-only TLVs from identifying a `deliver_sm` receipt.
- OBS-01 source/test changes are recorded in commits `82c06bddafc482c6e336c10c84f5d904d12c50c3`, `b3ed89c58b28c38d0ce7e0d43b1bf89a0c8a0a59`, and `5e3f4678b1516c5eb4c5f84fd0af48d44ffe49a5`.
- Final source/test-head GitHub Actions run [42371052716](https://github.com/majiddarvishan/go-smpp/actions/runs/42371052716) passed both Go 1.25.x and Go 1.27.x: gofmt, unit, race, fuzz smoke, protocol/codec benchmarks, Phase 17 performance/resource smoke and no-unsafe; Go 1.27.x also passed lint and coverage.
- The final Phase 21 documentation/status PR head `64af94935c5c07af1c7995813c3b1a228c0c244d` passed both Go jobs in [run 42371554356](https://github.com/majiddarvishan/go-smpp/actions/runs/42371554356).
- INT-01 fixture framework commit `9fc5ed44830365f8c9b24f5e1c2f3a4c9c261b40` passed both Go jobs in [run 42375378458](https://github.com/majiddarvishan/go-smpp/actions/runs/42375378458): gofmt, unit, race, fuzz smoke, protocol/codec benchmarks, Phase 17 development performance/resource smoke and no-unsafe; Go 1.27.x also passed lint and coverage.
- The Phase 17 CI result is a **2-second smoke**, not the 60-second reference acceptance.

## Remaining P1 / release gates

- **INT-01:** Real, anonymized SMPP peer interoperability fixtures and representative-peer strict/compatible behavior matrix. The offline harness and synthetic-only manifest are implemented; real peer evidence is still required.
- **PERF-01:** Re-run the 60-second Phase 17 reference acceptance on the combined current HEAD after post-acceptance library additions.
- **REL-01:** First production-ready version tag after the reference regression and release checklist pass.

Do not treat the 2-second CI smoke as the 60-second performance acceptance. Keep PR #8 Draft pending maintainer review; do not merge or tag automatically.
