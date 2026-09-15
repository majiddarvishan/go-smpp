// Package transport provides plain TCP and optional TLS-over-TCP adapters for
// SMPP sessions. The boundary remains net.Conn-compatible so callers may supply
// custom streams that preserve TCP-style ordered byte-stream semantics.
//
// X.25 is intentionally out of scope.
package transport
