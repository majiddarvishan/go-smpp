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

## Verification still required

No local Go execution has been performed for the Phase 21 branch edits yet. Do not mark Phase 21 tasks complete until the PR CI passes on both supported Go lines, including gofmt, lint, unit, API/no-panic, race, coverage, fuzz and performance smoke. Keep P1 real-peer interoperability, current-head reference performance and release-tag gates open until their own evidence exists.
