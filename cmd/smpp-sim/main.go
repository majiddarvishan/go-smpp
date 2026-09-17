package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/majiddarvishan/go-smpp/protocol"
	"github.com/majiddarvishan/go-smpp/server"
	"github.com/majiddarvishan/go-smpp/session"
)

func main() {
	var (
		listenAddr    = flag.String("listen", "127.0.0.1:2775", "TCP listen address")
		systemID      = flag.String("system-id", "go-smpp-sim", "system_id returned in successful bind responses")
		maxSessions   = flag.Int("max-sessions", 0, "maximum concurrent SMPP sessions; 0 means unlimited")
		windowSize    = flag.Int("window", 4096, "outbound request window per session")
		txQueueSize   = flag.Int("tx-queue", 4096, "TX queue capacity per session")
		readBuffer    = flag.Int("read-buffer", 256<<10, "session TCP read buffer size")
		maxPDUSize    = flag.Uint("max-pdu", 1<<20, "maximum accepted PDU size in bytes")
		statsInterval = flag.Duration("stats-interval", time.Second, "periodic aggregate throughput report; <=0 disables")
		tlsCert       = flag.String("tls-cert", "", "PEM server certificate; requires -tls-key")
		tlsKey        = flag.String("tls-key", "", "PEM server private key; requires -tls-cert")
	)
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var tlsConfig *tls.Config
	if (*tlsCert == "") != (*tlsKey == "") {
		logger.Error("both -tls-cert and -tls-key must be supplied together")
		os.Exit(2)
	}
	if *tlsCert != "" {
		certificate, err := tls.LoadX509KeyPair(*tlsCert, *tlsKey)
		if err != nil {
			logger.Error("load TLS certificate", "error", err)
			os.Exit(2)
		}
		tlsConfig = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}
	}

	var acceptedSubmits atomic.Uint64
	config := server.Config{
		Network:     "tcp",
		Address:     *listenAddr,
		TLSConfig:   tlsConfig,
		MaxSessions: *maxSessions,
		SessionConfig: session.Config{
			Profile:             protocol.SMPP34Profile(),
			MaxPDUSize:          uint32(*maxPDUSize),
			MaxPending:          *windowSize,
			WindowSize:          *windowSize,
			TXQueueSize:         *txQueueSize,
			ReadBufferSize:      *readBuffer,
			ResponseTimeout:     30 * time.Second,
			SessionInitTimeout:  30 * time.Second,
			EnquireLinkInterval: -1,
			EnquireLinkTimeout:  -1,
			InactivityTimeout:   -1,
			Logger:              logger,
		},
		Authenticator: server.AuthenticatorFunc(func(_ context.Context, _ *session.Session, _ session.BindMode, _ protocol.BindRequest) (server.BindResult, error) {
			return server.BindResult{Status: protocol.StatusOK, SystemID: []byte(*systemID)}, nil
		}),
		SubmitHandler: server.SubmitHandlerFunc(func(_ context.Context, _ *session.Session, _ protocol.SubmitSM) (server.SubmitResult, error) {
			acceptedSubmits.Add(1)
			return server.SubmitResult{Status: protocol.StatusOK, MessageID: []byte("sim")}, nil
		}),
	}

	srv, err := server.Listen(ctx, config)
	if err != nil {
		logger.Error("listen", "error", err)
		os.Exit(1)
	}
	defer srv.Close()

	scheme := "tcp"
	if tlsConfig != nil {
		scheme = "tls"
	}
	logger.Info("SMPP simulator listening", "transport", scheme, "address", srv.Addr().String(), "window", *windowSize)

	statsDone := make(chan struct{})
	if *statsInterval > 0 {
		go reportStats(ctx, logger, srv, &acceptedSubmits, *statsInterval, statsDone)
	} else {
		close(statsDone)
	}

	if err := srv.Serve(ctx); err != nil {
		logger.Error("serve", "error", err)
		os.Exit(1)
	}
	<-statsDone
}

func reportStats(ctx context.Context, logger *slog.Logger, srv *server.Server, submits *atomic.Uint64, interval time.Duration, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var previousRequests uint64
	previousAt := time.Now()
	for {
		select {
		case now := <-ticker.C:
			var requests, responses, decodeFailures uint64
			sessions := srv.Sessions()
			for _, sess := range sessions {
				metrics := sess.Metrics()
				requests += metrics.RequestsReceived
				responses += metrics.ResponsesSent
				decodeFailures += metrics.DecodeFailures
			}
			elapsed := now.Sub(previousAt).Seconds()
			totalSubmits := submits.Load()
			rate := float64(totalSubmits-previousRequests) / elapsed
			logger.Info("SMPP simulator stats",
				"sessions", len(sessions),
				"active_session_requests_received", requests,
				"active_session_responses_sent", responses,
				"submit_sm_received_total", totalSubmits,
				"decode_failures", decodeFailures,
				"submit_sm_per_second", fmt.Sprintf("%.0f", rate))
			previousRequests = totalSubmits
			previousAt = now
		case <-ctx.Done():
			return
		}
	}
}
