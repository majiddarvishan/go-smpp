// Package client provides the high-level ESME/client facade over the shared
// SMPP session engine. Active Client request methods are safe for concurrent
// use. Automatic reconnect/rebind is added in its dedicated phase.
package client
