// Package session implements the shared asynchronous SMPP session engine used
// by both ESME/client and SMSC/server roles.
//
// Session is safe for concurrent use. Public request methods are synchronous and
// context-aware while independent long-lived RX/TX goroutines keep the wire
// asynchronous, pipelined, and capable of out-of-order response correlation.
// The package does not create a goroutine per request.
package session
