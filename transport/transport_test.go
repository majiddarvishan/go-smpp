package transport

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net"
	"testing"
	"time"
)

type partialConn struct {
	buf bytes.Buffer
	max int
}

func (c *partialConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (c *partialConn) Write(p []byte) (int, error)      { if len(p) > c.max { p = p[:c.max] }; return c.buf.Write(p) }
func (c *partialConn) Close() error                     { return nil }
func (c *partialConn) LocalAddr() net.Addr              { return dummyAddr("local") }
func (c *partialConn) RemoteAddr() net.Addr             { return dummyAddr("remote") }
func (c *partialConn) SetDeadline(time.Time) error      { return nil }
func (c *partialConn) SetReadDeadline(time.Time) error  { return nil }
func (c *partialConn) SetWriteDeadline(time.Time) error { return nil }

type dummyAddr string
func (a dummyAddr) Network() string { return "test" }
func (a dummyAddr) String() string  { return string(a) }

func TestWriteFullHandlesShortWrites(t *testing.T) {
	conn := &partialConn{max: 3}
	want := []byte("0123456789")
	if err := WriteFull(conn, want); err != nil { t.Fatal(err) }
	if got := conn.buf.Bytes(); !bytes.Equal(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestTCPDialListen(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	listener, err := ListenTCP(ctx, "tcp", "127.0.0.1:0", nil)
	if err != nil { t.Fatal(err) }
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil { acceptErr <- err; return }
		accepted <- conn
	}()
	client, err := DialTCP(ctx, "tcp", listener.Addr().String(), nil)
	if err != nil { t.Fatal(err) }
	defer client.Close()
	var server net.Conn
	select {
	case server = <-accepted:
		defer server.Close()
	case err := <-acceptErr:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := WriteFull(client, []byte("ping")); err != nil { t.Fatal(err) }
	buf := make([]byte, 4)
	if err := ReadFull(server, buf); err != nil { t.Fatal(err) }
	if string(buf) != "ping" { t.Fatalf("got %q", buf) }
}

func TestTLSAdaptersReturnTLSConnections(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	if TLSClient(left, &tls.Config{InsecureSkipVerify: true}) == nil {
		t.Fatal("nil TLS client adapter")
	}
	if TLSServer(right, &tls.Config{}) == nil {
		t.Fatal("nil TLS server adapter")
	}
}
