package client

import (
	"net"
	"sync"
	"testing"

	"github.com/majiddarvishan/go-smpp/session"
)

func TestClientConcurrentClose(t *testing.T) {
	left, right := net.Pipe()
	defer right.Close()
	client, err := New(left, session.Config{})
	if err != nil { t.Fatal(err) }
	if client.Session().Role() != session.RoleESME { t.Fatalf("role=%s", client.Session().Role()) }
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = client.Close() }()
	}
	wg.Wait()
	client.Session().Wait()
}
