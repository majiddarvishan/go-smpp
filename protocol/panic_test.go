package protocol

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestPanicError(t *testing.T) {
	var nilErr *PanicError
	if nilErr.Error() != "<nil>" || nilErr.Unwrap() != nil {
		t.Fatalf("nil PanicError: Error()=%q Unwrap()=%v", nilErr.Error(), nilErr.Unwrap())
	}

	const secret = "PANIC_SECRET_MARKER"
	byValue := &PanicError{Source: "session.Handler", Value: secret}
	if got := byValue.Error(); !strings.Contains(got, "session.Handler") || !strings.Contains(got, "value type string") || strings.Contains(got, secret) {
		t.Fatalf("Error() = %q, want source/type without the panic value", got)
	}
	if byValue.Unwrap() != nil {
		t.Fatal("Unwrap() of a non-error panic value is not nil")
	}

	byError := &PanicError{Source: "codec.CommandDecoder", Value: io.ErrUnexpectedEOF}
	if !errors.Is(byError, io.ErrUnexpectedEOF) {
		t.Fatal("errors.Is does not see a panic(err) through the PanicError")
	}
}
