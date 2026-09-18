// Package session implements the shared asynchronous SMPP session engine used
// by both ESME/client and SMSC/server roles.
//
// Session and StateMachine are safe for concurrent use. Public request methods
// are synchronous and context-aware while independent long-lived RX/TX paths
// keep the wire asynchronous, pipelined, and capable of out-of-order response
// correlation. The package does not create a goroutine or timer per request.
//
// The runtime provides bounded request windowing, shared response deadlines,
// Session Init, Enquire Link and inactivity liveness handling, SMPP 5.0
// congestion callbacks, observability hooks, and fail-closed handling of fatal
// structural/framing errors.
package session
