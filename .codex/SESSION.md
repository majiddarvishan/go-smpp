# Session Handoff

## Current state

Repository: `github.com/majiddarvishan/go-smpp`
Base: `main` at `8effc6317d8141e36976b0a21bdbe190e2761116`
Branch: `feature/product-neutral-library-dlr-completeness`
Latest previously verified source/test commit: `5e3f4678b1516c5eb4c5f84fd0af48d44ffe49a5`
Latest INT-01 fixture framework/test commit with full Go CI: `9fc5ed44830365f8c9b24f5e1c2f3a4c9c261b40`
Latest simulator probe code commit: `ca5dfad5df1afdd9d80f8d742634149b59a6fd7e` (Go 1.25.x/1.27.x CI passed in run `42386492884`)
PR #8 remains open and Draft; consult the live GitHub PR for its current head and check state.
PR: [#8 — feat: complete product-neutral DLR and registered_delivery helpers](https://github.com/majiddarvishan/go-smpp/pull/8)

The repository is a general-purpose, product-neutral SMPP 3.4 / 5.0 Go library. No downstream application or product may be imported, named, or used to define public APIs. See D-046, D-047, D-049, D-050, D-051, D-052, D-053, `AGENTS.md`, and `.codex/ARCHITECTURE.md`.

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
- The manifest contains five synthetic SMPP 3.4 PDUs plus two reviewed sanitized `simulator-peer` fixtures. Simulator evidence is kept distinct from deployed-peer/vendor compatibility.
- Focused fixture tests are included in repository-wide `go test ./...` CI. No deployed real-peer evidence is available yet, so INT-01G remains open.


## INT-01E — local two-way simulator probe (implemented; CI verified; first live simulator run reviewed)

- The user confirmed both simulator components and the repository are on the same Linux host. SMSC simulator endpoint: `127.0.0.1:3000`, System ID `client_0`, System Type `smpp`, TRX, SMPP 3.4, plain TCP, one connection. ESME simulator connects to `127.0.0.1:4000`, System ID `smpp_client_0`, System Type `smpp`, TRX, SMPP 3.4, plain TCP, one connection. Do not record the bind password in this file.
- Added build-tagged local probe code under `cmd/interop-probe` and `internal/interopprobe`; invoke only through `scripts/interop-local.sh` or `go run -tags=interop`. Normal library builds do not include this tool.
- Mode `all` runs sequentially: go-smpp ESME connects to SMSC simulator and sends enquire_link/submit_sm with registered_delivery `0x03`; then go-smpp SMSC listens for the ESME simulator, sends enquire_link and a synthetic deliver_sm receipt, and observes responses. One connection per peer.
- The probe uses the existing client/server APIs; no production API or third-party dependency was added. Bind credentials are prompted with hidden terminal input or supplied by environment variables. The report does not include passwords, message content, or original message IDs/addresses.
- Raw tracing is enabled only in the local test process. Bind/outbind frames are discarded before body decoding; unsupported commands and non-allowlisted TLVs are dropped; addresses and IDs are replaced; message content is removed or replaced with a synthetic receipt. Only sanitized candidate frames are written to the restricted temporary archive. New candidates default to `reviewed=false`; the first two candidates were manually reviewed and added as `simulator-peer` fixtures.
- The fixture validator now distinguishes `simulator-peer` from `real-peer-anonymized`; simulator candidates require `wire-capture-sanitized`, opaque peer/evidence IDs, and reviewed sanitization. Simulator evidence is not deployed SMSC/ESME vendor evidence.
- Initial probe/sanitizer commit `05a6924563872e9a108b194e021dee70ec0e6c35` passed full CI in [run 42380908792](https://github.com/majiddarvishan/go-smpp/actions/runs/42380908792). The live two-way simulator capture and follow-up diagnostics are recorded below.

## Verification evidence — 2026-11-07

- Local `scripts/phase21-validation.sh` archive on commit `dff70d0968332e9fa78f4a4c80af744d00ab429a`: **37 PASS, 0 FAIL, 0 BLOCKED**, covering Go 1.25.0 and Go 1.27.0. Coverage was 84.4% / 84.3%; `golangci-lint` reported 0 issues; untracked `.bench/` was preserved.
- Follow-up DLR correctness fix `ce14ae06a3d9096937c5c54a99a93aa8f695ece7` prevents response-only TLVs from identifying a `deliver_sm` receipt.
- OBS-01 source/test changes are recorded in commits `82c06bddafc482c6e336c10c84f5d904d12c50c3`, `b3ed89c58b28c38d0ce7e0d43b1bf89a0c8a0a59`, and `5e3f4678b1516c5eb4c5f84fd0af48d44ffe49a5`.
- Final source/test-head GitHub Actions run [42371052716](https://github.com/majiddarvishan/go-smpp/actions/runs/42371052716) passed both Go 1.25.x and Go 1.27.x: gofmt, unit, race, fuzz smoke, protocol/codec benchmarks, Phase 17 performance/resource smoke and no-unsafe; Go 1.27.x also passed lint and coverage.
- The final Phase 21 documentation/status PR head `64af94935c5c07af1c7995813c3b1a228c0c244d` passed both Go jobs in [run 42371554356](https://github.com/majiddarvishan/go-smpp/actions/runs/42371554356).
- INT-01 fixture framework commit `9fc5ed44830365f8c9b24f5e1c2f3a4c9c261b40` passed both Go jobs in [run 42375378458](https://github.com/majiddarvishan/go-smpp/actions/runs/42375378458): gofmt, unit, race, fuzz smoke, protocol/codec benchmarks, Phase 17 development performance/resource smoke and no-unsafe; Go 1.27.x also passed lint and coverage.
- The Phase 17 CI result is a **2-second smoke**, not the 60-second reference acceptance.

## Local simulator interoperability review — 2026-11-07

- Reviewed uploaded sanitized archive `go-smpp-interop.qgB9QT.tar.gz`. The archive contains only `report.json`, `manifest-candidate.json`, and `run.log`; no raw PCAP or unsanitized frame dump.
- SMSC simulator (`peer-001`): TCP, TRX bind, enquire_link, submit_sm with registered_delivery `0x03`, submit_sm_resp and valid `DELIVRD` receipt all passed. Unbind request was observed but no matching unbind_resp was captured before the 8-second operation deadline.
- ESME simulator (`peer-002`): TRX bind, enquire_link, synthetic deliver_sm and deliver_sm_resp passed. ESME-originated submit_sm was NOT_OBSERVED in the original 5-second window. Unbind request was observed but no matching unbind_resp was captured before the operation deadline.
- Both peers' missing unbind_resp is an observed simulator compatibility gap. SMPP 3.4 permits either role to issue Unbind and requires a matching unbind_resp; do not change library behavior to hide this.
- Reviewed and added two sanitized simulator fixtures: `simulator-smsc-peer-001` and `simulator-esme-peer-002`. They are simulator-only evidence and do not close deployed-peer compatibility.
- Probe follow-up increases the default ESME submit observation window to 15 seconds (override `SMPP_INTEROP_SUBMIT_WAIT`), prints a just-in-time trigger prompt, and reports whether Unbind request/response frames were observed plus safe response-timeout metadata in both client and server roles. Final code commit `ca5dfad5df1afdd9d80f8d742634149b59a6fd7e` passed Go 1.25.x/1.27.x in [run 42386492884](https://github.com/majiddarvishan/go-smpp/actions/runs/42386492884).
- Next live check: run `scripts/interop-local.sh esme` with the ESME simulator; trigger one outbound submit_sm during the new 15-second prompt window if supported. A repeat of Unbind timeout remains a peer finding, not a go-smpp library fix.

## Remaining P1 / release gates

- **INT-01G:** Obtain anonymized deployed-peer exchanges and build the representative-peer strict/compatible behavior matrix. The local simulator fixtures are reviewed and committed; deployed-peer evidence remains open.
- **INT-01I:** Re-run the ESME phase with an explicit outbound `submit_sm` action during the 15-second observation window if the simulator supports it; keep missing `unbind_resp` as a simulator compatibility finding.
- **PERF-01:** Re-run the 60-second Phase 17 reference acceptance on the combined current HEAD after post-acceptance library additions.
- **REL-01:** First production-ready version tag after the reference regression and release checklist pass.

Do not treat the 2-second CI smoke as the 60-second performance acceptance. Keep PR #8 Draft pending maintainer review; do not merge or tag automatically.


## ESME CLI-driven interoperability follow-up — 2026-11-07

- User clarified that the external ESME simulator is controlled through its CLI and never originates `submit_sm` without an explicit operator command. The correct test order is: probe `READY` → start/restart ESME → complete TRX bind, enquire_link and synthetic deliver_sm/deliver_sm_resp → wait for `OBSERVE` → issue exactly one CLI `submit_sm` within the configured window. The probe itself does not generate this ESME-originated message.
- Reviewed uploaded sanitized archive `go-smpp-interop.MLqt5S.tar.gz`: run duration ~0.55s; `listen=PASS`, `bind_transceiver=FAIL` with generic `*errors.errorString`, zero sanitized frames, one excluded/dropped frame, no fixture candidate. The single excluded frame is consistent with the privacy rule that discards bind credential PDUs. No `OBSERVE` stage was reached, so no submit command was due.
- The old bind gate received an authenticator-accepted TRX bind attempt, then its 10ms `BindMode()` polling observed session closure before seeing `Bound_TRX`. The old helper discarded `Session.Err()`; the archive cannot establish whether the peer sent EOF/reset or whether a successful bind response was briefly written and the peer immediately closed. Do not infer credential mismatch from this report.
- Test-only probe follow-up on the existing feature branch:
  - Observe `EventResponseSent` for `bind_transceiver_resp` (emitted after the response write) instead of polling a transient state; prefer an already-observed response if session closure races with event consumption.
  - Preserve a safe session-close cause and classify EOF, reset, local close and fatal protocol errors without emitting raw PDUs, passwords, bind fields or caller-controlled error text.
  - Clarify `READY` and `OBSERVE` prompts for the manual CLI workflow.
  - Add regression tests for a written bind response followed by immediate session closure and for peer EOF.
- Commits: probe observer/cause fix `2fc35e3f73a704c3795a480ee604b885740d1bce`; regression tests `07e4ae7313af005f42efb9ad2eba1edb96fa74c8`; CLI docs `6652ea0f970ba1bba7b236519d700c09c3b6e402`; task registration `95aa8081dc88feae61fafd229145757a49de1ab1`; plan evidence `0a4d5bc06677fa9ed272465feb4e410fa8076a15`; final READY wording `efc705df50c2c8d9e70b6d8daf7572f6bb509a91`.
- Validation: pending GitHub Actions for the updated PR head. PR #8 remains Draft/open; no merge, release or tag.


### Final validation — 2026-11-07

- Formatting follow-up commits: `efc705df50c2c8d9e70b6d8daf7572f6bb509a91` clarified READY text; `900fc57ea54395c1e64264a840b1ac7c52dbd1d4` joined RX/TX loops before resolving the bind response/close race; `0cf19df7fe3dd145a07b839745a3be5bd4870358` corrected gofmt in the regression test.
- GitHub Actions [run 42393189702](https://github.com/majiddarvishan/go-smpp/actions/runs/42393189702) passed both jobs: Go 1.25.x and Go 1.27.x. Passed gofmt, unit tests, opt-in interop tests, race tests, fuzz smoke, protocol/codec benchmarks, Phase 17 performance smoke, sustained resource-bound smoke, and no-unsafe; Go 1.27.x also passed lint and coverage.
- The hosted CI 2-second resource smoke is not the dedicated 60-second reference acceptance. PERF-01 remains open.
- Final test-only bind-observation work is recorded as INT-01J complete. INT-01I remains open for a live CLI-driven ESME `submit_sm` exchange; INT-01G remains open for anonymized deployed-peer evidence.
- PR #8 remains Draft/open and unmerged. No release tag was created.


## ESME peer Unbind capability follow-up — 2026-11-08

- User confirmed the external ESME simulator does not support SMPP Unbind. In the latest live run, the probe successfully completed TRX bind, enquire_link, synthetic deliver_sm/deliver_sm_resp, and the operator-triggered ESME submit_sm; the sanitized candidate contains the matching submit_sm_resp. The only FAIL was the probe's server-initiated Unbind timeout (8s), which is expected for this peer and not evidence of a go-smpp library defect.
- Reviewed `go-smpp-interop.avJydg.tar.gz`: sanitized run log/report/manifest candidate only; no password or raw PDU file. The final candidate frame is go-smpp's `unbind` request with no ESME `unbind_resp`.
- Test-only probe change on the existing feature branch: ESME/server mode now skips server-initiated Unbind by default and emits `unbind=UNSUPPORTED` with `operation=not_attempted`, `request_sent=false`, and `response_observed=false`; the local session is then closed. The event is informational and does not change the core phase result. Strict exchange remains available via `SMPP_INTEROP_ATTEMPT_ESME_UNBIND=true` or `-attempt-esme-unbind=true` for a peer known to support Unbind. The go-smpp library's Unbind semantics are unchanged.
- Added regression tests proving the default skip does not invoke Unbind, explicit opt-in invokes it and can pass, and strict timeout remains FAIL. Updated docs and task/plan tracking. Full CI passed on Go 1.25.x and Go 1.27.x at source head `4657d5e6c239c3abfd54fcf13e5804cb1310a018` in [run 42474275074](https://github.com/majiddarvishan/go-smpp/actions/runs/42474275074).
- Remaining: run CI for the new probe/config/tests; then ask for one repeat ESME-mode run to verify the default output reports core PASS plus `unbind UNSUPPORTED` without an 8-second wait. SMSC-mode external Unbind behavior remains separately observed/strict; INT-01G, PERF-01 and REL-01 remain open. PR #8 remains Draft/open.


### Validation and task closure — 2026-11-08

- An initial CI attempt exposed a test assertion that expected `peer_response=not_observed` when no packet capture was supplied. The production probe always supplies capture; the assertion was narrowed to verify the strict Unbind timeout's stable metadata (`timeout_kind=response`, command ID `0x00000006`). Existing capture-specific tests continue to verify request/response observation details.
- Final source head `4657d5e6c239c3abfd54fcf13e5804cb1310a018` passed [GitHub Actions run 42474275074](https://github.com/majiddarvishan/go-smpp/actions/runs/42474275074) on both Go 1.25.x and Go 1.27.x. Passed gofmt, lint (Go 1.27.x), unit tests, opt-in interoperability probe tests, coverage floor, race tests, fuzz smoke, protocol/codec benchmarks, Phase 17 performance smoke, sustained resource-bound smoke, and no-unsafe.
- INT-01I (live CLI-triggered submit/submit response and peer capability characterization) and INT-01K (default skip + strict opt-in for unsupported ESME Unbind) are complete. INT-01G deployed-peer evidence, PERF-01 dedicated 60-second reference acceptance, and REL-01 release remain open.
- PR #8 remains Draft/open and unmerged. No release tag was created.


## ESME session continuity and premature loss investigation — 2026-11-08

- Reviewed uploaded `go-smpp-interop.g7u649.tar.gz`. The run completed TRX bind, then `enquire_link` failed with `*session.LossError` caused by `*net.OpError` with operation `write`. `deliver_sm` returned `server.ErrUnknownSession`, meaning the original `attempt.sess` was no longer in the server's active-session map when DeliverSM was attempted.
- The ESME `submit_sm` and matching `submit_sm_resp` were captured later, but the old `submitHandler` discarded its `*session.Session` argument. It could therefore report a Submit from a replacement TCP session as PASS for the original bind. The report/candidate also disagreed on frame count (1 vs 2): `result.finish(packetCapture)` ran before deferred `srv.Close()`, while the shared tracer could still receive frames from a replacement session. Do not treat this run as proof of connection continuity.
- User confirmed their ESME simulator does not intentionally close the connection. The archive alone does not identify the underlying write errno, so the close initiator is not yet proven. The new safe close-cause classification distinguishes broken pipe, reset, aborted connection, timeout and local closed-connection errors without printing raw error strings, credentials or PDU bytes.
- Probe-only changes on the existing feature branch:
  - Retain the session pointer in every Submit observation; PASS only when it matches the session that completed the first TRX bind. A replacement-session Submit is FAIL with `submit_received_on_replacement_session`, and no mixed-session fixture is emitted.
  - Add `session_health_after_enquire_link` with session state, closed flag, server ownership, active-session count and sanitized terminal-cause metadata.
  - If the original session is already closed, report DeliverSM as BLOCKED rather than issuing it against a stale session.
  - Explicitly close the server and join active sessions before the final report/candidate snapshot; apply the same cleanup to early-return paths.
- Added tests for same-session/replacement-session classification, peer EOF cause retention, and safe broken-pipe classification. Updated interoperability docs, PLAN and TASKS. INT-01L remains open until final CI passes and one repeat live ESME run verifies the diagnostic output. No public library semantics have been changed.
- INT-01I and INT-01K remain complete based on the earlier valid run and Unbind capability decision. INT-01G deployed-peer evidence, PERF-01 reference acceptance and REL-01 release remain open. PR #8 stays Draft/open and unmerged.


### INT-01L CI completion — 2026-11-08

- Final source head `4deb2e1a13497b42de721fb74318a503c7ea92b5` passed [GitHub Actions run 42476774013](https://github.com/majiddarvishan/go-smpp/actions/runs/42476774013) on Go 1.25.x and Go 1.27.x.
- Both versions passed gofmt, unit tests, opt-in interoperability probe tests, race tests, fuzz smoke, protocol/codec benchmarks, Phase 17 performance smoke, sustained resource-bound smoke, and no-unsafe. Go 1.27.x lint and coverage floor also passed.
- INT-01L now needs only one repeat live ESME-mode run to establish whether the original connection is locally closed or encounters a peer/OS write-side error. No public library behavior has been changed.


## WgVM1N TCP reset investigation and per-socket tracing — 2026-11-08

- Reviewed exact uploaded archive `go-smpp-interop.WgVM1N.tar.gz` (note the capital `N`). It contains `run.log`, `report.json`, and an empty `manifest-candidate.json`; no PCAP, TCP flags, or raw PDU bytes.
- Report timing: `11:45:46.763Z` to `11:45:50.167Z` (~3.4s). TRX bind passed; the next `enquire_link` failed with `*session.LossError` caused by a read-side `*net.OpError` / `ECONNRESET`. Session health showed Closed and removed from the server; DeliverSM was correctly blocked; a later Submit was received on a replacement Session and rejected as a same-session success. Candidate fixtures were empty.
- `ECONNRESET` on read means the local TCP stack reported a reset while reading. The library then closes the socket as part of Session termination; the old report alone cannot attribute the initiating reset to the peer application versus its OS/network path. The prior label `peer_reset_connection` overstated attribution and is now `tcp_reset_received`.
- Added build-tagged, test-only per-socket tracing to the ESME/server probe. Each accepted connection receives an ID; trace events include only complete SMPP header metadata (direction, command ID/status, sequence, length), read/write error classes, and local Close ordering/prior I/O error. No PDU body, credentials, addresses, raw packet bytes, or PCAP are persisted.
- Bind response detection is now correlated to the exact accepted socket through the connection wrapper rather than a shared Session Observer channel. Submit observations carry their connection ID and still require the original Session pointer. This should expose whether the first socket receives a reset before go-smpp calls Close, or whether local Close occurs first, and show the exact PDU immediately preceding the reset.
- Added regression test for fragmented bind_transceiver_resp header parsing, per-connection response correlation, and read-reset-before-local-close ordering; added safe reset-label test. Probe-only; no public API or library Session/Transport semantics changed.
- CI for source head `6c5b6075502109489f07f8b94140ced33a011283` was queued/in progress at the time of this note; final CI result must be recorded after completion. Next live step after CI: rerun the same ESME command and inspect the ordered `tcp_*` events. If the trace shows `tcp_read_error(connection_reset)` before `tcp_local_close`, go-smpp closed only after receiving the reset; if `tcp_local_close` comes first, investigate local shutdown path. A TCP packet capture would still be needed to attribute a reset beyond the remote TCP endpoint.
- INT-01L remains open until this live trace is reviewed. INT-01G deployed-peer evidence, PERF-01 reference acceptance and REL-01 release remain open. PR #8 stays Draft/open; no merge/tag/release.


### TCP trace validation and accept-loop join — 2026-11-08

- Full CI passed at source head `2a53a445a92e67264b02058131136c176b6e477e` in [run 42478439742](https://github.com/majiddarvishan/go-smpp/actions/runs/42478439742): Go 1.25.x and Go 1.27.x both passed; Go 1.27 lint/coverage also passed. This included the per-socket tracer, reset classification, and regression tests for successful-write bind correlation and read-reset-before-local-close ordering.
- A final cleanup commit `29c6d813adcbfc743cde10fafc74b340aea6b50b` makes the probe wait for `Server.Serve`'s accept loop to exit after listener/session shutdown and before the final TCP trace snapshot. This prevents a just-accepted replacement socket from being omitted during the final snapshot race.
- CI for `29c6d813adcbfc743cde10fafc74b340aea6b50b`: Go 1.25.x full matrix passed; Go 1.27.x was still running its Lint step at the time of this note. Verify the final run result before claiming full matrix pass.
- Next live action remains one ESME rerun using the same command. Inspect `tcp_accepted`, `tcp_pdu_written`, `tcp_pdu_read`, `tcp_read_error`, `tcp_write_error`, and `tcp_local_close` events ordered by `order` and grouped by `connection_id`. INT-01L remains open until the live trace establishes the local-close/reset ordering.
