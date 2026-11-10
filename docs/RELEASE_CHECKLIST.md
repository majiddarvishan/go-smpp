# Release checklist

Phase 19 release readiness is complete only when every item below is true.

- Public API freeze and compatibility policy reviewed.
- Concurrency, timeout/liveness, malformed-PDU, vendor-extension, encoding and safe packet-capture/observability docs match implementation.
- Raw packet tracing remains metadata-only by default; raw capture is explicit opt-in, and regression tests prove caller-controlled secrets are not copied into default logs/events/error strings.
- ESME and SMSC examples compile with the minimum Go version.
- go test ./... and go test -race ./... pass on Linux/amd64.
- Phase 18 reliability/fuzz/chaos gates remain green.
- Phase 17 reference acceptance has completed on the documented 8-core / 10-GB Linux/amd64 host.
- The exact reference environment, minimum passing TCP session count, sustained request-PDU/s, memory bounds, and commit are published.
- The reference run starts from a clean tree on Linux/amd64, Go 1.27.x, 8+ logical CPUs, 10+ GiB RAM; `scripts/acceptance.sh --preflight` passes and records the exact source SHA.
- A separate 60-second Phase 17 acceptance rerun has passed on the current executable candidate HEAD after post-acceptance library additions; a 2-second CI smoke does not satisfy this item. Reverify if the integration to `main` changes executable source.
- Only after the reference evidence is published is the first production-ready tag created.

The first production-ready tag establishes the v1 semantic-version compatibility line. Do not create that tag from an unverified reference-performance state.

## Current evidence status — 2026-11-10

The clean candidate `ebf28340fbe7a49b9ab6a8a0a9a34d7ba924dee9` passed `scripts/acceptance.sh --preflight`, race checks and the required 60-second reference acceptance with one TCP session at 683,465 request PDUs/s (100,000 required). See `docs/PERFORMANCE.md` for all reported load, resource and response measurements.

**Still to verify before tagging:** inspect/retain the `.bench/phase17-reference` generated evidence (including exact kernel/time, race log, one-session acceptance log and profiles); complete release checklist and outstanding deployed-peer/interop decisions. No artifact upload or independent profile inspection is claimed. PR #8 stays Draft/Open; no merge, Ready transition or release tag is authorized by this performance PASS alone.

### Performance evidence audit result — 2026-11-10

PERF-01 / PERF-02 **PASS** for the clean-source 60-second reference acceptance and uploaded nine-file text evidence archive (SHA-256 `3190baafa81397c08daa850c45808c8355df39cab3c65799028283186ac3d1b9`). All reported race, performance, response equality, resource bounds, source/kernel/toolchain provenance and cross-profile build identifiers were verified. See `docs/PERFORMANCE.md` for scope, metrics, and the distinction between cumulative `alloc_space`/block time and retained memory/wall clock. The binary `.pprof` files remain local and were not independently examined. Continue to withhold release tag until the remainder of this checklist and outstanding interoperability findings are resolved; retest if integrated executable code changes.
