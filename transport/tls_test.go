package transport

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"math/big"
	"net"
	"testing"
	"time"
)

func TestDialTLSAndListenTLS(t *testing.T) {
	certificate := testCertificate(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	listener, err := ListenTLS(ctx, "tcp", "127.0.0.1:0", nil, &tls.Config{Certificates: []tls.Certificate{certificate}})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	serverDone := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer conn.Close()
		buf := make([]byte, 4)
		if err := ReadFull(conn, buf); err != nil {
			serverDone <- err
			return
		}
		if string(buf) != "ping" {
			serverDone <- ErrNoProgress
			return
		}
		serverDone <- WriteFull(conn, []byte("pong"))
	}()

	client, err := DialTLS(ctx, "tcp", listener.Addr().String(), nil, &tls.Config{InsecureSkipVerify: true}) // test-only self-signed certificate
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := WriteFull(client, []byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	if err := ReadFull(client, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "pong" {
		t.Fatalf("got %q want pong", buf)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}

func testCertificate(t *testing.T) tls.Certificate {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: privateKey}
}
