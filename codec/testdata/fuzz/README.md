# Fuzz corpus

Seed corpus for `FuzzFramer` and `FuzzDecodePDU` (see `../../fuzz_test.go`). Go runs every file here as a
subtest under a plain `go test`, so these inputs are a regression suite, and `go test -fuzz` starts from
them instead of from the three inline seeds, so the CI smoke is not cold.

- Provenance: the inputs the Go fuzzing engine kept as coverage-increasing ("interesting") while fuzzing each
  target on 2026-10-05: `FuzzFramer` 120 s / 3.85 M executions, `FuzzDecodePDU` 150 s / 4.93 M executions,
  from a cold cache, Go 1.22, one CPU. No crashing input was found, so there are no crashers here yet. When
  one is found, the engine writes it into the matching directory; commit it with the fix.
- Files are named by content hash, in the engine's `go test fuzz v1` format.
- `FuzzDecodePDU` uses the SMPP 3.4 registry in compatible mode only; the SMPP 5.0 registry is not fuzzed.

Refreshing: run a target for a while, then copy new files from the cache into the matching directory.

```sh
go test ./codec -run '^$' -fuzz=FuzzDecodePDU -fuzztime=120s
cp "$(go env GOCACHE)"/fuzz/github.com/majiddarvishan/go-smpp/codec/FuzzDecodePDU/* codec/testdata/fuzz/FuzzDecodePDU/
```
