// Package codec implements SMPP PDU framing, binary field encoding/decoding,
// command codecs, TLV handling, and extensible command/TLV registries.
//
// Framer consumes an ordered TCP byte stream and supports fragmented and
// coalesced reads. A Framer has one stream owner and is not concurrent-safe.
// Fatal structural corruption poisons it permanently; callers must close the
// owning connection rather than attempt resynchronization.
//
// RegistryBuilder is concurrent-safe during setup. Freeze returns an immutable
// Registry suitable for concurrent reads on active sessions.
package codec
