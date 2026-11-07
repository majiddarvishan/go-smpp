# Session Handoff

## Current state

Repository: `github.com/majiddarvishan/go-smpp`
Current branch: `feature/product-neutral-library-dlr-completeness`
Base: `main` at `8effc6317d8141e36976b0a21bdbe190e2761116`

This is a **general-purpose, product-neutral SMPP library**. No downstream product or consumer-specific requirement may be imported into the source, public API, docs, examples or maintained handoff files. See D-046, `AGENTS.md`, and `.codex/ARCHITECTURE.md`.

## Implemented baseline

- SMPP 3.4 command and standard TLV coverage, with a shared negotiated SMPP 5.0 extension core.
- ESME/client and SMSC/server roles share the same session engine.
- TCP and optional TLS-over-TCP transport.
- Bounded pipelining, request correlation/window, shared response/liveness deadlines and reconnect without ambiguous request replay.
- GSM 03.38/GSM7, strict UCS-2, UTF-16BE surrogate support, binary, UDH and SAR helpers.
- Generic optional `message.ParseDeliveryReceipt` API.
- Generic standalone `cmd/smpp-sim` SMSC peer for local development/performance measurement; not a public package or dependency.
- Go 1.25 minimum; CI matrix Go 1.25.x and 1.27.x; zero third-party runtime dependencies.

## Performance and release status

Phase 17 reference acceptance passed on the documented reference host: **713,932 aggregate bidirectional request PDUs/s for 60 seconds, minimum passing session count 1**. Required responses were enabled. Exact host/environment and the historical local-script caveat are in `docs/PERFORMANCE.md`.

The measured result applies to the recorded reference commit, not automatically to the combined current HEAD after later library changes. A fresh current-head reference run remains a P1 release regression gate. Phase 19 remains open for the first production-ready tag.

## Merged generic library improvements

- PR #3 — bounded opt-in suppressed/deferred inbound response handling; Session-scoped one-shot reply ownership. Commit `1387d9bddda63464d5f464d4f5d6c0acbf6f17c8`.
- PR #4 — GSM7 extension ESC-pair-safe UDH/SAR multipart splitting. Commit `f8b4805eb3c10c046e1846f804fb5790efb3e8b6`.
- PR #6 — optional reconnect jitter and stop on explicit permanent bind rejection; legacy defaults unchanged. Commit `eadea5e2b8f96bf8552cba9d0f7c65b231ae5680`.
- PR #5 — additive UDH16/unpacked-GSM7 and binary segmentation helpers. Commit `bff9b61c5f1a923e5d02e8fc1e613c460db4f476`.
- PR #7 — generic DLR parser with text-first ID, TLV-first standard state, conflict diagnostics and API/no-panic tests. Merge commit `e4606a40961d62cf8eb917f28120385282e89b29`; final PR-head CI run [42306339442](https://github.com/majiddarvishan/go-smpp/actions/runs/42306339442) passed Go 1.25/1.27.

## Phase 21 — active branch work

Tasks were registered before implementation in `.codex/TASKS.md` and `PLAN.md`.

- Remove consumer-specific documentation and history; retain only generic protocol facts and generic development-tool descriptions.
- Extend the DLR parser to preserve standard text fields `sub`, `dlvrd`, dates, `err`, and `text`.
- Parse standard `network_error_code`, `delivery_failure_reason`, and `additional_status_info_text` TLVs.
- Support IA5 and Latin-1 receipt text, and report unsupported data coding explicitly without guessing packed GSM7.
- Add typed `protocol.MessageState` accessor without changing existing exported field types.
- Add lossless SMPP 3.4 `registered_delivery` bit-field helpers that preserve reserved values.
- Reconcile stale backlog and phase status against implemented source/tests and published performance evidence.
- Run full Go 1.25.x/1.27.x CI and merge only after final-head checks pass.

### Latest corrective patch — 2026-11-07

- Commit `97edd84c90ae7828def9e3c186dba1d270862d5b` fixes the `registered_delivery` field-extraction vector (`0x1f), with reserved-bit cases retained), applies gofmt-equivalent alignment fixes to the four files identified by the validation report, restores the CI workflow from the `main` baseline and adds a dedicated DLR fuzz smoke.
- The Phase 21 validation script now installs the pinned `golangci-lint v2.14.0` with the locally available Go 1.27.0 (`GOTOOLCHAIN=local`), uses a valid temporary tools directory, and does not disable checksum verification. If Go 1.25.0 cannot be securely downloaded because `GOSUMDB` is disabled or unset, the gate is reported as BLOCKED with remediation.
- The duplicate decision identifier was corrected: the performance-laboratory decision is now D-048; the original SMPP one-way decision remains D-042.
- These are source/documentation repairs, **not validation evidence**. No local Go test or CI run is recorded for this commit; all Phase 21 tasks remain open. `main` is unchanged and no PR has been opened.

### Latest local validation evidence — 2026-11-07

- The user ran `scripts/phase21-validation.sh` on branch HEAD `e60258af93dd67efe8b3984e6b57ad5556e6dd4d`.
- Result: **35 PASS, 2 FAIL, 0 BLOCKED**. The only failures were `go1.25.0-gofmt` and `go1.27.0-gofmt`; both identify only `message/delivery_receipt.go` and `protocol/registered_delivery.go`.
- Both Go versions passed unit, API snapshot, no-panic, race, all fuzz smoke gates, protocol/codec/session/perflab benchmark smoke, no-unsafe, lint, and coverage-floor gates.
- Phase 17 was a 2-second CI smoke only, not the 60-second reference acceptance.
- Git status showed an untracked `.bench/` directory. The validation script explicitly did not read, delete, or modify it; preserve it.
- Commit `de73b0f54580ff7fd1df0243fa924afc14e1ace6` applies the formatting-only gofmt alignment correction to all `DeliveryReceipt` fields after the DLR expansion, and to the `RegisteredDelivery` struct/composite literal. Rerun the full validation script before marking any Phase 21 task complete.

### Latest final-head PR CI — 2026-11-07

- The remaining gofmt failure was isolated to blank-line-separated field groups in `message/delivery_receipt.go`. Commit `73bbef53b190fdb6d4bfcdd23929ac3106ee39ee` changes whitespace only and aligns each group independently, matching gofmt.
- Draft PR #8: https://github.com/majiddarvishan/go-smpp/pull/8
- Final-head GitHub Actions run `42363925646` passed on commit `73bbef53b190fdb6d4bfcdd23929ac3106ee39ee` for both Go 1.25.x and Go 1.27.x.
- Both matrix jobs passed gofmt, unit tests, race tests, fuzz smoke, protocol/codec benchmarks, Phase 17 performance smoke, the 2-second sustained resource-bound smoke, and no-unsafe. The Go 1.27.x job also passed golangci-lint and coverage floor.
- The Phase 17 result remains a 2-second CI smoke, not the 60-second reference acceptance.
- The complete local `scripts/phase21-validation.sh` rerun is still required against the latest branch head. Keep Phase 21 task checkboxes open and PR #8 in draft until that evidence and final review are recorded.

## Verification still required

The last full local `scripts/phase21-validation.sh` run was on commit `47f6e28cec64e8e76c3efadd0ac8b783e7c888c9` and reported **35 PASS, 2 FAIL, 0 BLOCKED**. Both failures were the Go 1.25/1.27 gofmt gates, both identifying only `message/delivery_receipt.go`; `protocol/registered_delivery.go` was no longer reported after commit `de73b0f54580ff7fd1df0243fa924afc14e1ace6`. Commit `73bbef53b190fdb6d4bfcdd23929ac3106ee39ee` corrected the remaining blank-line-separated struct field alignment without changing behavior.

GitHub PR CI run `42363925646` passed both Go 1.25.x and Go 1.27.x on source commit `73bbef53b190fdb6d4bfcdd23929ac3106ee39ee`. The subsequent session-record commit `f501371bb0d9e7f8d816b27863d53a7b82182036` is now the branch head and needs its own final-head PR CI confirmation. The full local validation script also still needs to be rerun on the latest branch head before marking Phase 21 tasks complete. Keep P1 real-peer interoperability, the 60-second current-head reference performance acceptance, and release-tag gates open until their own evidence exists. Preserve the untracked `.bench/` directory.
