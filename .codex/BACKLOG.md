# Backlog

This file tracks intentionally deferred functionality. Items here are not forgotten; they are simply not required before the first submit/deliver milestone.

## SMPP 3.4 operations after initial hot path

- [ ] `data_sm` / `data_sm_resp`
- [ ] `submit_multi` / `submit_multi_resp`
- [ ] `query_sm` / `query_sm_resp`
- [ ] `cancel_sm` / `cancel_sm_resp`
- [ ] `replace_sm` / `replace_sm_resp`
- [ ] `alert_notification`
- [ ] `outbind`
- [ ] full standard command-status coverage
- [ ] full standard TLV coverage

These are mandatory for the SMPP 3.4 completeness phase even though they are deferred from the first working milestone.

## Message/encoding features

- [ ] GSM 03.38/GSM 7-bit default alphabet encode/decode
- [ ] GSM 7-bit extension table
- [ ] GSM 7-bit septet packing/unpacking
- [ ] strict UCS-2/BMP encode/decode helpers
- [ ] UTF-16BE Unicode encode/decode with surrogate-pair support for supplementary characters such as emoji
- [ ] encoding selection helper: prefer GSM 7-bit when representable, otherwise use configured Unicode behavior
- [ ] keep strict UCS-2 distinct from UTF-16BE-with-surrogates so peer/carrier emoji compatibility is explicit
- [ ] binary message convenience API
- [ ] UDH concatenation/segmentation using encoded septet/code-unit length rather than Go rune count
- [ ] SAR TLV segmentation
- [ ] multipart reassembly
- [ ] delivery-receipt convenience parsing/format helpers
- [ ] conformance tests for GSM extension characters, Unicode BMP text, surrogate pairs/emoji, and multipart boundaries

Encoding/message logic belongs in separate packages in this repository and must not be required by the low-level PDU codec.

## SMPP 5.0

- [ ] `congestion_state` support and flow-controller integration
- [ ] expanded network error classifications
- [ ] successful-delivery-only receipt mode
- [ ] service-type restriction statuses
- [ ] `billing_identification`
- [ ] number-portability TLVs
- [ ] source/destination network and node identification TLVs
- [ ] enhanced USSD-related support
- [ ] Cell Broadcast: `broadcast_sm`
- [ ] Cell Broadcast: `query_broadcast_sm`
- [ ] Cell Broadcast: `cancel_broadcast_sm`
- [ ] related broadcast TLVs/statuses

## Vendor interoperability

- [ ] registry examples for vendor-specific TLVs
- [ ] registry examples for vendor-specific commands
- [ ] optional peer-quirk profiles after real interoperability data is available
- [ ] strict vs compatible decoder/session modes informed by real peers
- [ ] document peers/carriers that accept UTF-16 surrogate pairs for emoji under SMPP Unicode/UCS-2 data coding

## Operations and observability

- [ ] Prometheus adapter/example (only if dependency policy allows it; core remains dependency-light)
- [ ] OpenTelemetry adapter/example (optional external package, not core requirement)
- [ ] structured packet trace formatter
- [ ] PCAP/debug interoperability tooling if needed

## Performance research backlog

Only pursue these after profiling identifies a need:

- [ ] compare sharded pending table vs fixed-slot/ring correlation
- [ ] compare write batching strategies
- [ ] compare receive-buffer pooling strategies
- [ ] compare timer wheel vs bucketed deadlines vs heap batching
- [ ] investigate runtime/network polling behavior under many sessions
- [ ] consider platform-specific socket tuning exposed as optional configuration
- [ ] evaluate `unsafe` only if a measured, material bottleneck remains and an architectural decision explicitly approves it

## Documentation/release backlog

- [ ] interoperability matrix against known SMPP peers
- [ ] migration/versioning policy after API stabilizes
- [ ] examples for common ESME transmitter/receiver/transceiver roles
- [ ] examples for SMSC/server role
- [ ] vendor extension guide
- [ ] GSM 7-bit and Unicode/emoji encoding guide
