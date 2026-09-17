package transport

import (
	"bytes"
	"context"
	"io"
	"net"
	"testing"
	"time"
)

func TestWriteBuffersTCPWritesAllBytes(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	accepted := make(chan *net.TCPConn, 1)
	errCh := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			errCh <- err
			return
		}
		tcp, ok := conn.(*net.TCPConn)
		if !ok {
			_ = conn.Close()
			errCh <- io.ErrUnexpectedEOF
			return
		}
		accepted <- tcp
	}()

	clientRaw, err := (&net.Dialer{}).DialContext(ctx, "tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	client := clientRaw.(*net.TCPConn)
	defer client.Close()

	var server *net.TCPConn
	select {
	case server = <-accepted:
	case err := <-errCh:
		t.Fatal(err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	defer server.Close()

	want := []byte("first-second-third")
	readDone := make(chan error, 1)
	go func() {
		got := make([]byte, len(want))
		_, err := io.ReadFull(server, got)
		if err == nil && !bytes.Equal(got, want) {
			err = io.ErrUnexpectedEOF
		}
		readDone <- err
	}()

	if err := WriteBuffers(client, net.Buffers{[]byte("first-"), []byte("second-"), []byte("third")}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-readDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
