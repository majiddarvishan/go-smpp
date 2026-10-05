// Package client provides the high-level ESME facade over the shared SMPP
// session engine.
//
// Client is safe for concurrent use. Public request methods are synchronous and
// context-aware while the underlying session remains asynchronous/pipelined.
// Dialed clients can reconnect and rebind after session loss, but requests from
// a lost session are never silently replayed on a replacement connection.
//
// Client.Session returns a session snapshot for inspection; applications should
// not cache it across reconnects.
//
// TLS is enabled by setting Config.TLSConfig; a nil TLSConfig means plain TCP.
// Set ServerName and keep certificate verification on. The recommended
// tls.Config baseline, and what is easy to get wrong, is documented in package
// transport.
package client
