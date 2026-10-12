// Package server provides an SMSC/server facade over the shared SMPP session
// engine.
//
// Server can listen on plain TCP or TLS-over-TCP, authenticate bind requests,
// handle inbound submit_sm, and originate deliver_sm, data_sm,
// alert_notification, and outbind operations. Server methods are safe for
// concurrent use, although only one Serve call may be active. A malformed
// client session is isolated from the listener and unrelated sessions.
//
// Connection admission is controlled by Config.MaxSessions (total sessions),
// Config.MaxUnboundPerIP (connections per remote IP that have not yet bound, off
// by default) and Config.BindRateLimiter (repeated failed binds); see
// docs/DECISIONS.md D3 for the reasoning and limits of the middle one.
//
// TLS is enabled by setting Config.TLSConfig; a nil TLSConfig means plain TCP.
// The recommended tls.Config baseline, and what is easy to get wrong, is
// documented in package transport.
package server
