package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/sync/errgroup"

	"github.com/FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline/internal/config"
	"github.com/FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline/internal/location"
	"github.com/FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline/internal/match"
	"github.com/FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline/internal/store/postgres"
)

var (
	baselinePingLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "baseline_ping_write_seconds",
		Help:    "Latency of PostGIS driver ping updates in seconds (Baseline A).",
		Buckets: []float64{0.001, 0.005, 0.010, 0.025, 0.050, 0.100, 0.250, 0.500, 1.000},
	})

	baselineMatchLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "baseline_match_seconds",
		Help:    "Latency of PostGIS ST_DWithin geospatial match query in seconds (Baseline A).",
		Buckets: []float64{0.005, 0.010, 0.025, 0.050, 0.100, 0.250, 0.500, 1.000, 2.500},
	})
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pgClient, err := postgres.NewClient(ctx, cfg.PostgresURL)
	if err != nil {
		slog.Error("failed to connect to postgres for baseline", "error", err)
		os.Exit(1)
	}
	defer pgClient.Close()

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","mode":"baseline_postgis"}`))
	})

	// Baseline A: Naive approach where EVERY ping directly updates Postgres/PostGIS
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		var req location.PingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		t := time.UnixMilli(req.Timestamp)
		if req.Timestamp == 0 {
			t = time.Now().UTC()
		}

		start := time.Now()
		err := pgClient.BaselineUpdatePing(r.Context(), req.DriverID, req.Lat, req.Lng, t)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		baselinePingLatency.Observe(time.Since(start).Seconds())

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"recorded_postgis"}`))
	})

	// Baseline A: Match queries Postgres ST_DWithin and ST_Distance directly
	mux.HandleFunc("/match", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		var req match.MatchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		start := time.Now()
		staleThreshold := time.Now().Add(-cfg.StaleWindow)
		const radiusMeters = 3000.0 // 3 km radius

		drivers, err := pgClient.BaselineFindNearby(r.Context(), req.PickupLat, req.PickupLng, radiusMeters, 1, staleThreshold)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if len(drivers) == 0 {
			http.Error(w, "no drivers available in postgis range", http.StatusNotFound)
			return
		}

		matchedDriver := drivers[0]
		baselineMatchLatency.Observe(time.Since(start).Seconds())

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ride_id":         req.RideID,
			"driver_id":       matchedDriver.DriverID,
			"distance_meters": matchedDriver.DistanceM,
			"matched_at":      time.Now(),
		})
	})

	port := 8089
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	g, gCtx := errgroup.WithContext(ctx)
	g.Go(func() error {
		slog.Info("starting baseline postgis service", "port", port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("baseline server failed: %w", err)
		}
		return nil
	})

	g.Go(func() error {
		<-gCtx.Done()
		slog.Info("shutting down baseline service")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	})

	if err := g.Wait(); err != nil {
		slog.Error("baseline service exited with error", "error", err)
	}
}
