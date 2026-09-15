// Package server provides the high-level SMSC/server facade over the shared
// SMPP session engine. Server lifecycle and active-session snapshots are safe
// for concurrent use; richer authentication/session policy is added in the
// dedicated server phase.
package server
