package protocol

import "fmt"

// PanicError reports a panic that this module recovered instead of letting it
// crash the host process. The library never panics on purpose, and it recovers
// any panic raised by code it calls on its own goroutines: user callbacks such
// as a session Handler, Observer or Authenticator, a registry's CommandDecoder
// or CommandEncoder, a caller-supplied net.Conn, or a defect in the library.
//
// Source names what panicked, for example "session.Handler" or
// "codec.CommandDecoder". Value is the original value passed to panic and may
// contain credentials, message content or other application data; inspect it
// only deliberately and never log it. Stack is the stack of the goroutine that
// panicked, captured at the recovery. Error deliberately omits Value.
type PanicError struct {
	Source string
	Value  any
	Stack  []byte
}

func (e *PanicError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("smpp: recovered panic in %s (value type %T)", e.Source, e.Value)
}

// Unwrap returns Value when it is an error, so errors.Is and errors.As can see
// a panic(err) through the PanicError. Callers that inspect or format the
// unwrapped error are responsible for treating its text as untrusted.
func (e *PanicError) Unwrap() error {
	if e == nil {
		return nil
	}
	err, _ := e.Value.(error)
	return err
}
