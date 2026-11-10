# General-Purpose Library Backlog

This file tracks intentionally deferred, reusable SMPP library work. The implementation source of truth is `PLAN.md`; completed items below are marked only where the corresponding implementation and tests exist.

## SMPP 3.4 protocol surface

- [x] `data_sm` / `data_sm_resp`
- [x] `submit_multi` / `submit_multi_resp`
- [x] `query_sm` / `query_sm_resp`
- [x] `cancel_sm` / `cancel_sm_resp`
- [x] `replace_sm` / `replace_sm_resp`
- [x] `alert_notification`
- [x] `outbind` lifecycle and one-way semantics
- [x] Standard command-status constants and codec coverage
- [x] All 44 standard SMPP 3.4 TLV tags and registry coverage
- [x] Lossless `registered_delivery` bit-field helpers (Phase 21; exhaustive 256-octet round-trip tests and final Go 1.25/1.27 CI pass)

## Message and encoding

- [x] GSM 03.38/GSM 7-bit default alphabet encode/decode
- [x] GSM 7-bit extension table
- [x] GSM 7-bit septet packing/unpacking
- [x] Strict UCS-2/BMP encode/decode helpers
- [x] UTF-16BE Unicode encode/decode with surrogate-pair support
- [x] Explicit GSM7/Unicode encoding selection
- [x] Strict UCS-2 distinct from UTF-16BE-with-surrogates
- [x] Binary message helpers
- [x] UDH concatenation/segmentation using encoded septet/code-unit lengths
- [x] SAR TLV segmentation
- [x] Multipart reassembly
- [x] Optional generic delivery-receipt parsing (Phase 20 base ID/state parser; Phase 21 completes standard text fields, encodings and receipt TLV handling)
- [x] Complete generic DLR metadata extraction: standard text fields, receipt TLVs, IA5/Latin-1 and explicit unsupported-DCS behavior (Phase 21; response-only TLVs do not identify a DeliverSM receipt)
- [ ] Delivery-receipt formatting helpers (deferred unless a concrete interoperable representation is required)
- [x] GSM extension, Unicode BMP, surrogate-pair and multipart boundary tests

Encoding/message logic stays outside the low-level PDU codec.

## SMPP 5.0

- [x] Negotiated profile/capability support
- [x] `congestion_state` TLV and flow-controller extension point
- [x] Service-type restriction status constants
- [x] `billing_identification` TLV registration/preservation
- [x] Number-portability TLV registration/preservation
- [x] Source/destination network and node identification TLVs
- [x] USSD service-operation TLV registration/preservation
- [x] Cell Broadcast: `broadcast_sm`
- [x] Cell Broadcast: `query_broadcast_sm`
- [x] Cell Broadcast: `cancel_broadcast_sm`
- [x] Related broadcast TLVs and command statuses
- [x] Raw `network_error_code` TLV registration/preservation and generic receipt extraction
- [ ] Typed network-error classification helpers (only after enum semantics are verified against the specification and peer evidence)
- [ ] Higher-level USSD workflow helpers (optional; generic `data_sm` plus TLV preservation may already be sufficient)
- [x] `registered_delivery` field decode/encode with reserved bits preserved; SMPP 3.4 reserves SMSC receipt value `0b11`; the named success-only alias is peer-specific and covered by tests/docs

## Vendor interoperability

- [x] Versioned offline fixture manifest and deterministic framer/codec replay harness (synthetic examples only; no real-peer compatibility claim).
- [ ] Registry examples for vendor-specific TLVs
- [ ] Registry examples for vendor-specific commands
- [ ] Optional peer-quirk profiles only after real interoperability data exists
- [ ] Strict/compatible behavior matrix against representative SMPP peers
- [ ] Document peers that accept UTF-16 surrogate pairs under SMPP Unicode/UCS-2 data coding
- [ ] Anonymized real-peer DLR fixtures (text-only, TLV-only, mixed and vendor status variants)

## Operations and observability

- [ ] Prometheus adapter/example (optional external package; core remains dependency-light)
- [ ] OpenTelemetry adapter/example (optional external package)
- [ ] Structured packet trace formatter
- [ ] PCAP/debug interoperability tooling if a concrete support need arises
- [x] Review raw packet capture guidance and redaction risks (OBS-01, PR #8; raw capture remains explicit opt-in and is documented as sensitive)

## Performance research and release

Only pursue performance changes after profiling identifies a material need.

- [ ] Re-run the reference acceptance on the combined current `main` after post-acceptance library changes
- [ ] Compare sharded pending table vs fixed-slot/ring correlation only if profiles justify it
- [ ] Compare write batching strategies only if profiles justify it
- [ ] Compare receive-buffer pooling strategies only if profiles justify it
- [ ] Compare timer wheel vs bucketed deadlines vs heap batching only if profiles justify it
- [ ] Investigate runtime/network polling under many sessions only if profiles justify it
- [ ] Consider platform-specific socket tuning only if a reusable need is demonstrated
- [ ] Evaluate `unsafe` only with measured material benefit and a separately reviewed decision; current policy remains no direct `unsafe`

## Documentation and release

- [ ] Interoperability matrix against known SMPP peers
- [x] Public API compatibility/versioning policy documented in `docs/API_COMPATIBILITY.md`
- [ ] Examples for ESME transmitter/receiver/transceiver roles beyond the current minimal client example
- [x] Generic SMSC/server example
- [x] Vendor extension guide
- [x] GSM 7-bit and Unicode/emoji interoperability guide
- [ ] First production-ready version tag (after current-head performance and release gates)
