package perflab

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/majiddarvishan/go-smpp/session"
)

func TestPhase17ReferenceAcceptance(t *testing.T) {
	if os.Getenv("SMPP_ACCEPTANCE") != "1" {
		t.Skip("set SMPP_ACCEPTANCE=1 to run the sustained Phase 17 reference test")
	}

	duration := acceptanceDuration("SMPP_ACCEPTANCE_DURATION", 60*time.Second)
	sessions := acceptanceInt("SMPP_ACCEPTANCE_SESSION_COUNT", 1)
	callers := acceptanceInt("SMPP_ACCEPTANCE_CALLERS", 128)
	minRPS := acceptanceInt("SMPP_ACCEPTANCE_MIN_RPS", 100000)
	maxHeapMB := acceptanceInt("SMPP_ACCEPTANCE_MAX_HEAP_MB", 1024)
	maxRetainedMB := acceptanceInt("SMPP_ACCEPTANCE_MAX_RETAINED_MB", 64)
	if duration <= 0 || sessions <= 0 || callers <= 0 || minRPS <= 0 {
		t.Fatal("invalid acceptance configuration")
	}

	pairs := make([]*sessionPair, sessions)
	for i := range pairs {
		pairs[i] = newTCPPair(t, false)
	}
	defer func() {
		for _, pair := range pairs {
			pair.close()
		}
	}()

	type trafficBaseline struct {
		requestsSent      uint64
		responsesReceived uint64
	}
	baselines := make([][2]trafficBaseline, sessions)
	for i, pair := range pairs {
		e := pair.esme.Metrics()
		s := pair.smsc.Metrics()
		baselines[i][0] = trafficBaseline{requestsSent: e.RequestsSent, responsesReceived: e.ResponsesReceived}
		baselines[i][1] = trafficBaseline{requestsSent: s.RequestsSent, responsesReceived: s.ResponsesReceived}
	}

	runtime.GC()
	var startMem runtime.MemStats
	runtime.ReadMemStats(&startMem)
	baselineGoroutines := runtime.NumGoroutine()

	stop := make(chan struct{})
	workerCtx, cancelWorkers := context.WithTimeout(context.Background(), duration+15*time.Second)
	defer cancelWorkers()

	var (
		completed atomic.Uint64
		firstErr  atomic.Value // error
		wg        sync.WaitGroup
	)
	wg.Add(callers)
	for worker := 0; worker < callers; worker++ {
		worker := worker
		go func() {
			defer wg.Done()
			pair := pairs[worker%len(pairs)]
			for {
				select {
				case <-stop:
					return
				default:
				}
				var err error
				if worker&1 == 0 {
					_, err = pair.esme.SubmitSM(workerCtx, benchmarkSubmit)
				} else {
					_, err = pair.smsc.DeliverSM(workerCtx, benchmarkDeliver)
				}
				if err != nil {
					if workerCtx.Err() == nil {
						firstErr.CompareAndSwap(nil, err)
					}
					return
				}
				completed.Add(1)
			}
		}()
	}

	var (
		maxGoroutines = baselineGoroutines
		maxHeapAlloc  = startMem.HeapAlloc
		maxPending    int
		maxWindow     uint32
	)
	ticker := time.NewTicker(250 * time.Millisecond)
	deadline := time.NewTimer(duration)
	start := time.Now()
monitor:
	for {
		select {
		case <-ticker.C:
			g := runtime.NumGoroutine()
			if g > maxGoroutines {
				maxGoroutines = g
			}
			var mem runtime.MemStats
			runtime.ReadMemStats(&mem)
			if mem.HeapAlloc > maxHeapAlloc {
				maxHeapAlloc = mem.HeapAlloc
			}
			for _, pair := range pairs {
				for _, s := range []*session.Session{pair.esme, pair.smsc} {
					if pending := s.Pending(); pending > maxPending {
						maxPending = pending
					}
					if inUse := s.Window().InUse; inUse > maxWindow {
						maxWindow = inUse
					}
				}
			}
		case <-deadline.C:
			break monitor
		}
	}
	ticker.Stop()
	measured := time.Since(start)
	completedAtStop := completed.Load()
	close(stop)
	wg.Wait()
	if err, _ := firstErr.Load().(error); err != nil {
		t.Fatalf("traffic error: %v", err)
	}

	var requestDelta, responseDelta uint64
	for i, pair := range pairs {
		e := pair.esme.Metrics()
		s := pair.smsc.Metrics()
		requestDelta += e.RequestsSent - baselines[i][0].requestsSent
		requestDelta += s.RequestsSent - baselines[i][1].requestsSent
		responseDelta += e.ResponsesReceived - baselines[i][0].responsesReceived
		responseDelta += s.ResponsesReceived - baselines[i][1].responsesReceived
	}
	if requestDelta != responseDelta {
		t.Fatalf("required response processing mismatch: requests=%d responses=%d", requestDelta, responseDelta)
	}

	rps := float64(completedAtStop) / measured.Seconds()
	runtime.GC()
	var endMem runtime.MemStats
	runtime.ReadMemStats(&endMem)
	retainedGrowth := uint64(0)
	if endMem.HeapAlloc > startMem.HeapAlloc {
		retainedGrowth = endMem.HeapAlloc - startMem.HeapAlloc
	}

	allowedGoroutines := baselineGoroutines + callers + 32
	if maxGoroutines > allowedGoroutines {
		t.Fatalf("goroutine growth exceeded fixed-worker bound: baseline=%d max=%d allowed=%d", baselineGoroutines, maxGoroutines, allowedGoroutines)
	}
	if maxHeapAlloc > uint64(maxHeapMB)<<20 {
		t.Fatalf("peak heap exceeded bound: peak=%d MiB limit=%d MiB", maxHeapAlloc>>20, maxHeapMB)
	}
	if retainedGrowth > uint64(maxRetainedMB)<<20 {
		t.Fatalf("retained heap growth exceeded bound after GC: growth=%d MiB limit=%d MiB", retainedGrowth>>20, maxRetainedMB)
	}
	if maxWindow > benchmarkWindow || maxPending > benchmarkWindow {
		t.Fatalf("outstanding work exceeded configured bound: pending=%d window=%d limit=%d", maxPending, maxWindow, benchmarkWindow)
	}

	t.Logf("PHASE17_RESULT sessions=%d callers=%d duration=%s request_pdu_s=%.0f completed=%d requests=%d responses=%d goroutines_baseline=%d goroutines_max=%d heap_peak_mib=%d heap_retained_growth_mib=%d pending_max=%d window_max=%d",
		sessions, callers, measured.Round(time.Millisecond), rps, completedAtStop, requestDelta, responseDelta,
		baselineGoroutines, maxGoroutines, maxHeapAlloc>>20, retainedGrowth>>20, maxPending, maxWindow)

	if rps < float64(minRPS) {
		t.Fatalf("throughput %.0f request-PDU/s below required %d", rps, minRPS)
	}
}

func acceptanceInt(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func acceptanceDuration(name string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func TestAcceptanceConfigParsers(t *testing.T) {
	t.Setenv("SMPP_ACCEPTANCE_TEST_INT", "17")
	if got := acceptanceInt("SMPP_ACCEPTANCE_TEST_INT", 3); got != 17 {
		t.Fatalf("acceptanceInt=%d", got)
	}
	t.Setenv("SMPP_ACCEPTANCE_TEST_DURATION", "250ms")
	if got := acceptanceDuration("SMPP_ACCEPTANCE_TEST_DURATION", time.Second); got != 250*time.Millisecond {
		t.Fatalf("acceptanceDuration=%s", got)
	}
}
