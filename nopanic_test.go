package smpp_test

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/majiddarvishan/go-smpp/client"
	"github.com/majiddarvishan/go-smpp/codec"
	"github.com/majiddarvishan/go-smpp/encoding"
	"github.com/majiddarvishan/go-smpp/message"
	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/server"
	"github.com/majiddarvishan/go-smpp/session"
	"github.com/majiddarvishan/go-smpp/transport"
)

// TestNoExportedEntryPointPanicsOnZeroValues is the mechanical half of the
// rule that this library never panics: it calls every exported function, and
// every exported method on a nil and on a zero-value receiver, with zero
// values for every argument (nil contexts, nil slices, nil callbacks, zero
// configs), and fails on any panic or any call that does not return.
//
// The lists below are checked against the API snapshot in testdata/api, so a
// new exported function or a new type with methods fails this test until it
// is added here. The other half, panics inside callbacks and on session
// goroutines, is covered by session/panic_test.go and server/panic_test.go.
//
// A value method called through a nil pointer panics in Go itself, before
// any library code runs, so nil receivers are probed only with the methods
// declared on the pointer type.
func TestNoExportedEntryPointPanicsOnZeroValues(t *testing.T) {
	for name, fn := range noPanicFuncs {
		probeCall(t, name, reflect.ValueOf(fn))
	}
	for _, ptr := range noPanicPointerTypes {
		elem := ptr.Elem()
		for i := 0; i < ptr.NumMethod(); i++ {
			m := ptr.Method(i)
			if _, isValueMethod := elem.MethodByName(m.Name); !isValueMethod {
				probeCall(t, fmt.Sprintf("(%s)(nil).%s", ptr, m.Name), reflect.Zero(ptr).Method(i))
			}
			probeCall(t, fmt.Sprintf("(&%s{}).%s", elem, m.Name), reflect.New(elem).Method(i))
		}
	}
	for _, typ := range noPanicValueTypes {
		for i := 0; i < typ.NumMethod(); i++ {
			probeCall(t, fmt.Sprintf("(%s{}).%s", typ, typ.Method(i).Name), reflect.Zero(typ).Method(i))
		}
	}
}

// noPanicSkipped lists exported functions that are deliberately not probed,
// with the reason. Keep it short: each entry is a hole in the guarantee.
var noPanicSkipped = map[string]string{
	"server.ListenAndServe": "with a zero Config it serves on a random port until ctx ends, and ctx is nil",
}

func probeCall(t *testing.T, name string, fn reflect.Value) {
	t.Helper()
	typ := fn.Type()
	args := make([]reflect.Value, typ.NumIn())
	for i := range args {
		args[i] = reflect.Zero(typ.In(i))
	}
	result := make(chan any, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				result <- r
			}
		}()
		var out []reflect.Value
		if typ.IsVariadic() {
			out = fn.CallSlice(args)
		} else {
			out = fn.Call(args)
		}
		for _, v := range out {
			// A zero Config listens on a random port; do not leak it.
			switch c := v.Interface().(type) {
			case net.Listener:
				if c != nil {
					_ = c.Close()
				}
			case *server.Server:
				_ = c.Close()
			}
		}
		result <- nil
	}()
	select {
	case r := <-result:
		if r != nil {
			t.Errorf("%s panicked on zero values: %v", name, r)
		}
	case <-time.After(5 * time.Second):
		t.Errorf("%s did not return on zero values", name)
	}
}

// TestNoPanicProbeCoversTheWholeAPI fails when the snapshot in testdata/api
// lists an exported function or a type with methods that the probe above
// does not call, or when the probe names something the snapshot does not
// have.
func TestNoPanicProbeCoversTheWholeAPI(t *testing.T) {
	funcLine := regexp.MustCompile(`^func ([A-Z]\w*)`)
	methodLine := regexp.MustCompile(`^method \((\*?)([A-Z]\w*)\)\.`)
	wantFuncs := map[string]bool{}
	wantTypes := map[string]bool{}
	files, err := filepath.Glob(filepath.Join("testdata", "api", "*.txt"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no API snapshot files: %v", err)
	}
	for _, file := range files {
		pkg := strings.TrimSuffix(filepath.Base(file), ".txt")
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			if m := funcLine.FindStringSubmatch(line); m != nil {
				wantFuncs[pkg+"."+m[1]] = true
			}
			if m := methodLine.FindStringSubmatch(line); m != nil {
				wantTypes[m[1]+pkg+"."+m[2]] = true
			}
		}
	}

	haveFuncs := map[string]bool{}
	for name := range noPanicFuncs {
		haveFuncs[name] = true
	}
	for name := range noPanicSkipped {
		haveFuncs[name] = true
	}
	haveTypes := map[string]bool{}
	for _, typ := range noPanicPointerTypes {
		haveTypes[typ.String()] = true
	}
	for _, typ := range noPanicValueTypes {
		haveTypes[typ.String()] = true
	}
	for name := range wantFuncs {
		if !haveFuncs[name] {
			t.Errorf("exported func %s is not in noPanicFuncs (or noPanicSkipped, with a reason)", name)
		}
	}
	for name := range haveFuncs {
		if !wantFuncs[name] {
			t.Errorf("noPanicFuncs names %s, which the API snapshot does not have", name)
		}
	}
	for name := range wantTypes {
		if !haveTypes[name] {
			t.Errorf("type %s has exported methods but is not in noPanicPointerTypes or noPanicValueTypes", name)
		}
	}
	for name := range haveTypes {
		if !wantTypes[name] {
			t.Errorf("the probe lists %s, which has no exported methods in the API snapshot", name)
		}
	}
}

var noPanicFuncs = map[string]any{
	"client.Dial":                          client.Dial,
	"client.New":                           client.New,
	"codec.AppendCString":                  codec.AppendCString,
	"codec.AppendOctets":                   codec.AppendOctets,
	"codec.AppendTLV":                      codec.AppendTLV,
	"codec.DecodeHeader":                   codec.DecodeHeader,
	"codec.DecodePDU":                      codec.DecodePDU,
	"codec.EncodeHeader":                   codec.EncodeHeader,
	"codec.EncodePDU":                      codec.EncodePDU,
	"codec.EncodedPDUSizeHint":             codec.EncodedPDUSizeHint,
	"codec.NewFramer":                      codec.NewFramer,
	"codec.NewRegistryBuilder":             codec.NewRegistryBuilder,
	"codec.NewSMPP34Registry":              codec.NewSMPP34Registry,
	"codec.NewSMPP34RegistryBuilder":       codec.NewSMPP34RegistryBuilder,
	"codec.NewSMPP50Registry":              codec.NewSMPP50Registry,
	"codec.NewSMPP50RegistryBuilder":       codec.NewSMPP50RegistryBuilder,
	"codec.ParseTLVs":                      codec.ParseTLVs,
	"codec.PutUint16":                      codec.PutUint16,
	"codec.PutUint32":                      codec.PutUint32,
	"codec.PutUint8":                       codec.PutUint8,
	"codec.ReadCString":                    codec.ReadCString,
	"codec.ReadOctets":                     codec.ReadOctets,
	"codec.ReadUint16":                     codec.ReadUint16,
	"codec.ReadUint32":                     codec.ReadUint32,
	"codec.ReadUint8":                      codec.ReadUint8,
	"codec.ResponseHeader":                 codec.ResponseHeader,
	"codec.ScanTLVs":                       codec.ScanTLVs,
	"encoding.CanEncodeGSM7":               encoding.CanEncodeGSM7,
	"encoding.CloneBinary":                 encoding.CloneBinary,
	"encoding.DecodeGSM7":                  encoding.DecodeGSM7,
	"encoding.DecodeUCS2":                  encoding.DecodeUCS2,
	"encoding.DecodeUTF16BE":               encoding.DecodeUTF16BE,
	"encoding.EncodeGSM7":                  encoding.EncodeGSM7,
	"encoding.EncodeUCS2":                  encoding.EncodeUCS2,
	"encoding.EncodeUTF16BE":               encoding.EncodeUTF16BE,
	"encoding.GSM7SeptetCount":             encoding.GSM7SeptetCount,
	"encoding.PackSeptets":                 encoding.PackSeptets,
	"encoding.PackSeptetsWithFill":         encoding.PackSeptetsWithFill,
	"encoding.SelectText":                  encoding.SelectText,
	"encoding.UTF16CodeUnitCount":          encoding.UTF16CodeUnitCount,
	"encoding.UnpackSeptets":               encoding.UnpackSeptets,
	"message.BuildConcatUDH16":             message.BuildConcatUDH16,
	"message.BuildConcatUDH8":              message.BuildConcatUDH8,
	"message.DecodeTextSegments":           message.DecodeTextSegments,
	"message.EncodeText":                   message.EncodeText,
	"message.GSM7UDHFillBits":              message.GSM7UDHFillBits,
	"message.ParseConcatUDH":               message.ParseConcatUDH,
	"message.ParseSAR":                     message.ParseSAR,
	"message.Reassemble":                   message.Reassemble,
	"message.SARTLVs":                      message.SARTLVs,
	"message.SegmentBinarySAR":             message.SegmentBinarySAR,
	"message.SegmentBinaryUDH":             message.SegmentBinaryUDH,
	"message.SegmentTextSAR":               message.SegmentTextSAR,
	"message.SegmentTextUDH":               message.SegmentTextUDH,
	"protocol.CongestionStateFromOptional": protocol.CongestionStateFromOptional,
	"protocol.NegotiatePeerCapabilities":   protocol.NegotiatePeerCapabilities,
	"protocol.PutUint32":                   protocol.PutUint32,
	"protocol.ReadUint32":                  protocol.ReadUint32,
	"protocol.SCInterfaceVersion":          protocol.SCInterfaceVersion,
	"protocol.SMPP34Profile":               protocol.SMPP34Profile,
	"protocol.SMPP50Profile":               protocol.SMPP50Profile,
	"server.Listen":                        server.Listen,
	"server.New":                           server.New,
	"server.NewBindThrottle":               server.NewBindThrottle,
	"server.NewWithConfig":                 server.NewWithConfig,
	"session.CanIssue":                     session.CanIssue,
	"session.IsStandardCommand":            session.IsStandardCommand,
	"session.New":                          session.New,
	"session.NewStateMachine":              session.NewStateMachine,
	"transport.DialTCP":                    transport.DialTCP,
	"transport.DialTLS":                    transport.DialTLS,
	"transport.ListenTCP":                  transport.ListenTCP,
	"transport.ListenTLS":                  transport.ListenTLS,
	"transport.ReadFull":                   transport.ReadFull,
	"transport.TLSClient":                  transport.TLSClient,
	"transport.TLSServer":                  transport.TLSServer,
	"transport.WriteBuffers":               transport.WriteBuffers,
	"transport.WriteFull":                  transport.WriteFull,
}

var noPanicPointerTypes = []reflect.Type{
	reflect.TypeFor[*client.Client](),
	reflect.TypeFor[*client.UnavailableError](),
	reflect.TypeFor[*codec.Framer](),
	reflect.TypeFor[*codec.Registry](),
	reflect.TypeFor[*codec.RegistryBuilder](),
	reflect.TypeFor[*protocol.FatalError](),
	reflect.TypeFor[*protocol.PanicError](),
	reflect.TypeFor[*protocol.SemanticError](),
	reflect.TypeFor[*server.Server](),
	reflect.TypeFor[*session.LossError](),
	reflect.TypeFor[*session.ResponseError](),
	reflect.TypeFor[*session.Session](),
	reflect.TypeFor[*session.StateError](),
	reflect.TypeFor[*session.StateMachine](),
	reflect.TypeFor[*session.TimeoutError](),
}

var noPanicValueTypes = []reflect.Type{
	reflect.TypeFor[codec.CommandActor](),
	reflect.TypeFor[codec.CommandDefinition](),
	reflect.TypeFor[codec.RegistryMode](),
	reflect.TypeFor[encoding.Kind](),
	reflect.TypeFor[protocol.CommandID](),
	reflect.TypeFor[protocol.CommandStatus](),
	reflect.TypeFor[protocol.CongestionState](),
	reflect.TypeFor[protocol.FatalErrorKind](),
	reflect.TypeFor[protocol.Profile](),
	reflect.TypeFor[protocol.SequenceNumber](),
	reflect.TypeFor[protocol.SessionState](),
	reflect.TypeFor[server.AuthenticatorFunc](),
	reflect.TypeFor[server.SubmitHandlerFunc](),
	reflect.TypeFor[session.BindMode](),
	reflect.TypeFor[session.EventKind](),
	reflect.TypeFor[session.FlowControllerFunc](),
	reflect.TypeFor[session.HandlerFunc](),
	reflect.TypeFor[session.ObserverFunc](),
	reflect.TypeFor[session.PacketTracerFunc](),
	reflect.TypeFor[session.Role](),
	reflect.TypeFor[session.TimeoutKind](),
}
