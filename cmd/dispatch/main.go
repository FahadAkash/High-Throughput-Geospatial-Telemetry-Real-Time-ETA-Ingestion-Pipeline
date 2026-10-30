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

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/errgroup"

	"github.com/FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline/internal/config"
	"github.com/FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline/internal/match"
	"github.com/FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline/internal/routing"
	"github.com/FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline/internal/store/postgres"
	redisStore "github.com/FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline/internal/store/redis"
	"github.com/FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline/internal/surge"
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

	// 1. Redis Client
	redisOpts, err := cfg.RedisOptions()
	if err != nil {
		slog.Error("failed to parse redis options", "error", err)
		os.Exit(1)
	}
	rdb := redis.NewClient(redisOpts)
	defer rdb.Close()
	redisClient := redisStore.NewClient(rdb)

	// 2. Postgres Client
	pgClient, err := postgres.NewClient(ctx, cfg.PostgresURL)
	if err != nil {
		slog.Warn("postgres connection failed; starting dispatch in non-persisted mode", "error", err)
	} else {
		defer pgClient.Close()
	}

	// 3. Routing & Surge
	etaCache := routing.NewETACache(redisClient, 5*time.Minute)
	router := routing.NewOSRMClient(cfg.OSRMURL, etaCache)
	surgeCalc := surge.NewCalculator(redisClient)

	// 4. Match Service
	matchService := match.NewService(
		redisClient,
		redisClient,
		router,
		surgeCalc,
		pgClient,
		nil,
		cfg.StaleWindow,
		cfg.ClaimTTL,
		cfg.MaxCandidates,
	)

	// 5. HTTP Handlers
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	mux.HandleFunc("/match", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		var req match.MatchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid match json: %v", err), http.StatusBadRequest)
			return
		}

		if req.RideID == "" {
			req.RideID = fmt.Sprintf("ride_%d", time.Now().UnixNano())
		}

		resp, err := matchService.Match(r.Context(), &req)
		if err != nil {
			if errors.Is(err, match.ErrNoDriversAvailable) || errors.Is(err, match.ErrAllDriversConflict) {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.DispatchPort),
		Handler: mux,
	}

	g, gCtx := errgroup.WithContext(ctx)
	g.Go(func() error {
		slog.Info("starting dispatch service", "port", cfg.DispatchPort)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("dispatch server failed: %w", err)
		}
		return nil
	})

	g.Go(func() error {
		<-gCtx.Done()
		slog.Info("shutting down dispatch service")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	})

	if err := g.Wait(); err != nil {
		slog.Error("dispatch exited with error", "error", err)
	}
}
