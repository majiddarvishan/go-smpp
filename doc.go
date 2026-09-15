// Package smpp provides the public entry point for the go-smpp library.
//
// The initial implementation targets SMPP 3.4 over TCP while keeping the
// protocol core extensible for SMPP 5.0. High-level client and server APIs are
// added in later implementation phases; low-level protocol primitives live in
// package protocol.
package smpp
