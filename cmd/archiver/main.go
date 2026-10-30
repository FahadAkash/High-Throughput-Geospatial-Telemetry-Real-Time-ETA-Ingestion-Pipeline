package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/sync/errgroup"

	"github.com/FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline/internal/config"
	"github.com/FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline/internal/store/kafka"
	"github.com/FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline/internal/store/postgres"
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

	// 1. Postgres storage pool
	pgClient, err := postgres.NewClient(ctx, cfg.PostgresURL)
	if err != nil {
		slog.Error("failed to initialize postgres pool", "error", err)
		os.Exit(1)
	}
	defer pgClient.Close()

	// 2. Telemetry batch handler
	batchHandler := func(ctx context.Context, events []*kafka.TelemetryEvent) error {
		rows := make([]postgres.TelemetryRow, len(events))
		for i, e := range events {
			rows[i] = postgres.TelemetryRow{
				DriverID: e.DriverID,
				H3Cell:   e.H3Cell,
				Lat:      e.Lat,
				Lng:      e.Lng,
				Ts:       e.Timestamp,
			}
		}
		return pgClient.BatchInsertTelemetry(ctx, rows)
	}

	// 3. Kafka consumer
	consumer, err := kafka.NewConsumer(cfg.KafkaBrokers, "telemetry-archiver-group", cfg.KafkaTopic, batchHandler)
	if err != nil {
		slog.Error("failed to initialize kafka consumer", "error", err)
		os.Exit(1)
	}
	defer consumer.Close()

	// 4. Metrics & Health HTTP server
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.MetricsPort),
		Handler: mux,
	}

	g, gCtx := errgroup.WithContext(ctx)

	// Archiver consumer routine
	g.Go(func() error {
		slog.Info("starting kafka telemetry consumer", "topic", cfg.KafkaTopic)
		if err := consumer.Run(gCtx); err != nil && !errors.Is(err, context.Canceled) {
			return fmt.Errorf("consumer run loop: %w", err)
		}
		return nil
	})

	// HTTP metrics server routine
	g.Go(func() error {
		slog.Info("starting archiver metrics server", "port", cfg.MetricsPort)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("metrics server failed: %w", err)
		}
		return nil
	})

	g.Go(func() error {
		<-gCtx.Done()
		slog.Info("shutting down archiver service")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	})

	if err := g.Wait(); err != nil {
		slog.Error("archiver exited with error", "error", err)
	}
}
