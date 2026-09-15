package server

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/majiddarvishan/go-smpp/session"
)

func TestServerConcurrentClose(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil { t.Fatal(err) }
	server := New(listener, session.Config{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(ctx) }()

	conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil { t.Fatal(err) }
	defer conn.Close()
	deadline := time.Now().Add(time.Second)
	for len(server.Sessions()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(server.Sessions()) != 1 { t.Fatalf("sessions=%d", len(server.Sessions())) }

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = server.Close() }()
	}
	wg.Wait()
	select {
	case err := <-serveDone:
		if err != nil { t.Fatal(err) }
	case <-time.After(time.Second):
		t.Fatal("Serve did not stop")
	}
}
