package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// --- Scenario Definitions ---

type TestPhase struct {
	Name            string
	Description     string
	DriverCount     int
	PingRatePerSec  int // total pings/s across all drivers
	MatchRatePerSec int
	DurationSec     int
}

var phases = []TestPhase{
	{
		Name:            "warmup",
		Description:     "Light warmup to prime connections and JIT",
		DriverCount:     50,
		PingRatePerSec:  100,
		MatchRatePerSec: 5,
		DurationSec:     5,
	},
	{
		Name:            "load_test",
		Description:     "Sustained normal production load",
		DriverCount:     200,
		PingRatePerSec:  500,
		MatchRatePerSec: 20,
		DurationSec:     10,
	},
	{
		Name:            "stress_test",
		Description:     "High concurrency beyond expected capacity",
		DriverCount:     500,
		PingRatePerSec:  2000,
		MatchRatePerSec: 80,
		DurationSec:     10,
	},
	{
		Name:            "spike_test",
		Description:     "Sudden burst from low to extreme traffic",
		DriverCount:     1000,
		PingRatePerSec:  5000,
		MatchRatePerSec: 200,
		DurationSec:     8,
	},
	{
		Name:            "volume_test",
		Description:     "Maximum volume throughput saturation test",
		DriverCount:     2000,
		PingRatePerSec:  10000,
		MatchRatePerSec: 500,
		DurationSec:     10,
	},
	{
		Name:            "soak_test",
		Description:     "Extended duration sustained load for leak detection",
		DriverCount:     300,
		PingRatePerSec:  1000,
		MatchRatePerSec: 40,
		DurationSec:     30,
	},
}

// --- Latency Collector ---

type LatencyCollector struct {
	mu      sync.Mutex
	samples []float64
}

func (lc *LatencyCollector) Record(us float64) {
	lc.mu.Lock()
	lc.samples = append(lc.samples, us)
	lc.mu.Unlock()
}

type LatencyStats struct {
	Count  int64
	MinUs  float64
	MeanUs float64
	P50Us  float64
	P90Us  float64
	P95Us  float64
	P99Us  float64
	MaxUs  float64
}

func (lc *LatencyCollector) Stats() LatencyStats {
	lc.mu.Lock()
	s := make([]float64, len(lc.samples))
	copy(s, lc.samples)
	lc.mu.Unlock()

	if len(s) == 0 {
		return LatencyStats{}
	}

	sort.Float64s(s)
	n := len(s)
	sum := 0.0
	for _, v := range s {
		sum += v
	}

	pct := func(p float64) float64 {
		idx := int(math.Ceil(float64(n)*p)) - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= n {
			idx = n - 1
		}
		return s[idx]
	}

	return LatencyStats{
		Count:  int64(n),
		MinUs:  s[0],
		MeanUs: sum / float64(n),
		P50Us:  pct(0.50),
		P90Us:  pct(0.90),
		P95Us:  pct(0.95),
		P99Us:  pct(0.99),
		MaxUs:  s[n-1],
	}
}

// --- Phase Result ---

type PhaseResult struct {
	Phase           TestPhase
	PingLatency     LatencyStats
	MatchLatency    LatencyStats
	TotalPings      int64
	TotalMatches    int64
	FailedPings     int64
	FailedMatches   int64
	PingThroughput  float64 // ops/sec achieved
	MatchThroughput float64
	StartTime       time.Time
	EndTime         time.Time
	WallDuration    time.Duration
}

// --- Bounds ---

const (
	minLat = 23.7000
	maxLat = 23.8500
	minLng = 90.3500
	maxLng = 90.4500
)

func main() {
	gatewayURL := flag.String("gateway", "http://127.0.0.1:8080", "Gateway base URL")
	dispatchURL := flag.String("dispatch", "http://127.0.0.1:8081", "Dispatch base URL")
	outputFile := flag.String("output", "STRESS_TEST_RESULTS.md", "Output markdown file")
	seed := flag.Int64("seed", 42, "Random seed")
	flag.Parse()

	rnd := rand.New(rand.NewSource(*seed))

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        5000,
			MaxIdleConnsPerHost: 3000,
			MaxConnsPerHost:     3000,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	fmt.Println("======================================================================")
	fmt.Println("  COMPREHENSIVE STRESS & PERFORMANCE TESTING SUITE")
	fmt.Println("======================================================================")
	fmt.Printf("Gateway:  %s\n", *gatewayURL)
	fmt.Printf("Dispatch: %s\n", *dispatchURL)
	fmt.Printf("Phases:   %d\n", len(phases))
	fmt.Println("----------------------------------------------------------------------")

	var results []PhaseResult

	for i, phase := range phases {
		fmt.Printf("\n[%d/%d] Running: %s (%s)\n", i+1, len(phases), phase.Name, phase.Description)
		fmt.Printf("       Drivers=%d  PingRate=%d/s  MatchRate=%d/s  Duration=%ds\n",
			phase.DriverCount, phase.PingRatePerSec, phase.MatchRatePerSec, phase.DurationSec)

		result := runPhase(phase, *gatewayURL, *dispatchURL, client, rnd)
		results = append(results, result)

		ps := result.PingLatency
		ms := result.MatchLatency
		fmt.Printf("       Ping:  p50=%.2fms p95=%.2fms p99=%.2fms max=%.2fms (ok=%d fail=%d  %.0f ops/s)\n",
			ps.P50Us/1000, ps.P95Us/1000, ps.P99Us/1000, ps.MaxUs/1000,
			result.TotalPings, result.FailedPings, result.PingThroughput)
		fmt.Printf("       Match: p50=%.2fms p95=%.2fms p99=%.2fms max=%.2fms (ok=%d fail=%d  %.0f ops/s)\n",
			ms.P50Us/1000, ms.P95Us/1000, ms.P99Us/1000, ms.MaxUs/1000,
			result.TotalMatches, result.FailedMatches, result.MatchThroughput)

		// Brief cooldown between phases
		if i < len(phases)-1 {
			fmt.Print("       Cooldown 2s...")
			time.Sleep(2 * time.Second)
			fmt.Println(" OK")
		}
	}

	// Write results to markdown
	writeReport(*outputFile, results, *gatewayURL, *dispatchURL, *seed)

	fmt.Println("\n======================================================================")
	fmt.Printf("  ALL %d PHASES COMPLETED - Results saved to %s\n", len(phases), *outputFile)
	fmt.Println("======================================================================")
}

func runPhase(phase TestPhase, gatewayURL, dispatchURL string, client *http.Client, rnd *rand.Rand) PhaseResult {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(phase.DurationSec+5)*time.Second)
	defer cancel()

	pingLC := &LatencyCollector{}
	matchLC := &LatencyCollector{}
	var totalPings, totalMatches, failedPings, failedMatches int64

	// Pre-generate driver IDs and initial positions
	type driver struct {
		id  string
		lat float64
		lng float64
	}
	drivers := make([]driver, phase.DriverCount)
	for i := range drivers {
		drivers[i] = driver{
			id:  fmt.Sprintf("stress_drv_%05d", i),
			lat: minLat + rnd.Float64()*(maxLat-minLat),
			lng: minLng + rnd.Float64()*(maxLng-minLng),
		}
	}

	var wg sync.WaitGroup
	startTime := time.Now()
	deadline := startTime.Add(time.Duration(phase.DurationSec) * time.Second)

	// --- PING WORKERS ---
	if phase.PingRatePerSec > 0 {
		// Split across multiple goroutines for high throughput
		numPingWorkers := phase.PingRatePerSec / 100
		if numPingWorkers < 1 {
			numPingWorkers = 1
		}
		if numPingWorkers > 200 {
			numPingWorkers = 200
		}
		perWorkerRate := phase.PingRatePerSec / numPingWorkers

		for w := 0; w < numPingWorkers; w++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()
				interval := time.Second / time.Duration(perWorkerRate)
				ticker := time.NewTicker(interval)
				defer ticker.Stop()
				localRnd := rand.New(rand.NewSource(rnd.Int63()))

				for {
					select {
					case <-ctx.Done():
						return
					case t := <-ticker.C:
						if t.After(deadline) {
							return
						}
						d := drivers[localRnd.Intn(len(drivers))]
						lat := d.lat + (localRnd.Float64()-0.5)*0.0003
						lng := d.lng + (localRnd.Float64()-0.5)*0.0003

						payload, _ := json.Marshal(map[string]interface{}{
							"driver_id": d.id,
							"lat":       lat,
							"lng":       lng,
							"timestamp": time.Now().UnixMilli(),
							"status":    "available",
						})

						start := time.Now()
						resp, err := client.Post(gatewayURL+"/ping", "application/json", bytes.NewReader(payload))
						elapsedUs := float64(time.Since(start).Microseconds())

						if err != nil || (resp != nil && resp.StatusCode >= 400) {
							atomic.AddInt64(&failedPings, 1)
						} else {
							atomic.AddInt64(&totalPings, 1)
							pingLC.Record(elapsedUs)
						}
						if resp != nil {
							resp.Body.Close()
						}
					}
				}
			}(w)
		}
	}

	// --- MATCH WORKERS ---
	if phase.MatchRatePerSec > 0 {
		numMatchWorkers := phase.MatchRatePerSec / 10
		if numMatchWorkers < 1 {
			numMatchWorkers = 1
		}
		if numMatchWorkers > 100 {
			numMatchWorkers = 100
		}
		perWorkerRate := phase.MatchRatePerSec / numMatchWorkers

		for w := 0; w < numMatchWorkers; w++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()
				interval := time.Second / time.Duration(perWorkerRate)
				ticker := time.NewTicker(interval)
				defer ticker.Stop()
				localRnd := rand.New(rand.NewSource(rnd.Int63()))
				reqCounter := int64(0)

				for {
					select {
					case <-ctx.Done():
						return
					case t := <-ticker.C:
						if t.After(deadline) {
							return
						}
						reqCounter++
						rLat := minLat + localRnd.Float64()*(maxLat-minLat)
						rLng := minLng + localRnd.Float64()*(maxLng-minLng)

						payload, _ := json.Marshal(map[string]interface{}{
							"ride_id":     fmt.Sprintf("stress_ride_%d_%d_%d", workerID, reqCounter, time.Now().UnixNano()),
							"rider_id":    fmt.Sprintf("stress_rider_%d_%d", workerID, reqCounter),
							"pickup_lat":  rLat,
							"pickup_lng":  rLng,
							"dropoff_lat": rLat + 0.01,
							"dropoff_lng": rLng + 0.01,
						})

						start := time.Now()
						resp, err := client.Post(dispatchURL+"/match", "application/json", bytes.NewReader(payload))
						elapsedUs := float64(time.Since(start).Microseconds())

						if err != nil || (resp != nil && resp.StatusCode >= 500) {
							atomic.AddInt64(&failedMatches, 1)
						} else {
							atomic.AddInt64(&totalMatches, 1)
							matchLC.Record(elapsedUs)
						}
						if resp != nil {
							resp.Body.Close()
						}
					}
				}
			}(w)
		}
	}

	wg.Wait()
	endTime := time.Now()
	wallDur := endTime.Sub(startTime)

	pingThroughput := 0.0
	if wallDur.Seconds() > 0 {
		pingThroughput = float64(totalPings) / wallDur.Seconds()
	}
	matchThroughput := 0.0
	if wallDur.Seconds() > 0 {
		matchThroughput = float64(totalMatches) / wallDur.Seconds()
	}

	return PhaseResult{
		Phase:           phase,
		PingLatency:     pingLC.Stats(),
		MatchLatency:    matchLC.Stats(),
		TotalPings:      totalPings,
		TotalMatches:    totalMatches,
		FailedPings:     failedPings,
		FailedMatches:   failedMatches,
		PingThroughput:  pingThroughput,
		MatchThroughput: matchThroughput,
		StartTime:       startTime,
		EndTime:         endTime,
		WallDuration:    wallDur,
	}
}

func writeReport(path string, results []PhaseResult, gatewayURL, dispatchURL string, seed int64) {
	absPath, _ := filepath.Abs(path)
	hostname, _ := os.Hostname()

	var buf bytes.Buffer
	w := func(format string, args ...interface{}) {
		fmt.Fprintf(&buf, format, args...)
	}

	w("# Stress & Performance Test Results\n\n")
	w("**Generated**: %s (UTC)  \n", time.Now().UTC().Format(time.RFC3339))
	w("**Machine**: %s (%s/%s, %d CPUs)  \n", hostname, runtime.GOOS, runtime.GOARCH, runtime.NumCPU())
	w("**Go Version**: %s  \n", runtime.Version())
	w("**Random Seed**: %d  \n", seed)
	w("**Gateway Target**: `%s`  \n", gatewayURL)
	w("**Dispatch Target**: `%s`  \n\n", dispatchURL)

	w("---\n\n")

	// Summary Table
	w("## Executive Summary\n\n")
	w("| Test Phase | Ping p50 (ms) | Ping p95 (ms) | Ping p99 (ms) | Ping Max (ms) | Match p50 (ms) | Match p95 (ms) | Match p99 (ms) | Match Max (ms) | Ping Ops/s | Match Ops/s | Ping Fail %% | Match Fail %% |\n")
	w("| :--- | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: |\n")

	for _, r := range results {
		ps := r.PingLatency
		ms := r.MatchLatency
		pingFailPct := 0.0
		if r.TotalPings+r.FailedPings > 0 {
			pingFailPct = float64(r.FailedPings) / float64(r.TotalPings+r.FailedPings) * 100
		}
		matchFailPct := 0.0
		if r.TotalMatches+r.FailedMatches > 0 {
			matchFailPct = float64(r.FailedMatches) / float64(r.TotalMatches+r.FailedMatches) * 100
		}
		w("| **%s** | %.2f | %.2f | %.2f | %.2f | %.2f | %.2f | %.2f | %.2f | %.0f | %.0f | %.1f%% | %.1f%% |\n",
			r.Phase.Name,
			ps.P50Us/1000, ps.P95Us/1000, ps.P99Us/1000, ps.MaxUs/1000,
			ms.P50Us/1000, ms.P95Us/1000, ms.P99Us/1000, ms.MaxUs/1000,
			r.PingThroughput, r.MatchThroughput,
			pingFailPct, matchFailPct)
	}

	w("\n---\n\n")

	// Detailed per-phase reports
	w("## Detailed Phase Reports\n\n")

	for i, r := range results {
		ps := r.PingLatency
		ms := r.MatchLatency

		w("### %d. %s\n\n", i+1, r.Phase.Name)
		w("> %s\n\n", r.Phase.Description)

		w("**Configuration**:\n")
		w("- Concurrent Drivers: `%d`\n", r.Phase.DriverCount)
		w("- Target Ping Rate: `%d` ops/s\n", r.Phase.PingRatePerSec)
		w("- Target Match Rate: `%d` ops/s\n", r.Phase.MatchRatePerSec)
		w("- Duration: `%d` seconds\n", r.Phase.DurationSec)
		w("- Wall Clock: `%v`\n\n", r.WallDuration.Round(time.Millisecond))

		// Ping table
		w("#### Ping Write Latency (Gateway → Redis Hot Path)\n\n")
		w("| Metric | Value |\n")
		w("| :--- | :--- |\n")
		w("| Successful Requests | `%d` |\n", r.TotalPings)
		w("| Failed Requests | `%d` |\n", r.FailedPings)
		w("| Achieved Throughput | `%.0f` ops/s |\n", r.PingThroughput)
		w("| Min Latency | `%.2f` ms |\n", ps.MinUs/1000)
		w("| Mean Latency | `%.2f` ms |\n", ps.MeanUs/1000)
		w("| **p50 (Median)** | **`%.2f` ms** |\n", ps.P50Us/1000)
		w("| p90 | `%.2f` ms |\n", ps.P90Us/1000)
		w("| **p95** | **`%.2f` ms** |\n", ps.P95Us/1000)
		w("| **p99** | **`%.2f` ms** |\n", ps.P99Us/1000)
		w("| **Max Latency** | **`%.2f` ms** |\n\n", ps.MaxUs/1000)

		// Match table
		w("#### Match Dispatch Latency (K-Ring Search → Claim → Postgres)\n\n")
		w("| Metric | Value |\n")
		w("| :--- | :--- |\n")
		w("| Successful Requests | `%d` |\n", r.TotalMatches)
		w("| Failed Requests | `%d` |\n", r.FailedMatches)
		w("| Achieved Throughput | `%.0f` ops/s |\n", r.MatchThroughput)
		w("| Min Latency | `%.2f` ms |\n", ms.MinUs/1000)
		w("| Mean Latency | `%.2f` ms |\n", ms.MeanUs/1000)
		w("| **p50 (Median)** | **`%.2f` ms** |\n", ms.P50Us/1000)
		w("| p90 | `%.2f` ms |\n", ms.P90Us/1000)
		w("| **p95** | **`%.2f` ms** |\n", ms.P95Us/1000)
		w("| **p99** | **`%.2f` ms** |\n", ms.P99Us/1000)
		w("| **Max Latency** | **`%.2f` ms** |\n\n", ms.MaxUs/1000)

		w("---\n\n")
	}

	// Scalability analysis
	w("## Scalability Analysis\n\n")
	w("| Metric | Warmup | Load | Stress | Spike | Volume | Soak |\n")
	w("| :--- | :---: | :---: | :---: | :---: | :---: | :---: |\n")

	w("| Target Ping Rate (ops/s) |")
	for _, r := range results {
		w(" %d |", r.Phase.PingRatePerSec)
	}
	w("\n")

	w("| Achieved Ping Rate (ops/s) |")
	for _, r := range results {
		w(" %.0f |", r.PingThroughput)
	}
	w("\n")

	w("| Ping p99 (ms) |")
	for _, r := range results {
		w(" %.2f |", r.PingLatency.P99Us/1000)
	}
	w("\n")

	w("| Match p99 (ms) |")
	for _, r := range results {
		w(" %.2f |", r.MatchLatency.P99Us/1000)
	}
	w("\n")

	w("| Total Pings |")
	for _, r := range results {
		w(" %d |", r.TotalPings)
	}
	w("\n")

	w("| Ping Failure Rate |")
	for _, r := range results {
		failPct := 0.0
		if r.TotalPings+r.FailedPings > 0 {
			failPct = float64(r.FailedPings) / float64(r.TotalPings+r.FailedPings) * 100
		}
		w(" %.1f%% |", failPct)
	}
	w("\n\n")

	_ = os.WriteFile(absPath, buf.Bytes(), 0644)
	fmt.Printf("Report written to: %s\n", absPath)
}
