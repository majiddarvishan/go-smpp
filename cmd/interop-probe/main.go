//go:build interop

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"time"

	"github.com/majiddarvishan/go-smpp/internal/interopprobe"
)

func main() {
	mode := flag.String("mode", envOr("SMPP_INTEROP_MODE", "all"), "probe mode: all, smsc, or esme")
	smscAddress := flag.String("smsc-address", envOr("SMPP_INTEROP_SMSC_ADDRESS", "127.0.0.1:3000"), "external SMPP 3.4 SMSC endpoint")
	listenAddress := flag.String("listen-address", envOr("SMPP_INTEROP_ESME_LISTEN_ADDRESS", "127.0.0.1:4000"), "local address for go-smpp SMSC/server mode")
	requestTimeout := flag.Duration("request-timeout", 8*time.Second, "per-operation timeout")
	peerWait := flag.Duration("peer-wait", 30*time.Second, "maximum wait for the ESME simulator to connect")
	dlrWait := flag.Duration("dlr-wait", 5*time.Second, "maximum wait for a deliver_sm after submit_sm")
	submitWait := flag.Duration("submit-wait", 15*time.Second, "maximum wait for an ESME-originated submit_sm")
	attemptESMEUnbindDefault, err := strconv.ParseBool(envOr("SMPP_INTEROP_ATTEMPT_ESME_UNBIND", "false"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "SMPP_INTEROP_ATTEMPT_ESME_UNBIND must be true or false")
		os.Exit(2)
	}
	attemptESMEUnbind := flag.Bool("attempt-esme-unbind", attemptESMEUnbindDefault, "attempt server-initiated Unbind in ESME mode (requires peer support)")
	flag.Parse()

	if *mode != "all" && *mode != "smsc" && *mode != "esme" {
		fmt.Fprintln(os.Stderr, "invalid -mode; use all, smsc, or esme")
		os.Exit(2)
	}
	needsSMSC := *mode == "all" || *mode == "smsc"
	needsESME := *mode == "all" || *mode == "esme"
	smscPassword := []byte(os.Getenv("SMPP_INTEROP_SMSC_PASSWORD"))
	esmePassword := []byte(os.Getenv("SMPP_INTEROP_ESME_PASSWORD"))
	if needsSMSC && len(smscPassword) == 0 {
		fmt.Fprintln(os.Stderr, "SMPP_INTEROP_SMSC_PASSWORD is required; run scripts/interop-local.sh for hidden prompts")
		os.Exit(2)
	}
	if needsESME && len(esmePassword) == 0 {
		fmt.Fprintln(os.Stderr, "SMPP_INTEROP_ESME_PASSWORD is required; run scripts/interop-local.sh for hidden prompts")
		os.Exit(2)
	}

	config := interopprobe.Config{
		Mode:               *mode,
		SMSCAddress:        *smscAddress,
		SMSCSystemID:       envOr("SMPP_INTEROP_SMSC_SYSTEM_ID", "client_0"),
		SMSCSystemType:     envOr("SMPP_INTEROP_SMSC_SYSTEM_TYPE", "smpp"),
		SMSCPassword:       smscPassword,
		ESMEListenAddress:  *listenAddress,
		ESMESystemID:       envOr("SMPP_INTEROP_ESME_SYSTEM_ID", "smpp_client_0"),
		ESMESystemType:     envOr("SMPP_INTEROP_ESME_SYSTEM_TYPE", "smpp"),
		ESMEPassword:       esmePassword,
		SourceAddress:      envOr("SMPP_INTEROP_SOURCE_ADDRESS", "1000"),
		DestinationAddress: envOr("SMPP_INTEROP_DESTINATION_ADDRESS", "2000"),
		Message:            envOr("SMPP_INTEROP_MESSAGE", "go-smpp interoperability probe"),
		RequestTimeout:     *requestTimeout,
		PeerWait:           *peerWait,
		DLRWait:            *dlrWait,
		SubmitWait:         *submitWait,
		AttemptESMEUnbind:  *attemptESMEUnbind,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	report := interopprobe.Run(ctx, config, os.Stdout)

	reportPath := os.Getenv("SMPP_INTEROP_REPORT_PATH")
	if reportPath == "" {
		reportPath = filepath.Join(os.TempDir(), fmt.Sprintf("go-smpp-interop-%d-report.json", time.Now().UnixNano()))
	}
	candidatePath := os.Getenv("SMPP_INTEROP_CANDIDATE_PATH")
	if candidatePath == "" {
		candidatePath = filepath.Join(filepath.Dir(reportPath), "manifest-candidate.json")
	}
	if err := writePrivateJSON(reportPath, report); err != nil {
		fmt.Fprintf(os.Stderr, "could not write sanitized report (%T)\n", err)
		os.Exit(1)
	}
	candidate := interopprobe.FixtureManifest{SchemaVersion: 1, Fixtures: report.Candidates}
	if err := writePrivateJSON(candidatePath, candidate); err != nil {
		fmt.Fprintf(os.Stderr, "could not write sanitized fixture candidate (%T)\n", err)
		os.Exit(1)
	}

	failures, blocked := 0, 0
	for _, result := range report.Results {
		fmt.Printf("RESULT %s %s frames=%d excluded_or_dropped=%d\n", result.Name, result.Status, result.FramesCaptured, result.FramesDropped)
		switch result.Status {
		case interopprobe.StatusFail:
			failures++
		case interopprobe.StatusBlocked:
			blocked++
		}
		for _, event := range result.Events {
			fmt.Printf("  %-28s %-12s", event.Name, event.Status)
			if len(event.Details) > 0 {
				encoded, _ := json.Marshal(event.Details)
				fmt.Printf(" %s", encoded)
			}
			fmt.Println()
		}
	}
	fmt.Printf("OVERALL failures=%d blocked=%d candidates=%d\n", failures, blocked, len(report.Candidates))
	fmt.Printf("REPORT_PATH=%s\nCANDIDATE_PATH=%s\n", reportPath, candidatePath)
	if failures > 0 {
		os.Exit(1)
	}
	if blocked > 0 {
		os.Exit(2)
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func writePrivateJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}
