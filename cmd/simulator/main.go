package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

type ScenarioConfig struct {
	Name                   string `yaml:"name"`
	City                   string `yaml:"city"`
	DriverCount            int    `yaml:"driver_count"`
	PingIntervalSeconds    int    `yaml:"ping_interval_seconds"`
	DurationSeconds        int    `yaml:"duration_seconds"`
	RiderRequestRatePerSec int    `yaml:"rider_request_rate_per_sec"`
	Seed                   int64  `yaml:"seed"`
	Bounds                 Bounds `yaml:"bounds"`
}

type Bounds struct {
	MinLat float64 `yaml:"min_lat"`
	MaxLat float64 `yaml:"max_lat"`
	MinLng float64 `yaml:"min_lng"`
	MaxLng float64 `yaml:"max_lng"`
}

type LatencySummary struct {
	Count int64   `json:"count"`
	MinMs float64 `json:"min_ms"`
	P50Ms float64 `json:"p50_ms"`
	P95Ms float64 `json:"p95_ms"`
	P99Ms float64 `json:"p99_ms"`
	MaxMs float64 `json:"max_ms"`
}

type BenchmarkResult struct {
	Date          string         `json:"date"`
	Scenario      string         `json:"scenario"`
	Design        string         `json:"design"`
	GoVersion     string         `json:"go_version"`
	Machine       string         `json:"machine"`
	Seed          int64          `json:"seed"`
	DriverCount   int            `json:"driver_count"`
	TotalPings    int64          `json:"total_pings"`
	TotalMatches  int64          `json:"total_matches"`
	PingLatency   LatencySummary `json:"ping_latency"`
	MatchLatency  LatencySummary `json:"match_latency"`
	FailedPings   int64          `json:"failed_pings"`
	FailedMatches int64          `json:"failed_matches"`
}

func computePercentiles(samples []float64) LatencySummary {
	if len(samples) == 0 {
		return LatencySummary{}
	}
	sort.Float64s(samples)
	n := len(samples)

	getPercentile := func(p float64) float64 {
		idx := int(float64(n) * p)
		if idx >= n {
			idx = n - 1
		}
		return samples[idx]
	}

	return LatencySummary{
		Count: int64(n),
		MinMs: samples[0],
		P50Ms: getPercentile(0.50),
		P95Ms: getPercentile(0.95),
		P99Ms: getPercentile(0.99),
		MaxMs: samples[n-1],
	}
}

func main() {
	scenarioPath := flag.String("scenario", "bench/scenarios/dhaka_rush_hour.yaml", "Path to scenario yaml")
	design := flag.String("design", "Design_B_Redis_H3", "Target design: Design_B_Redis_H3 or Baseline_A_PostGIS")
	targetURL := flag.String("target", "http://127.0.0.1:8080", "Target base URL")
	matchURL := flag.String("match-target", "http://127.0.0.1:8081", "Match base URL")
	flag.Parse()

	data, err := os.ReadFile(*scenarioPath)
	if err != nil {
		slog.Error("failed to read scenario", "path", *scenarioPath, "error", err)
		os.Exit(1)
	}

	var sc ScenarioConfig
	if err := yaml.Unmarshal(data, &sc); err != nil {
		slog.Error("failed to parse scenario yaml", "error", err)
		os.Exit(1)
	}

	rnd := rand.New(rand.NewSource(sc.Seed))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Printf("Starting open-loop benchmark [%s] on scenario [%s] for %ds...\n", *design, sc.Name, sc.DurationSeconds)

	type Driver struct {
		ID  string
		Lat float64
		Lng float64
	}

	drivers := make([]Driver, sc.DriverCount)
	for i := 0; i < sc.DriverCount; i++ {
		drivers[i] = Driver{
			ID:  fmt.Sprintf("drv_%05d", i+1),
			Lat: sc.Bounds.MinLat + rnd.Float64()*(sc.Bounds.MaxLat-sc.Bounds.MinLat),
			Lng: sc.Bounds.MinLng + rnd.Float64()*(sc.Bounds.MaxLng-sc.Bounds.MinLng),
		}
	}

	client := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        1000,
			MaxIdleConnsPerHost: 500,
		},
	}

	var pingLatencies []float64
	var matchLatencies []float64
	var pingMu, matchMu sync.Mutex
	var failedPings, failedMatches int64

	var wg sync.WaitGroup

	// Open-loop ping generation: fixed interval per driver
	pingInterval := time.Duration(sc.PingIntervalSeconds) * time.Second
	for i := range drivers {
		wg.Add(1)
		go func(d Driver) {
			defer wg.Done()
			ticker := time.NewTicker(pingInterval)
			defer ticker.Stop()

			timer := time.After(time.Duration(sc.DurationSeconds) * time.Second)
			curLat, curLng := d.Lat, d.Lng

			for {
				select {
				case <-ctx.Done():
					return
				case <-timer:
					return
				case <-ticker.C:
					// Slight jitter/movement ~ 10-30 meters
					curLat += (rnd.Float64() - 0.5) * 0.0003
					curLng += (rnd.Float64() - 0.5) * 0.0003

					payload, _ := json.Marshal(map[string]interface{}{
						"driver_id": d.ID,
						"lat":       curLat,
						"lng":       curLng,
						"timestamp": time.Now().UnixMilli(),
						"status":    "available",
					})

					start := time.Now()
					resp, err := client.Post(*targetURL+"/ping", "application/json", bytes.NewReader(payload))
					elapsedMs := float64(time.Since(start).Microseconds()) / 1000.0

					if err != nil || resp.StatusCode >= 400 {
						pingMu.Lock()
						failedPings++
						pingMu.Unlock()
					} else {
						_ = resp.Body.Close()
						pingMu.Lock()
						pingLatencies = append(pingLatencies, elapsedMs)
						pingMu.Unlock()
					}
				}
			}
		}(drivers[i])
	}

	// Open-loop rider match requests at fixed arrival rate
	if sc.RiderRequestRatePerSec > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			interval := time.Second / time.Duration(sc.RiderRequestRatePerSec)
			ticker := time.NewTicker(interval)
			defer ticker.Stop()

			timer := time.After(time.Duration(sc.DurationSeconds) * time.Second)
			reqID := 0

			for {
				select {
				case <-ctx.Done():
					return
				case <-timer:
					return
				case <-ticker.C:
					reqID++
					rLat := sc.Bounds.MinLat + rnd.Float64()*(sc.Bounds.MaxLat-sc.Bounds.MinLat)
					rLng := sc.Bounds.MinLng + rnd.Float64()*(sc.Bounds.MaxLng-sc.Bounds.MinLng)

					payload, _ := json.Marshal(map[string]interface{}{
						"ride_id":     fmt.Sprintf("sim_ride_%d", reqID),
						"rider_id":    fmt.Sprintf("sim_rider_%d", reqID),
						"pickup_lat":  rLat,
						"pickup_lng":  rLng,
						"dropoff_lat": rLat + 0.015,
						"dropoff_lng": rLng + 0.015,
					})

					start := time.Now()
					resp, err := client.Post(*matchURL+"/match", "application/json", bytes.NewReader(payload))
					elapsedMs := float64(time.Since(start).Microseconds()) / 1000.0

					if err != nil || resp.StatusCode >= 400 {
						matchMu.Lock()
						failedMatches++
						matchMu.Unlock()
					} else {
						_ = resp.Body.Close()
						matchMu.Lock()
						matchLatencies = append(matchLatencies, elapsedMs)
						matchMu.Unlock()
					}
				}
			}
		}()
	}

	wg.Wait()

	// Compute summaries
	pingSummary := computePercentiles(pingLatencies)
	matchSummary := computePercentiles(matchLatencies)

	hostname, _ := os.Hostname()
	result := BenchmarkResult{
		Date:          time.Now().UTC().Format("2006-01-02"),
		Scenario:      sc.Name,
		Design:        *design,
		GoVersion:     runtime.Version(),
		Machine:       fmt.Sprintf("%s (%s/%s, %d CPUs)", hostname, runtime.GOOS, runtime.GOARCH, runtime.NumCPU()),
		Seed:          sc.Seed,
		DriverCount:   sc.DriverCount,
		TotalPings:    int64(len(pingLatencies)),
		TotalMatches:  int64(len(matchLatencies)),
		PingLatency:   pingSummary,
		MatchLatency:  matchSummary,
		FailedPings:   failedPings,
		FailedMatches: failedMatches,
	}

	_ = os.MkdirAll("bench/results", 0755)
	outFileName := fmt.Sprintf("%s-%s-%s.json", result.Date, sc.Name, *design)
	outFilePath := filepath.Join("bench", "results", outFileName)

	outBytes, err := json.MarshalIndent(result, "", "  ")
	if err == nil {
		_ = os.WriteFile(outFilePath, outBytes, 0644)
		fmt.Printf("Benchmark completed successfully! Results written to %s\n", outFilePath)
	}

	fmt.Printf("Ping Latency (p50: %.2fms, p95: %.2fms, p99: %.2fms, max: %.2fms, samples: %d)\n",
		pingSummary.P50Ms, pingSummary.P95Ms, pingSummary.P99Ms, pingSummary.MaxMs, pingSummary.Count)
	fmt.Printf("Match Latency (p50: %.2fms, p95: %.2fms, p99: %.2fms, max: %.2fms, samples: %d)\n",
		matchSummary.P50Ms, matchSummary.P95Ms, matchSummary.P99Ms, matchSummary.MaxMs, matchSummary.Count)
}
