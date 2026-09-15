// Package client provides the high-level ESME/client facade over the shared
// SMPP session engine. Active Client request methods are safe for concurrent
// use. Dialed clients can be configured for automatic reconnect and rebind;
// requests from a lost session are never silently replayed.
package client
