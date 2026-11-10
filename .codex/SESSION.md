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


## ESME EOF before Bind Response — 2026-11-08

- Reviewed the uploaded archive `go-smpp-interop.wvS2vr.tar.gz`. It contains only `run.log`, `report.json`, and an empty candidate manifest; there is no PCAP or raw TCP capture.
- At `12:11:05.329418Z`, connection 1 received a complete 45-byte `bind_transceiver` request (`0x00000009`, sequence `1006632970`). The configured authenticator accepted the TRX bind. At `12:11:05.329696Z`, the next read returned EOF / `peer_closed_connection`; at `12:11:05.329717Z`, go-smpp closed the socket with `prior_io_error=read:peer_closed_connection`. There was no successful `tcp_pdu_written` Bind Response event. The ESME side closed its TCP stream before the bind response was observed; this is not evidence of a go-smpp-initiated close.
- Connection 2 was accepted after connection 1's EOF and closed about 38 microseconds later. This likely raced the probe's early-return cleanup, but the old trace did not label the close origin. New probe metadata marks `probe_shutdown_started=true` on local close events that occur after the runner begins shutdown.
- The later connection-1 `tcp_write_error` reported `local_connection_closed` after the recorded local Close. It is secondary. The probe now extracts only the attempted PDU header from a failed write (when available), so the next run can confirm whether the queued write was `bind_transceiver_resp`; no payload/body bytes are retained.
- The current probe uses distinct decimal-only IDs: `12345678901234567890` in the application-supplied `submit_sm_resp` and `9876543210987654321` in the unrelated synthetic DLR. SMPP still encodes each as a NUL-terminated C-Octet String. No global numeric restriction is added: `server.SubmitResult.MessageID` is supplied by the application, and DLR IDs from external peers remain opaque.
- Added regression tests for failed-write attempted-header metadata, close-during-probe-shutdown marking, and numeric-only submit/DLR ID consistency. Source/test head `3cb8ff9f59ba392b7fde2f54b9ee941c8b04505e` passed the full Go 1.25.x/1.27.x CI matrix in [run 42481521309](https://github.com/majiddarvishan/go-smpp/actions/runs/42481521309), including gofmt, lint, unit, opt-in interop, coverage, race, fuzz, benchmarks, performance/resource smoke and no-unsafe. Latest documentation-only head `4f35c3e59434f398a0d505264a96432f4e1c49d3` is running CI.
- Next: verify final CI; rerun `SMPP_INTEROP_SUBMIT_WAIT=30s scripts/interop-local.sh esme`; capture the ESME simulator's local log around bind. If needed, run a FIN/RST-only loopback capture on the same host (never capture payload or credentials). INT-01L remains open. PR #8 remains Draft/open, unmerged.


## Successful ESME/server repeat — 2026-11-08

- Reviewed `go-smpp-interop.2KhVNK.tar.gz`. It contains a complete SMPP 3.4 exchange on one TCP connection: TRX Bind/BindResp, EnquireLink/EnquireLinkResp, DeliverSM/DeliverSMResp, and ESME-originated SubmitSM/SubmitSMResp. All PDU trace events use `connection_id=1`; the Submit event confirms `session_match=true`. The candidate excludes both Bind frames and retains the six safe non-bind frames.
- Correct command IDs: `0x00000004` is `submit_sm`; `0x80000004` is `submit_sm_resp`. The last trace pair is Submit, not Unbind. No Unbind PDU/response is present in this archive.
- The `unbind=UNSUPPORTED` event is the separate server-initiated Unbind test (SMSC → ESME). The known ESME simulator does not support this operation, so it is skipped; the runner then closes its local session. Report metadata now makes the initiator, peer role, operation kind and request direction explicit. The library's Unbind implementation is unchanged.
- Reviewed and added a separate manifest fixture `simulator-esme-peer-002-submit`, evidence `INT-01-PEER-002-002`, with six sanitized frames. The previous peer-002 fixture remains unchanged because it records a different run and a prior strict Unbind attempt.
- The loopback capture's local time is UTC+03:30. FIN from `127.0.0.1:4000` to client port `46662` at `16:43:51.816897`, followed by peer FIN at `16:43:51.816946`, aligns with the report's local Close at `13:13:51.816872Z`. The active session shut down gracefully; it did not RST. Other RST/ACK packets target different ephemeral ports and occurred before listener startup, immediately after shutdown, or after probe exit. This is consistent with refused connection attempts while no listener is accepting; because SYNs were excluded by the tcpdump filter and trace omits remote ports, the initiating local process cannot be identified.
- The earlier `wvS2vr` peer EOF before Bind Response was not reproduced. That trace showed the ESME peer closed first and go-smpp closed only after EOF; the later write error was secondary. The successful repeat demonstrates the probe and peer can complete the handshake and message exchange. No public Session/Transport change is justified.
- Probe report metadata now clarifies Unbind direction; tests cover the new fields. CI has a small diagnostic improvement to print `gofmt -d` when formatting fails. Latest branch head `dea096d71e6acd31fb192a050d90fad1124a1a74` passed the full Go 1.25.x/1.27.x CI matrix in [run 42485670483](https://github.com/majiddarvishan/go-smpp/actions/runs/42485670483). INT-01L is complete; deployed-peer evidence and the reference-host performance gate remain open. PR #8 remains Draft/open and unmerged.


### INT-01L reopened — intermittent ESME bind EOF (2026-11-08, archive nmFaln)

- Reviewed `go-smpp-interop.nmFaln.tar.gz`; it contains only `run.log`, `report.json`, and an empty `manifest-candidate.json`. No PCAP, ESME-side log, or sanitized PDU fixture was supplied.
- Report window: `13:41:13.325624830Z`–`13:41:15.972089996Z`. On `connection_id=1`, TCP was accepted at `13:41:15.970402535Z`; the 45-byte `bind_transceiver` (seq `276824068`) was read at `13:41:15.971049832Z`; peer EOF was observed at `13:41:15.971342852Z`, 293 µs after the Bind read. The authenticator result was `accepted`.
- No `bind_transceiver_resp` was written. The session closed after the read-side peer EOF; the later write attempt for `0x80000009` failed with `local_connection_closed`. This confirms the local close/write error is downstream of the peer EOF, not the initiating event.
- `connection_id=2` was accepted at `13:41:15.971848340Z` and closed at `13:41:15.971857921Z` while probe shutdown was already marked. No PDU was read on it; the report does not establish which local process initiated that connection.
- This reproduces the earlier `wvS2vr` failure after the successful same-pair run `2KhVNK`. Therefore the event is intermittent; do not mark INT-01L complete. The current evidence does not justify a public Session/Transport behavior change.
- Next required evidence: ESME simulator console/diagnostic log for this exact bind attempt. If the simulator cannot provide it, capture only FIN/RST packet summaries on loopback (no payload, no PCAP containing SMPP data or credentials). Keep PR #8 Draft/open; no merge or release.


### Follow-up FIN/RST-only capture — 2026-11-08 (18:01 local)

- The supplied capture was filtered to FIN/RST only and includes no SYN or ordinary ACK packets; no simultaneous probe `report.json` or ESME console log was provided. Therefore ephemeral ports cannot be mapped to probe `connection_id` values or to a specific Bind attempt.
- At `18:01:40.274839`, client-side `127.0.0.1:46680` sent FIN to server `127.0.0.1:4000`. At `18:01:40.275994`, the server sent FIN back to `46680`. This tuple is consistent with a graceful close initiated by the client side.
- A distinct tuple `4000 → 46686` shows server FIN at `18:01:40.275480` followed by server RST at `18:01:40.275770`; without the earlier handshake and ACK sequence, the reason is undetermined.
- Other server-originated RSTs target different ephemeral ports at `18:01:31`, `18:01:34`, and `18:01:37`. They may be connection attempts/refusals or stale traffic; the present filter cannot establish which process initiated them.
- This capture supports that at least one client-side TCP endpoint initiated a graceful close, but does not yet prove that it is the same socket/session as the reported Bind EOF. No code change is justified.
- Next capture: include only TCP SYN/FIN/RST flags (no payload), and run it concurrently with one ESME probe while saving the sanitized probe report. Correlate timestamps and client ephemeral ports. Ask for ESME console diagnostics around the same attempt if available.


### Bind authentication failure and peer decoder compatibility (2026-11-10 review of 2026-11-08 evidence)

The `oagXgd` ESME interoperability run reported:

- `bind_transceiver` was received and rejected with `command_status=0x0000000e` (`StatusInvalidPassword`), reason `password_mismatch`; this is an actual authentication rejection, not a timeout or a missing BindResp.
- The server **successfully wrote** `bind_transceiver_resp` (`0x80000009`) with the same sequence `2915041288`, error status `0x0000000e`, and `command_length=16`. The separate ESME simulator printed `(MPP/SmppBindResp.cpp,191) - DECODE - BindResp, (packetPtr - packet.c_str()) != command_length`.
- The concurrent header-only loopback capture confirms the TCP connection to `127.0.0.1:4000` succeeded on client port `52560` at `18:10:49.334922` local, followed by a server FIN at `18:10:49.349865` and client FIN at `18:10:49.350057`. Earlier SYN→RST pairs occur when no listener responds to the connection attempt. The packet filter includes no SMPP payload.
- **Conformance:** SMPP 3.4 Issue 1.2 sections 4.1.2 and 4.1.4 explicitly omit the whole Bind Response body when `command_status` is nonzero. Section 4.1.6 defines the same response-family body shape for transceiver; unlike the two preceding sections it does not repeat that explicit note. The shared library follows the protocol's error-response body-omission convention: nonzero status produces a header-only 16-byte Bind Response; successful responses carry a NUL-terminated `system_id` (17+ bytes). Specification: https://smpp.org/SMPP_v3_4_Issue1_2.pdf.
- The ESME parser's offset-mismatch diagnostic is **consistent with** incorrectly requiring a `system_id` terminator in a failed BindResp, but the actual `SmppBindResp.cpp` source has not been inspected; treat this as a peer-decoder compatibility hypothesis, not a proven code defect.
- Do **not** append a nonstandard NUL to the library's failed Bind Responses or bypass/relax authentication to make this peer pass. Fix the local credential mismatch without sharing any secret. The ESME parser should handle a header-only nonzero-status BindResp rather than trying to parse successful-bind fields.
- Codec golden-wire regression tests now cover all three SMPP 3.4 BindResp variants, exact 16-byte invalid-password framing and success C-Octet String bodies; a separate real TCP/server test checks rejection and success framing through the authenticator. No public library behavior changed. CI and an operator retest with locally matched credentials remain to be completed.
- A prior unrelated CI run on `c7e2479` failed only at Go 1.25's `FuzzParseDeliveryReceipt` smoke with `context deadline exceeded`; unit and race tests passed there. The new focused test's initial CI also failed because its synthetic passwords exceeded the SMPP 3.4 field limit; corrected to within 8 octets in `e0ec9ff`. Do not mislabel these test-fixture mistakes as production defects.


### BindResp wire audit verification — 2026-11-10

- Final source head `f5dc82fdf1230c794229f016d248aca0790b3193` passed both Go 1.25.x and Go 1.27.x in [GitHub Actions run 42784198156](https://github.com/majiddarvishan/go-smpp/actions/runs/42784198156). Full matrix passed: gofmt, lint (Go 1.27), unit tests, opt-in interop tests, coverage floor, race tests, fuzz smoke, protocol/codec benchmarks, Phase 17 performance smoke, sustained resource-bound smoke, and no-unsafe.
- The first version of the server regression used synthetic passwords exceeding SMPP 3.4's field limit; they were corrected. It then assumed successful BindResp length was exactly system_id+header, but the real server legitimately returned sc_interface_version TLV, so the success assertion was corrected to accept the TLV while checking system_id. Full CI passed after both fixes.
- INT-01M's source audit and regression tests are complete; the peer's own BindResp decoder needs a local follow-up when the ESME sends credentials matching those supplied privately at the probe prompt. A failed bind still must be reported as an authentication error; the library must not silently pad its header-only error response.
- Earlier Go 1.25 fuzz smoke failure (`context deadline exceeded`) at the old `c7e2479` head did not recur in the fully passing final matrix; no production code or CI thresholds were changed on the basis of that isolated event. PR #8 remains Draft/open and unmerged.


### Successful repeat with an empty precursor TCP connection — 2026-11-10 (`CpcJZ3`)

- Reviewed user-provided sanitized archive `go-smpp-interop.CpcJZ3.tar.gz` (`report.json`, `run.log`, `manifest-candidate.json`); run window `08:49:48.101316757Z`–`08:49:59.057931413Z`. The verdict is **PASS**, `OVERALL failures=0 blocked=0 candidates=1`; six non-Bind frames were captured, two Bind frames excluded for privacy. There is no raw PDU/PCAP in the archive.
- TCP connection 1 was accepted at `08:49:49.880562930Z`, received **no SMPP PDU**, then returned `peer_closed_connection` at `08:49:49.948583312Z` (approximately 68 ms later); the library's local Close followed. The process/purpose of that short-lived connection is **not established**. It is distinct from earlier failures where the peer closed **after** sending Bind.
- TCP connection 2 was accepted at `08:49:49.949844124Z`; `bind_transceiver` (45 bytes, sequence `3745513482`) was read and `bind_transceiver_resp` (37 bytes, `command_status=0`, same sequence) was written. The session remained `Bound_TRX`, `server_active_sessions=1`, `server_owns_session=true`, and `session_match=true`.
- On connection 2, `enquire_link` and `enquire_link_resp` matched sequence `1`; `deliver_sm` and `deliver_sm_resp` matched sequence `2` with status zero; the manually issued ESME `submit_sm` and the library's `submit_sm_resp` matched sequence `3292528651` with response status zero. This also confirms round-trip preservation of the peer's high-bit inbound sequence numbers on both BindResp and SubmitSMResp.
- The probe's `unbind=UNSUPPORTED` means **SMSC-initiated** Unbind was intentionally not attempted for this ESME simulator. The completed SubmitSM response was followed by a local Close by the probe; no peer Unbind exchange was recorded.
- The candidate manifest's six sanitized frames were checked for declared length, command ID, status, and sequence consistency; all checks passed. The candidate has `sanitization.reviewed=false` and reuses ID `simulator-esme-peer-002` / evidence `INT-01-PEER-002-001`, which **already belongs to an existing reviewed fixture from another run**. Do not overwrite/re-import it. The existing `simulator-esme-peer-002-submit` fixture already covers this exact six-command exchange; this independent run is recorded as further evidence, not a redundant fixture.
- Compared with earlier `oagXgd`, a valid successful 37-byte BindResp shows that matching credentials make the normal exchange succeed. This does **not** prove how the peer decoder handles a nonzero-status, 16-byte BindResp; keep those questions separate. Earlier intermittent post-Bind EOF remains open as INT-01L, while the local ESME interoperability success path is verified. Do not modify public codec, Session, authentication or Transport on the basis of this passing run.
- PR #8 remains Draft/open. No merge, tag, or release. Reference-host 60-second performance acceptance and deployed-peer evidence are still pending.


### Pre-Bind TCP disconnect recovery regression — 2026-11-10

- Reviewed `go-smpp-interop.CpcJZ3.tar.gz`: first TCP connection closed after ~68 ms without a PDU; second connection completed Bind, EnquireLink, DeliverSM, ESME SubmitSM and responses with `session_match=true`. Report PASS/zero failures.
- The sanitized candidate has six structurally valid frames, excludes both credential-bearing Bind frames, and is `reviewed=false` under an already-used `simulator-esme-peer-002` fixture ID/evidence ref. Its semantic commands duplicate existing reviewed `simulator-esme-peer-002-submit`; do not import as a conflicting duplicate. High-bit Submit sequence `3292528651` and Bind sequence `3745513482` are preserved in responses.
- Added generic `server/prebind_peer_close_test.go`: server `MaxSessions=1`; five consecutive connections accepted then closed without any PDU, each slot reclaimed; a subsequent TRX Bind succeeds. No production code changed.
- Full Go 1.25.x/1.27.x CI for source commit `5a1d2500a362fc0d405d5aed8d02d2e2550001ea` passed in [run 42788172657](https://github.com/majiddarvishan/go-smpp/actions/runs/42788172657) attempt 2. Initial Go 1.25 fuzz smoke failed with `context deadline exceeded` on `FuzzFramerChunked` during a 2s fuzz run; unit/race passed. A rerun of only the failed job succeeded. No thresholds or fuzz algorithms were changed.
- INT-01N complete. INT-01L remains open because the earlier occasional **post-Bind** peer EOF is a distinct unresolved event. Local simulator interoperability is verified; deployed-peer evidence and 60-second reference-host performance acceptance remain outstanding. PR #8 stays Draft/open; no release/tag.


### Current candidate reference acceptance preflight — 2026-11-10

- The feature branch is ahead of `main` by 206 commits and behind by 0 at this checkpoint; performance can be verified on the candidate SHA **before** any PR merge. The reference result from 2026-10-06 (713,932 request PDUs/s) is historical and does not cover these library additions.
- `scripts/acceptance.sh --preflight` now checks Linux/amd64, Go 1.27.x, 8+ logical CPUs, 10+ GiB RAM, and clean Git working tree without creating files or running the workload. The full acceptance enforces the same checks, records `source_clean=true` and the exact commit, then runs the 60s bidirectional TCP workload with full responses, resource checks and profiles. `.bench/` is excluded from Git.
- CI validates `bash -n scripts/acceptance.sh`; the `SMPP reference performance acceptance` workflow will invoke preflight before the long run. This workflow requires a self-hosted `smpp-reference` runner. Connected GitHub actions cannot dispatch workflows or inspect runner inventory; there is no verified current-head reference result yet.
- Local operator command on the eligible reference host: `scripts/acceptance.sh --preflight` followed by `scripts/acceptance.sh .bench/phase17-reference`. Preserve `environment.txt`, `acceptance-sessions-N.txt`, `minimum-session-count.txt` and the profiling outputs. Do not claim release PASS from CI smoke or the old reference result.
- PERF-01 and REL-01 remain open. PR #8 remains Draft/open and unmerged. After integration, retest if the executable tree changes.

## Phase 17 current-candidate reference acceptance — 2026-11-10

- Operator ran `scripts/acceptance.sh --preflight` and `scripts/acceptance.sh .bench/phase17-reference` from a clean Linux/amd64 candidate at `ebf28340fbe7a49b9ab6a8a0a9a34d7ba924dee9` with go1.27.0, 12 logical CPUs, 32,649,508 KiB RAM, `GOMAXPROCS=8`, 128 callers, TX batch 32, 60s and 100,000 request-PDU/s target.
- Preflight **PASS**; race checks `session`, `client`, `server` **PASS**; Phase 17 sustained acceptance **PASS** with minimum passing session count **1**, 683,465 request PDUs/s, 41,008,141 timed completed, 41,008,269 requests/41,008,269 responses, max goroutines 136 from baseline 8, peak heap 3 MiB, retained heap growth 0 MiB, pending/window max 64. Separate diagnostic benchmark: 723,710 request PDUs/s, 1,382 ns/op, 896 B/op and 9 allocs/op.
- Runner reported writing local evidence to `.bench/phase17-reference`, but the artifact files/profiles have not been attached or independently reviewed. PERF-01 is closed from operator stdout; PERF-02 covers artifact audit before release. Historical 2026-10-06 evidence remains separate.
- No executable source changed during this documentation update. INT-01G (deployed-peer evidence) and INT-01L (intermittent post-Bind peer EOF diagnostics) remain open. REL-01 is NOT authorized yet; PR #8 stays Draft/Open, unmerged, without tag. Retest if future integration changes executable code.
