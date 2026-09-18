# Release checklist

Phase 19 release readiness is complete only when every item below is true.

- Public API freeze and compatibility policy reviewed.
- Concurrency, timeout/liveness, malformed-PDU, vendor-extension, and encoding interoperability docs match implementation.
- ESME and SMSC examples compile with the minimum Go version.
- go test ./... and go test -race ./... pass on Linux/amd64.
- Phase 18 reliability/fuzz/chaos gates remain green.
- Phase 17 reference acceptance has completed on the documented 8-core / 10-GB Linux/amd64 host.
- The exact reference environment, minimum passing TCP session count, sustained request-PDU/s, memory bounds, and commit are published.
- Only after the reference evidence is published is the first production-ready tag created.

The first production-ready tag establishes the v1 semantic-version compatibility line. Do not create that tag from an unverified reference-performance state.
