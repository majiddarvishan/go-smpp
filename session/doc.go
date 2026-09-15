// Package session will implement the shared asynchronous SMPP session engine.
//
// Public high-level APIs may be synchronous, while this package remains
// pipelined, thread-safe, and capable of out-of-order response correlation.
package session
