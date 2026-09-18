# Public API compatibility policy

Phase 19 freezes the release-facing Go API of github.com/majiddarvishan/go-smpp and its public packages.

## Compatibility baseline

The API freeze starts with the Phase 19 release-readiness work. Until the first production-ready v1.0.0 tag is cut, an incompatible correction is allowed only when it fixes a correctness, security, or protocol-compliance defect that cannot reasonably be repaired compatibly. Such a change must be called out in release notes and recorded in project decisions before the tag.

Starting with v1.0.0, the project follows semantic-version compatibility for public Go APIs:

- patch releases may fix bugs and add behavior that does not require source changes;
- minor releases may add exported symbols, optional fields, commands, TLVs, or capabilities without breaking existing source;
- removal, rename, incompatible signature changes, or materially incompatible semantics require a new major version.

The compatibility promise applies to exported identifiers in the root package and the public packages client, server, session, codec, protocol, encoding, message, and transport.

## Stable behavioral contracts

The following are part of the public compatibility contract:

- transport is an ordered TCP-compatible byte stream; TLS is TLS-over-TCP;
- locally generated sequence numbers are non-zero and stay within 0x00000001..0x7fffffff;
- inbound non-zero sequence numbers through 0xffffffff are accepted and echoed exactly in responses;
- request APIs are context-aware while the wire engine remains asynchronous and may have multiple requests in flight;
- requests are never silently replayed after a lost connection;
- fatal structural/framing corruption is fail-closed: emit a safe diagnostic, poison parsing state, and close only the offending session;
- the default maximum PDU size is bounded and remains configurable;
- strict/compatible registry handling never weakens framing validation;
- GSM 7-bit, strict UCS-2, and UTF-16BE-with-surrogates remain distinct encoding choices.

## Internal freedom

Unexported types, internal package layout, goroutine arrangement, allocation strategy, benchmark implementation, and private helper behavior may change as long as the public contracts above remain intact.

Configuration defaults may evolve only when the change is source compatible and documented in release notes. Applications that require exact operational behavior should set timeouts, window size, maximum PDU size, and batching explicitly.

## Deprecation

When practical, an API scheduled for removal is first documented as deprecated and kept through at least one minor release. After v1.0.0, breaking API changes require the normal major-version process.
