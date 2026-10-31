package codec

import (
	"runtime/debug"

	"github.com/majiddarvishan/go-smpp/protocol"
)

// decodeBody runs a command decoder, which may be caller-supplied (a vendor
// command registered on a RegistryBuilder) and runs on the session's receive
// goroutine. A panic in it is returned as a semantic error carrying a
// *protocol.PanicError: the session then rejects that one PDU with
// ESME_RSYSERR and keeps the connection, instead of the panic killing the
// process. Framing is unaffected, because the frame was already delimited.
func decodeBody(def CommandDefinition, header Header, body []byte) (value any, err error) {
	defer func() {
		if r := recover(); r != nil {
			value = nil
			err = &protocol.SemanticError{
				Status:   protocol.StatusSystemError,
				Command:  header.CommandID,
				Sequence: header.SequenceNumber,
				Reason:   "command decoder panicked",
				Cause:    &protocol.PanicError{Source: "codec.CommandDecoder", Value: r, Stack: debug.Stack()},
			}
		}
	}()
	return def.Decode(header, body)
}

// encodeBody runs a command encoder; see decodeBody. A panic is returned as a
// *protocol.PanicError and dst is returned unchanged.
func encodeBody(def CommandDefinition, dst []byte, body any) (out []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			out = dst
			err = &protocol.PanicError{Source: "codec.CommandEncoder", Value: r, Stack: debug.Stack()}
		}
	}()
	return def.Encode(dst, body)
}
