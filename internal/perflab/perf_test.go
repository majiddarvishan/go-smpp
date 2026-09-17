package perflab

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"math/big"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/session"
	"github.com/majiddarvishan/go-smpp/transport"
)

const benchmarkWindow = 4096

type sessionPair struct {
	esme *session.Session
	smsc *session.Session
}

func (p *sessionPair) close() {
	_ = p.esme.Close()
	_ = p.smsc.Close()
	p.esme.Wait()
	p.smsc.Wait()
}

func benchmarkSessionConfig(role session.Role, handler session.Handler) session.Config {
	return session.Config{
		Role:                role,
		Profile:             protocol.SMPP34Profile(),
		WindowSize:          benchmarkWindow,
		MaxPending:          benchmarkWindow,
		TXQueueSize:         benchmarkWindow,
		ReadBufferSize:      256 << 10,
		ResponseTimeout:     30 * time.Second,
		SessionInitTimeout:  30 * time.Second,
		EnquireLinkInterval: -1,
		EnquireLinkTimeout:  -1,
		InactivityTimeout:   -1,
		Handler:             handler,
	}
}

func smscHandler(_ context.Context, _ *session.Session, pdu session.InboundPDU) (session.Response, error) {
	switch pdu.Header.CommandID {
	case protocol.CommandBindTransceiver:
		return session.Response{Status: protocol.StatusOK, Body: protocol.BindResponse{SystemID: []byte("perflab")}}, nil
	case protocol.CommandSubmitSM:
		return session.Response{Status: protocol.StatusOK, Body: protocol.SubmitSMResp{MessageID: []byte("1")}}, nil
	default:
		return session.Response{Status: protocol.StatusSystemError, Body: protocol.EmptyBody{}}, nil
	}
}

func esmeHandler(_ context.Context, _ *session.Session, pdu session.InboundPDU) (session.Response, error) {
	if pdu.Header.CommandID == protocol.CommandDeliverSM {
		return session.Response{Status: protocol.StatusOK, Body: protocol.DeliverSMResp{}}, nil
	}
	return session.Response{Status: protocol.StatusSystemError, Body: protocol.EmptyBody{}}, nil
}

func newPipePair(tb testing.TB) *sessionPair {
	tb.Helper()
	esmeConn, smscConn := net.Pipe()
	return newPairFromConns(tb, esmeConn, smscConn)
}

func newPairFromConns(tb testing.TB, esmeConn, smscConn net.Conn) *sessionPair {
	tb.Helper()
	smsc, err := session.New(smscConn, benchmarkSessionConfig(session.RoleSMSC, session.HandlerFunc(smscHandler)))
	if err != nil {
		_ = esmeConn.Close()
		_ = smscConn.Close()
		tb.Fatal(err)
	}
	esme, err := session.New(esmeConn, benchmarkSessionConfig(session.RoleESME, session.HandlerFunc(esmeHandler)))
	if err != nil {
		_ = smsc.Close()
		tb.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := esme.BindTransceiver(ctx, protocol.BindRequest{SystemID: []byte("perflab"), InterfaceVersion: protocol.InterfaceVersion34}); err != nil {
		_ = esme.Close()
		_ = smsc.Close()
		tb.Fatal(err)
	}
	return &sessionPair{esme: esme, smsc: smsc}
}

func newTCPPair(tb testing.TB, useTLS bool) *sessionPair {
	tb.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatal(err)
	}
	defer listener.Close()
	if useTLS {
		listener = tls.NewListener(listener, &tls.Config{Certificates: []tls.Certificate{benchmarkCertificate(tb)}, MinVersion: tls.VersionTLS12})
	}

	accepted := make(chan net.Conn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			acceptErr <- err
			return
		}
		if tlsConn, ok := conn.(*tls.Conn); ok {
			if err := tlsConn.HandshakeContext(ctx); err != nil {
				_ = conn.Close()
				acceptErr <- err
				return
			}
		}
		accepted <- conn
	}()

	var esmeConn net.Conn
	if useTLS {
		esmeConn, err = transport.DialTLS(ctx, "tcp", listener.Addr().String(), nil, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}) // benchmark-only self-signed cert
	} else {
		esmeConn, err = transport.DialTCP(ctx, "tcp", listener.Addr().String(), nil)
	}
	if err != nil {
		tb.Fatal(err)
	}

	var smscConn net.Conn
	select {
	case smscConn = <-accepted:
	case err := <-acceptErr:
		_ = esmeConn.Close()
		tb.Fatal(err)
	case <-ctx.Done():
		_ = esmeConn.Close()
		tb.Fatal(ctx.Err())
	}
	return newPairFromConns(tb, esmeConn, smscConn)
}

func benchmarkCertificate(tb testing.TB) tls.Certificate {
	tb.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		tb.Fatal(err)
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
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		tb.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

var benchmarkSubmit = protocol.SubmitSM{
	SourceAddrTON:   protocol.TONInternational,
	SourceAddrNPI:   protocol.NPIISDN,
	SourceAddr:      []byte("12025550100"),
	DestAddrTON:     protocol.TONInternational,
	DestAddrNPI:     protocol.NPIISDN,
	DestinationAddr: []byte("12025550101"),
	DataCoding:      protocol.DataCodingSMSCDefault,
	ShortMessage:    []byte("phase16-throughput"),
}

var benchmarkDeliver = protocol.DeliverSM{
	SourceAddrTON:   protocol.TONInternational,
	SourceAddrNPI:   protocol.NPIISDN,
	SourceAddr:      []byte("12025550101"),
	DestAddrTON:     protocol.TONInternational,
	DestAddrNPI:     protocol.NPIISDN,
	DestinationAddr: []byte("12025550100"),
	DataCoding:      protocol.DataCodingSMSCDefault,
	ShortMessage:    []byte("phase16-delivery"),
}

func BenchmarkInMemorySessionBidirectional(b *testing.B) {
	benchmarkBidirectional(b, false, false)
}

func BenchmarkLocalhostTCPBidirectional(b *testing.B) {
	benchmarkBidirectional(b, true, false)
}

func BenchmarkLocalhostTLSBidirectional(b *testing.B) {
	benchmarkBidirectional(b, true, true)
}

func benchmarkBidirectional(b *testing.B, localhost, useTLS bool) {
	for _, count := range benchmarkSessionCounts() {
		b.Run(fmt.Sprintf("sessions_%d", count), func(b *testing.B) {
			pairs := make([]*sessionPair, count)
			for i := range pairs {
				if localhost {
					pairs[i] = newTCPPair(b, useTLS)
				} else {
					pairs[i] = newPipePair(b)
				}
				defer pairs[i].close()
			}

			var next atomic.Uint64
			errCh := make(chan error, 1)
			parallelism := benchmarkParallelism()
			b.SetParallelism(parallelism)
			b.ReportAllocs()
			b.ResetTimer()
			started := time.Now()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					index := next.Add(1) - 1
					pair := pairs[int(index/2)%len(pairs)]
					var err error
					if index&1 == 0 {
						_, err = pair.esme.SubmitSM(context.Background(), benchmarkSubmit)
					} else {
						_, err = pair.smsc.DeliverSM(context.Background(), benchmarkDeliver)
					}
					if err != nil {
						select {
						case errCh <- err:
						default:
						}
						return
					}
				}
			})
			elapsed := time.Since(started)
			b.StopTimer()
			select {
			case err := <-errCh:
				b.Fatal(err)
			default:
			}
			if elapsed > 0 {
				b.ReportMetric(float64(b.N)/elapsed.Seconds(), "request_pdu/s")
			}
			b.ReportMetric(float64(count), "sessions")
			b.ReportMetric(float64(parallelism*runtime.GOMAXPROCS(0)), "callers")
		})
	}
}

func benchmarkParallelism() int {
	raw := strings.TrimSpace(os.Getenv("SMPP_BENCH_PARALLELISM"))
	if raw == "" {
		return 16
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 16
	}
	return value
}

func benchmarkSessionCounts() []int {
	raw := strings.TrimSpace(os.Getenv("SMPP_BENCH_SESSIONS"))
	if raw == "" {
		return []int{1}
	}
	parts := strings.Split(raw, ",")
	counts := make([]int, 0, len(parts))
	seen := make(map[int]struct{}, len(parts))
	for _, part := range parts {
		value, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || value <= 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		counts = append(counts, value)
	}
	if len(counts) == 0 {
		return []int{1}
	}
	return counts
}
