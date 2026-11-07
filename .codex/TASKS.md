# Active Library Tasks

Repository: `github.com/majiddarvishan/go-smpp`

## Architecture rule

This repository is a **general-purpose, product-neutral SMPP library**. It must not import, name, document, or encode assumptions for any downstream application. All behavior belongs to the protocol/library only when it is reusable across independent ESME/SMSC applications. The standalone `cmd/smpp-sim` is a generic SMSC peer used only for this repository's local development and performance measurements; it is not part of the exported library API and must not be coupled to a consumer product.

## Completed baseline

- [x] SMPP 3.4 command and standard TLV coverage.
- [x] SMPP 5.0 negotiated extension surface currently implemented by the shared core.
- [x] Shared ESME/SMSC session engine, TCP/TLS transport, bounded window/deadlines, reconnect without request replay.
- [x] GSM 03.38, UCS-2/UTF-16BE, UDH/SAR and binary message helpers.
- [x] Optional generic `message.ParseDeliveryReceipt(protocol.DeliverSM)` (Phase 20; PR #7 merged; see `.codex/DLR_PARSER_CONTRACT.md`).
- [x] Phase 17 reference acceptance on the documented reference host (713,932 request PDUs/s for 60 seconds, minimum passing sessions=1; see `docs/PERFORMANCE.md`). A combined-HEAD rerun after subsequent library additions remains a separate release regression task.

## P0 — Product-neutrality and generic protocol completeness (Phase 21)

Work branch: `feature/product-neutral-library-dlr-completeness`.
Task registration was committed before source or documentation cleanup.

- [x] **BOUNDARY-01:** Remove all references to downstream products, consumer-specific requirements, and integration instructions from library docs and `.codex` state. Keep protocol facts and generic development-tool notes.
- [x] **BOUNDARY-02:** Record the product-neutral dependency/documentation rule in `AGENTS.md`, `.codex/DECISIONS.md`, and `.codex/ARCHITECTURE.md`. State explicitly that no downstream product may be imported or named in the library.
- [x] **DLR-08:** Extend the generic DLR parser to expose standard text fields `sub`, `dlvrd`, `submit date`, `done date`, `err`, and `text` while preserving their raw string values and leading zeroes.
- [x] **DLR-09:** Parse standard `deliver_sm` receipt TLVs (`receipted_message_id`, `message_state`, and optional `network_error_code`) with strict length/duplicate handling. Tolerate and validate `delivery_failure_reason` and `additional_status_info_text` if peer-supplied on `deliver_sm`, but do not treat these response-oriented TLVs as standard receipt evidence or let either alone identify a DLR.
- [x] **DLR-10:** Support ASCII-compatible SMPP `data_coding` values IA5 and Latin-1 for receipt text. Return a distinct unsupported-data-coding error for a marked receipt whose text cannot safely be decoded; do not guess packed GSM7 or other national alphabets.
- [x] **DLR-11:** Add an additive typed accessor returning `protocol.MessageState` from the selected standard state without changing existing exported field types.
- [x] **PROTO-01:** Complete and verify general-purpose `registered_delivery` bit-field parse/encode helpers. Preserve every reserved bit on round-trip. Document that SMSC receipt value `0b11` is reserved by SMPP 3.4 while `SMSCReceiptModeSuccessOnly` is only a peer-agreed, non-portable alias; decode/encode must not infer peer semantics.
- [x] **AUDIT-01:** Reconcile the historical backlog against implemented Phase 12–14/17 evidence. Mark completed items only where source/tests/docs prove them; retain genuinely unsupported SMPP 5.0/vendor features as open.
- [x] **DOC-01:** Align `PLAN.md`, `README.md`, `.codex/PROJECT_CONTEXT.md`, and `.codex/SESSION.md` with the published Phase 17 reference evidence and current release gate. Keep the exact evidence caveat in `docs/PERFORMANCE.md`.
- [x] **TEST-01:** Add table-driven DLR and exhaustive 256-octet registered_delivery tests; update API snapshots and no-panic coverage for additive exports.
- [x] **CI-01:** Pass gofmt, lint, unit, race, coverage, fuzz, API/no-panic and benchmark smoke on Go 1.25.x and Go 1.27.x. Implementation run `42369084385` and subsequent documentation-only PR-head run `42369351000` passed both jobs; PR #8 remains Draft pending maintainer review and has not been merged.

## P1 — Generic library follow-ups

- [ ] **INT-01:** Add opt-in interoperability fixtures from real, anonymized SMPP peers; no vendor normalization or quirks without evidence.
- [x] **OBS-01:** Audit every packet-trace, decode-failure, panic, callback-error and fatal-diagnostic output path. Keep raw PDU capture explicitly opt-in; ensure caller-controlled panic/error strings and protocol `Reason` fields cannot leak through library logs/events/default error strings; document raw-frame sensitivity, metadata-only operation, synchronous callback constraints and safe temporary capture/redaction practices. Regression tests cover authenticator panic, one-way handler error, custom decoder error, fatal decoder Reason and protocol error strings. Source/test CI run `42371052716` and final documentation/status PR-head run `42371554356` passed Go 1.25.x/1.27.x.
- [ ] **PERF-01:** Re-run the 60-second reference acceptance on a combined current `main` after post-acceptance library changes; do not substitute hosted-runner smoke for the reference machine.
- [ ] **REL-01:** Create the first production-ready version tag only after the release checklist and current-head performance gate are satisfied.

## Execution rules

1. Register scope in this file and `PLAN.md` before source or documentation implementation.
2. Keep changes on an isolated branch and submit a PR.
3. Keep public APIs additive/backward-compatible unless a correctness, security or protocol-compliance defect requires a reviewed exception.
4. Do not add dependencies, `unsafe`, per-PDU goroutines, unbounded queues, or consumer-specific behavior.
5. Mark tasks complete only after tests/evidence pass. Record exact commits and CI runs in `.codex/SESSION.md`.
