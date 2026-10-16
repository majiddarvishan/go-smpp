# Fuzz corpus

Seed corpus for `FuzzFramer` and `FuzzDecodePDU` (see `../../fuzz_test.go`) `FuzzFramerChunked` (see `../../fuzz_chunked_test.go`) and `FuzzDecodePDU50` (see `../../fuzz_decode50_test.go`). Go runs every file here as a
subtest under a plain `go test`, so these inputs are a regression suite, and `go test -fuzz` starts from
them instead of from the three inline seeds, so the CI smoke is not cold.

- Provenance: the inputs the Go fuzzing engine kept as coverage-increasing ("interesting") while fuzzing each
  target on 2026-10-05: `FuzzFramer` 120 s / 3.85 M executions, `FuzzDecodePDU` 150 s / 4.93 M executions,
  from a cold cache, Go 1.22, one CPU. No crashing input was found, so there are no crashers here yet. When
  one is found, the engine writes it into the matching directory; commit it with the fix.
- Files are named by content hash, in the engine's `go test fuzz v1` format.
- `FuzzFramerChunked` (added 2026-10-13): fuzzed 100 s / 2.49 M executions from a cold cache, one CPU, no crashing input; its inputs are two byte strings, the stream and a plan for how to cut it into reads, and the property is that cutting changes nothing.
- `FuzzDecodePDU50` (added 2026-10-13): the SMPP 5.0 registry in both modes, first input byte picks the mode. two runs of 120 s and 130 s (3.54 M and 3.67 M executions) from a cold cache, no crashing input left. Two findings on the way: a decoder/encoder asymmetry for responses with a non-zero command_status (the encoder was fixed, see `codec/error_response.go`; the second run is with the fix and covers every status) and a property that wrongly treated a nil slice and an empty one as different (fixed; it now compares encoded bytes).
- `FuzzDecodePDU` uses the SMPP 3.4 registry in compatible mode only; SMPP 5.0 is covered by `FuzzDecodePDU50`.

Refreshing: run a target for a while, then copy new files from the cache into the matching directory.

```sh
go test ./codec -run '^$' -fuzz=FuzzDecodePDU -fuzztime=120s
cp "$(go env GOCACHE)"/fuzz/github.com/majiddarvishan/go-smpp/codec/FuzzDecodePDU/* codec/testdata/fuzz/FuzzDecodePDU/
```

Note for anyone adding a target: `-fuzz` takes a regular expression, so a name that is a prefix of another (FuzzFramer, FuzzFramerChunked) matches both and Go refuses to fuzz ("matches more than one fuzz test"). Anchor it: `-fuzz='^FuzzFramer$'`.
