package transport

import (
	"crypto/tls"
	"net"
	"testing"
)

// A nil context means context.Background() rather than a panic inside net.
func TestNilContextMeansBackground(t *testing.T) {
	listener, err := ListenTCP(nil, "", "127.0.0.1:0", nil) //nolint:staticcheck // a nil ctx is the case under test
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	conn, err := DialTCP(nil, "", listener.Addr().String(), nil) //nolint:staticcheck // a nil ctx is the case under test
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()

	// The peer closes without a handshake, so this fails; what matters is that
	// a nil ctx reaches the handshake instead of panicking.
	if conn, err := DialTLS(nil, "", listener.Addr().String(), &net.Dialer{}, &tls.Config{ServerName: "x"}); err == nil { //nolint:staticcheck // a nil ctx is the case under test
		_ = conn.Close()
	}

	tlsListener, err := ListenTLS(nil, "", "127.0.0.1:0", nil, &tls.Config{}) //nolint:staticcheck // a nil ctx is the case under test
	if err != nil {
		t.Fatal(err)
	}
	_ = tlsListener.Close()
}
