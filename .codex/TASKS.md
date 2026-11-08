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

- [ ] **INT-01:** Add opt-in interoperability fixtures from independent SMPP 3.4 simulator peers and deployed peers; no vendor normalization or quirks without evidence. The offline framework is implemented and reviewed local simulator fixtures now cover both roles, including the ESME-originated SubmitSM exchange; deployed-peer evidence/behavior matrix remains open.
  - [x] **INT-01A:** Define a versioned, product-neutral fixture manifest that separates synthetic examples from anonymized real-peer evidence.
  - [x] **INT-01B:** Validate fixture provenance, peer aliases, protocol version, direction, expected decode outcome, and mandatory sanitization metadata for real-peer captures.
  - [x] **INT-01C:** Add deterministic offline replay through the SMPP stream framer and codec, with header/body assertions and optional exact wire round-trip checks.
  - [x] **INT-01D:** Add only explicitly synthetic examples until reviewed, anonymized real-peer exchanges are supplied; do not claim live-peer interoperability from synthetic fixtures.
  - [x] **INT-01E:** Add an opt-in local integration probe for separate SMPP 3.4 simulator components in both ESME-to-SMSC and SMSC-to-ESME roles. Enforce one connection per peer and sequential execution; obtain credentials only through hidden prompts or environment variables; export only sanitized allowlisted PDU frames and safe metadata. Do not add production API or runtime dependencies, and do not present simulator evidence as deployed-peer/vendor interoperability. Final probe/sanitizer commit `05a6924563872e9a108b194e021dee70ec0e6c35` verified by CI run `42380908792` on Go 1.25.x/1.27.x, including the opt-in sanitizer tests and CLI compile.
  - [x] **INT-01F:** Run the probe against the local SMPP 3.4 simulator components, review the sanitized archive, and add only the two approved sanitized `simulator-peer` fixtures (`simulator-smsc-peer-001`, `simulator-esme-peer-002`) to the manifest. Bind/DLR exchanges passed; both peers omitted `unbind_resp`, and the ESME submit action was not observed in the original 5-second window.
  - [ ] **INT-01G:** Add reviewed, anonymized deployed-peer exchanges and build the representative-peer strict/compatible behavior matrix from observed behavior only.
- [x] **INT-01H:** Improve the opt-in probe's ESME-originated `submit_sm` observation workflow with a longer configurable window, an explicit just-in-time prompt, and sanitized Unbind request/response diagnostics. Keep all changes build-tagged and outside the public API. Final two-role diagnostic wiring and the 15-second window passed Go 1.25.x/1.27.x CI in run `42386492884` at commit `ca5dfad5df1afdd9d80f8d742634149b59a6fd7e`.
  - [x] **INT-01I:** Re-run the ESME phase with the operator-controlled ESME CLI and verify the complete single-session flow. Archive `go-smpp-interop.2KhVNK.tar.gz` reviewed: TRX bind/response, enquire_link/response, deliver_sm/response and ESME-originated submit_sm/response all passed on `connection_id=1`; Submit reported `session_match=true`. The final PDU pair `0x00000004`/`0x80000004` is Submit/SubmitResp, not Unbind. The report’s `unbind=UNSUPPORTED` is the separately skipped SMSC-initiated Unbind because this ESME simulator does not support it. The loopback FIN/FIN pair confirms graceful local shutdown after the probe; the active connection did not RST. New reviewed fixture `simulator-esme-peer-002-submit` (`INT-01-PEER-002-002`) added.
  - [x] **INT-01K:** In the opt-in ESME/server probe, classify unsupported **SMSC-initiated** Unbind (SMSC → ESME) as `UNSUPPORTED`, skip it by default and close the local session; retain explicit opt-in for peers that support it. Report metadata identifies `operation_kind`, `initiator_role`, `peer_role`, and `request_direction`, preventing confusion with ESME-originated Unbind. Public library Unbind semantics are unchanged. Regression tests pass; full Go 1.25.x/1.27.x CI passed at latest branch head `9f039bd2d2df428718a3660cf501c6b0b9189873` in [run 42485046872](https://github.com/majiddarvishan/go-smpp/actions/runs/42485046872).
  - [x] **INT-01L:** Diagnose ESME/server bind/session loss. The `wvS2vr` run proves the ESME TCP peer sent Bind and then returned EOF before any Bind Response; go-smpp closed only after that EOF, and the later write error was secondary. The 2026-11-08 repeat (`2KhVNK`) completed Bind, EnquireLink, DeliverSM and SubmitSM on one session. FIN from server port 4000 followed by peer FIN aligns with the probe’s local shutdown; no RST occurred on the active connection. Other RST/ACK packets target different ephemeral ports and are consistent with connection attempts while the listener was unavailable; the filtered capture omitted SYNs and cannot attribute those attempts to a process. The intermittent peer EOF was not reproduced; no public Session/Transport change is justified. Test-only failed-write header and shutdown-close diagnostics remain in place. Decimal-only probe MessageID is preserved; generic DLR IDs remain opaque. Latest full Go 1.25.x/1.27.x CI passed at `9f039bd2d2df428718a3660cf501c6b0b9189873` in [run 42485046872](https://github.com/majiddarvishan/go-smpp/actions/runs/42485046872). If peer EOF recurs, ESME-side logs are required to identify the simulator’s reason.
  - [x] **INT-01J:** Make the test-only ESME bind gate observe the actual successful `bind_transceiver_resp` write rather than polling transient session state; preserve a safe, allowlisted session-close cause when the response is not written. Add regression coverage for response/close races and peer EOF. Correct the CLI prompt and document the exact READY → bind → OBSERVE → CLI submit sequence. Final head `0cf19df7fe3dd145a07b839745a3be5bd4870358` passed full CI on Go 1.25.x and Go 1.27.x in [run 42393189702](https://github.com/majiddarvishan/go-smpp/actions/runs/42393189702).
- [x] **OBS-01:** Audit every packet-trace, decode-failure, panic, callback-error and fatal-diagnostic output path. Keep raw PDU capture explicitly opt-in; ensure caller-controlled panic/error strings and protocol `Reason` fields cannot leak through library logs/events/default error strings; document raw-frame sensitivity, metadata-only operation, synchronous callback constraints and safe temporary capture/redaction practices. Regression tests cover authenticator panic, one-way handler error, custom decoder error, fatal decoder Reason and protocol error strings. Source/test CI run `42371052716` and final documentation/status PR-head run `42371554356` passed Go 1.25.x/1.27.x.
- [ ] **PERF-01:** Re-run the 60-second reference acceptance on a combined current `main` after post-acceptance library changes; do not substitute hosted-runner smoke for the reference machine.
- [ ] **REL-01:** Create the first production-ready version tag only after the release checklist and current-head performance gate are satisfied.

## Execution rules

1. Register scope in this file and `PLAN.md` before source or documentation implementation.
2. Keep changes on an isolated branch and submit a PR.
3. Keep public APIs additive/backward-compatible unless a correctness, security or protocol-compliance defect requires a reviewed exception.
4. Do not add dependencies, `unsafe`, per-PDU goroutines, unbounded queues, or consumer-specific behavior.
5. Mark tasks complete only after tests/evidence pass. Record exact commits and CI runs in `.codex/SESSION.md`.
