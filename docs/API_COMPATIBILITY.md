# Public API compatibility policy

Phase 19 freezes the release-facing Go API of github.com/majiddarvishan/go-smpp and its public packages.

## Compatibility baseline

The API freeze starts with the Phase 19 release-readiness work. Until the first production-ready v1.0.0 tag is cut, an incompatible correction is allowed only when it fixes a correctness, security, or protocol-compliance defect that cannot reasonably be repaired compatibly. Such a change must be called out in release notes and recorded in project decisions before the tag.

Starting with v1.0.0, the project follows semantic-version compatibility for public Go APIs:

- patch releases may fix bugs and add behavior that does not require source changes;
- minor releases may add exported symbols, optional fields, commands, TLVs, or capabilities without breaking existing source;
- removal, rename, incompatible signature changes, or materially incompatible semantics require a new major version.

## What the promise covers

The compatibility promise applies to exported identifiers in the root package and the public packages `client`, `server`, `session`, `codec`, `protocol`, `encoding`, `message` and `transport`. "Exported identifier" means functions, methods, types, constants, variables, exported struct fields, and the method sets of exported interfaces.

It does **not** apply to:

- anything under `internal/` (today `internal/perflab`, the benchmark harness). Go forbids importing it from outside this module, and nothing in it is a promise;
- the programs under `cmd/` and `examples/`: their flags, output and structure may change in any release;
- `scripts/`, `docs/`, tests, and unexported identifiers.

The usual Go rules for what counts as breaking apply, and a few are worth stating because this API exposes interfaces and configuration structs:

- adding a field to a `Config` struct is compatible, which is why configuration is set with keyed fields; unkeyed struct literals are not supported;
- **adding a method to an exported interface is breaking** when callers implement it (for example `session.Handler`, `session.PacketTracer`, `server.Authenticator`, `server.BindRateLimiter`), so such interfaces stay as small as they are, and new capabilities arrive as new optional interfaces or new fields;
- changing a function or method signature, or the type of an exported field, is breaking even if existing call sites happen to still compile.

## How it is checked, and how far

Four tests in the root package keep this honest. The first below is the exhaustive one; the others are narrower, and it is worth being exact about what each does:

| Test | What it enforces | What it does not |
| --- | --- | --- |
| `api_snapshot_test.go` | **Every exported identifier** of the eight public packages, against the committed files in `testdata/api/`: constants with their values, variables, functions, types, the exported fields of structs, the methods of every named type, the methods of interfaces, and whether an interface is sealed. Anything removed or changed fails the test (it is breaking); anything added fails it too, until the snapshot is updated on purpose so a reviewer sees it. Unexported identifiers never appear, so changing them never fails it. A new public package without a snapshot, or a snapshot of a package that is gone, fails as well. | Behaviour: it sees signatures and shapes, not what a function does. It does not see struct tags or the order of struct fields. |
| `api_contract_test.go` | A compile-time pin of 28 selected identifiers across `client`, `server`, `session`, `codec`, `encoding`, `message` and `protocol`. For all 28, removal or rename stops the build of `go test`. For five (`client.Dial`, `client.New`, `server.Listen`, `server.ListenAndServe`, `session.New`) the exact function signature is pinned too; the rest are pinned by existence only, so a changed signature on them is not caught. | It is a representative sample, not an enumeration; the snapshot test above is the exhaustive one. |
| `architecture_test.go` | Dependency direction between the eight public packages (for example `protocol`, `encoding` and `transport` import no sibling). | Anything about the shape of exported identifiers. |
| `posture_test.go` | No third-party dependencies, no `unsafe`, no cgo. | Anything about the API. |

When the API changes, the snapshot test fails and prints what was **removed or changed** (breaking, so a compatibility decision first, per the baseline above) and what was **added** (allowed, but acknowledged). After a deliberate change, accept it with:

```sh
go test -run TestExportedAPISnapshot -update .
```

and commit the changed files in `testdata/api/`; the diff of those files is the API change a reviewer reads. The snapshot is generated with the standard library only (`go/types`, no new dependency), which is why a method added to an exported interface, a changed signature or a changed constant value are all visible. The one place Go's own type printer varies between releases is how it prints a type alias; the test pins `gotypesalias=1` so the output reads the same on every supported Go version.

## Stable behavioral contracts

The following are part of the public compatibility contract:

- transport is an ordered TCP-compatible byte stream; TLS is TLS-over-TCP;
- locally generated sequence numbers are non-zero and stay within 0x00000001..0x7fffffff;
- inbound non-zero sequence numbers through 0xffffffff are accepted and echoed exactly in responses;
- request APIs are context-aware while the wire engine remains asynchronous and may have multiple requests in flight;
- requests are never silently replayed after a lost connection;
- fatal structural/framing corruption is fail-closed: emit a safe diagnostic, poison parsing state, and close only the offending session;
- no exported entry point and no library goroutine lets a panic reach the host process; a recovered panic costs at most the PDU, notification or session it occurred in (`docs/CONCURRENCY.md`, "Panics");
- the default maximum PDU size is bounded and remains configurable;
- strict/compatible registry handling never weakens framing validation;
- GSM 7-bit, strict UCS-2, and UTF-16BE-with-surrogates remain distinct encoding choices.

## Internal freedom

Unexported types, internal package layout, goroutine arrangement, allocation strategy, benchmark implementation, and private helper behavior may change as long as the public contracts above remain intact.

Configuration defaults may evolve only when the change is source compatible and documented in release notes. Applications that require exact operational behavior should set timeouts, window size, maximum PDU size, and batching explicitly.

## Deprecation

When practical, an API scheduled for removal is first documented as deprecated and kept through at least one minor release. After v1.0.0, breaking API changes require the normal major-version process.
