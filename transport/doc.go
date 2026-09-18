// Package transport provides plain TCP and optional TLS-over-TCP adapters for
// SMPP sessions.
//
// The boundary remains net.Conn-compatible so callers may supply custom ordered
// byte streams with TCP-like semantics. Plain *net.TCPConn sessions can use
// net.Buffers scatter/gather writes for bounded TX batching. X.25 is
// intentionally out of scope.
package transport
