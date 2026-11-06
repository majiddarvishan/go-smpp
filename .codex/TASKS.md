# Active Library Tasks

Scope: `github.com/majiddarvishan/go-smpp` only. Simulator `majiddarvishan/smpp_sim` is design-frozen and MUST NOT be implemented or modified during this workstream.

## P0 — Receipt parsing for SMPP Test Studio (authorized 2026-11-07)

Work branch: `feature/dlr-receipt-parser-v1`, based on `b8eaa68ffda24836aa38bef913f26bcbe2a454ef`.
Progress source of truth: `PLAN.md` Phase 20; mirror status here only after verification.

- [x] **DLR-01 Contract:** Add optional `message.ParseDeliveryReceipt(protocol.DeliverSM)`, independent from the low-level codec, with exact opaque ID preservation, no global normalization and no effect on `deliver_sm_resp` ACK selection.
- [x] **DLR-02 Precedence:** Text `id:` takes precedence over `receipted_message_id` TLV for MessageID; `message_state` TLV takes precedence over textual `stat:` for state. Preserve both original sources and report conflicts without hiding them.
- [x] **DLR-03 Safe parsing:** Handle absent fields, case/leading-zero-sensitive identifiers, malformed state TLV, invalid/duplicate conflicting TLVs, optional C-Octet NUL terminator, text fields, and ordinary `deliver_sm` that is not a receipt without guessing or panic.
- [x] **DLR-04 Fixtures:** Add table-driven tests for common SMPP 3.4 receipt samples, mixed/contradictory Text+TLVs, escaped/unknown text status, duplicate tags and malformed input; do not modify the Session's stream decoder or assume a vendor-specific ID normalization.
- [x] **DLR-05 API safety:** Update exported API snapshot and no-panic coverage; keep changes additive, Go 1.25-compatible, stdlib-only and without per-PDU goroutines.
- [x] **DLR-06 Documentation:** Describe the returned semantics, unsupported/vendor variants, error behavior and how the simulator may consume it; update `.codex/SESSION.md`, `.codex/BACKLOG.md` and `PLAN.md` on completion.
- [ ] **DLR-07 Verification:** Pass `gofmt`, unit, full Go 1.25/1.27 CI, `go test -race ./...` and API compatibility checks, then review/merge on GitHub. Record actual CI evidence; no independent `smpp_sim` 50k TPS claims.

**Implementation verification:** [PR #7](https://github.com/majiddarvishan/go-smpp/pull/7), [CI run 42305732414](https://github.com/majiddarvishan/go-smpp/actions/runs/42305732414) passed Go 1.25/1.27 matrices (format, unit, API/no-panic, lint, race, coverage/fuzz/performance smoke). DLR-07 stays open until merge is completed and confirmed.

## P1 — Follow-up library reliability and interoperability

- [ ] **INT-01:** Build optional vendor receipt fixtures (alternative message ID encodings, TLV-only DLR, text-only DLR, unknown status) without silently changing the baseline parser.
- [ ] **OBS-01:** Review opt-in PacketTracer raw credential/message exposure and propose a backwards-compatible safe header-only option if needed.
- [ ] **PERF-01:** Run a measured combined-HEAD reference throughput regression; do not infer the formal 100k reference or simulator 50k benchmark from CI smoke.
- [ ] **REL-01:** Review version tagging only after release gates and reference runner results.

### Rules of execution

Register scope before implementing. Changes belong in the library, on an isolated PR. Maintain `PLAN.md` as the authoritative progress log; only mark tasks complete when measured evidence supports completion. No auto-resubmission or behavior changes to the frozen simulator design.
