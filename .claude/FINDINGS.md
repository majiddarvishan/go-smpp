# Prioritized remediation backlog

Derived from `CODE_REVIEW.md`. Ordered by (impact × likelihood) ÷ effort. IDs match the review.

For the sequenced, per-phase implementation plan with acceptance criteria and `[x]` completion
tracking, see `TASKS.md`. This file is the prioritization view; `TASKS.md` is the execution view.

Legend — **S1** production incident risk · **S2** correctness/resource risk under load ·
**S3** latent/defensive · **S4** cosmetic.

---

## Tier 0 — blocks any public release

| ID | Finding | Location | Sev | Effort |
| --- | --- | --- | --- | --- |
| C6a | **No LICENSE file.** Default copyright applies; the library is legally unusable by anyone. | repo root | S1 | minutes |

Nothing else on this list matters until C6a is done.

---

## Tier 1 — correctness and availability

| ID | Finding | Location | Sev | Effort |
| --- | --- | --- | --- | --- |
| B1 | `requestWindow.release()` **panics**, taking down the host process on an internal accounting slip. | `session/window.go:83` | S1 | small |
| B2 | **No write deadline anywhere** in library code. A stalled peer blocks `txLoop` forever; response deadlines never arm because they are set after the write. | `transport/transport.go:76,98` + `txLoop` | S1 | medium |
| SEC1 | Same defect from the security side: unauthenticated slow-loris can exhaust `MaxSessions`. | `server/server.go` | S1 | (closed by B2 + B7) |
| B3 | `livenessLoop` calls `EnquireLink` synchronously; a saturated window parks the liveness goroutine, disabling all timeout supervision exactly when load is highest. | `session/session.go:740` | S2 | small |
| B5 | `ensureSCInterfaceVersion` `append`s into a caller-owned backing array — silent cross-contamination between responses. | `session/session.go:1308` | S2 | trivial |
| B4 | Completion-channel pool relies on an unenforced cross-file drain invariant; saves one tiny alloc for real aliasing risk. | `session/session.go:444-469` | S2 | small |

**Suggested order:** C6a → B1 → B5 → B3 → B2 → B4.
B1 and B5 are near-trivial and remove the two sharpest edges. B2 is the largest correctness item
and deserves its own commit plus the T3 test.

---

## Tier 2 — performance against the 100k PDU/s target

| ID | Finding | Location | Sev | Effort |
| --- | --- | --- | --- | --- |
| P1 | **One full frame heap allocation per outbound PDU.** No `sync.Pool` exists anywhere in the repo. At target load this is 100k allocs/s. | `session/session.go:359,421,1143` | S1 (perf) | medium |
| P2 | `append(dst, make([]byte, HeaderSize)...)` allocates a throwaway 16-byte slice per encode, twice over. | `codec/pdu.go:76,87` | S2 (perf) | trivial |
| P4 | `ownDecodedPDU` allocates once per byte-slice field; could clone into one arena. | `session/session.go` | S3 (perf) | medium |
| P3 | Registry map lookup on every encode/decode; command IDs are dense enough for a flat table. | `codec/registry.go` | S3 (perf) | medium |
| P5 | `livenessResolution()` floors at 10 ms → a 100 Hz ticker per session with short timers. Folding liveness into the shared deadline heap also removes the 4th goroutine. | `session/session.go:756` | S3 (perf) | medium |
| P6 | Size hints exist only for submit/deliver. | `codec/size_hint.go` | S4 (perf) | small |

**Suggested order:** P2 (trivial, measurable) → P1 (the big one) → P4 → P5 → P3 → P6.

`AGENTS.md` requires profile-driven performance work. Land each behind the `[profile]` CI gate
with before/after `-benchmem` numbers in the commit message. P1 and P4 compose — do P1 first,
then reuse its pool for P4's arena.

---

## Tier 3 — protocol correctness detail

| ID | Finding | Location | Sev | Effort |
| --- | --- | --- | --- | --- |
| B6 | Every semantic decode failure reports `ESME_RINVMSGLEN` (0x01), regardless of the actual fault. Misroutes peer alarms. | `session/session.go:964,1023` | S3 | medium |
| B10 | Vendor commands bypass the operation matrix — accepted in any bound state, direction never checked. Strictly more permissive than `CanIssue`. | `session/session.go:1089` | S3 | medium |
| B9 | `Reassemble` uses non-stable `sort.Slice` (arbitrary order on duplicate segments) and silently truncates when `total == 0`. | `message/message.go:207,209` | S3 | small |
| B7 | `Framer` retains up to `maxPDUSize` (1 MiB) per session forever after one fragmented large PDU. | `codec/framer.go` | S3 | small |
| B8 | `splitBytes` returns one empty segment for empty input — undocumented, probably unintended. | `message/message.go:346` | S3 | trivial |

---

## Tier 4 — hygiene, tooling, hardening

| ID | Finding | Location | Sev | Effort |
| --- | --- | --- | --- | --- |
| C1 | **12 files fail `gofmt -l .`**, including the two hottest files. No CI format gate. | repo-wide | S2 | trivial + gate |
| T2 | CI checks the `unsafe` ban but not formatting or lint. | `.github/workflows/ci.yml` | S2 | small |
| SEC2 | No bind-attempt rate limiting; unlimited online password guessing at line rate. | `server/server.go` | S2 | medium |
| T1 | No coverage measurement or floor. | CI | S2 | small |
| C2 | No `.golangci.yml`, no lint step. | repo root | S3 | small |
| T4 | Fuzz corpus not committed; 2 s CI smoke starts cold every run. | `codec/testdata/fuzz` | S3 | small |
| SEC3 | `PacketTracer` doc comment does not warn that enabling it exposes bind credentials in raw frames. | `session/observability.go` | S3 | trivial |
| SEC4 | No recommended TLS baseline documented. | `transport/transport.go` | S3 | trivial |
| C6b | No `.gitignore`. | repo root | S3 | trivial |
| C4 | `ownDecodedPDU` / `responseOptionalParameters` are parallel type switches; adding a body type requires editing both, silently. | `session/session.go` | S3 | medium |
| C3 | Dead code: `Session.noteActivity` (`session.go:698`), test-only `deadlineManager.schedule` (`deadline.go:56`). | `session/` | S4 | trivial |
| A6 | `SubmitSM` / `DeliverSM` duplication is deliberate but uncommented. | `protocol/pdu.go` | S4 | trivial |

---

## Tier 5 — structural (do after Tier 1, before v1)

| ID | Finding | Location | Sev | Effort |
| --- | --- | --- | --- | --- |
| A5 | `session/session.go` is 1401 lines spanning six concerns. Split into `tx.go` / `rx.go` / `liveness.go` / `negotiate.go` / `own.go`. Pure file movement, no API change. | `session/` | S3 | medium |

Do this **after** Tier 1 lands, so the fixes do not have to be rebased across a large file move —
but **before** v1, since it makes every subsequent review cheaper.

---

## Explicitly not recommended

| ID | Why not |
| --- | --- |
| C5 | Collapsing the ~15 typed operation wrappers into a generic helper would reduce line count but hurt greppability and godoc. The current explicit style matches the repo's stdlib-only philosophy. Leave as-is — noted only so the choice is deliberate. |

---

## Release checklist for v1.0.0

- [ ] LICENSE added (C6a)
- [ ] No `panic` reachable from any library path (B1)
- [ ] Write deadlines implemented and tested against a stalled peer (B2, T3)
- [ ] `gofmt -l .` empty, gated in CI (C1, T2)
- [ ] `golangci-lint` configured and gated (C2)
- [ ] Phase 17 reference-machine throughput result measured and published
- [ ] P1 frame pooling landed with before/after profile evidence
- [ ] Compatibility promise documented; `internal/` explicitly excluded
- [ ] `.gitignore` added (C6b)
- [ ] First git tag pushed

---

## Note on verification limits

`go test -race ./...` could **not** be run during this review — the host has no cgo/C toolchain
(`-race requires cgo`, `gcc` not found). All concurrency findings (B1–B4) are derived from
reading the code. CI does run the race detector on ubuntu-latest; re-confirm B4 in particular
under `-race` before deciding whether to keep or delete the completion-channel pool.
