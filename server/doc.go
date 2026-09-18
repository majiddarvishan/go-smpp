// Package server provides an SMSC/server facade over the shared SMPP session
// engine.
//
// Server can listen on plain TCP or TLS-over-TCP, authenticate bind requests,
// handle inbound submit_sm, and originate deliver_sm, data_sm,
// alert_notification, and outbind operations. Server methods are safe for
// concurrent use, although only one Serve call may be active. A malformed
// client session is isolated from the listener and unrelated sessions.
package server
