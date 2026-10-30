package location

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline/internal/geo"
	"github.com/FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline/internal/store/kafka"
)

var (
	pingWriteLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "ride_ping_write_seconds",
		Help:    "Latency of hot-path driver telemetry writes to Redis in seconds.",
		Buckets: []float64{0.0005, 0.001, 0.002, 0.005, 0.010, 0.025, 0.050, 0.100},
	})

	stalePingsDropped = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ride_stale_pings_dropped_total",
		Help: "Total count of stale driver GPS pings discarded on arrival.",
	})

	onlineDriversGauge = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ride_online_drivers",
		Help: "Approximate current count of active online drivers.",
	})
)

// Clock abstracts system time for deterministic staleness testing.
type Clock interface {
	Now() time.Time
}

// RealClock uses standard UTC time.
type RealClock struct{}

func (RealClock) Now() time.Time {
	return time.Now().UTC()
}

// RedisWriter interface defines required Redis operations for location ingestion.
type RedisWriter interface {
	UpdateLocation(
		ctx context.Context,
		driverID string,
		lat, lng float64,
		newCellHex, oldCellHex string,
		tsMs int64,
		status string,
		ttl time.Duration,
	) error
}

// KafkaPublisher interface defines the asynchronous cold-path event publisher.
type KafkaPublisher interface {
	PublishPing(ctx context.Context, event *kafka.TelemetryEvent)
}

// PingRequest contains driver telemetry transmitted by gateway.
type PingRequest struct {
	DriverID  string  `json:"driver_id"`
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	Timestamp int64   `json:"timestamp"` // Unix ms
	OldCell   string  `json:"old_cell,omitempty"`
	Status    string  `json:"status,omitempty"`
	Speed     float64 `json:"speed,omitempty"`
	Bearing   float64 `json:"bearing,omitempty"`
}

// Service coordinates hot-path Redis updates and cold-path Kafka publishing.
type Service struct {
	redisClient RedisWriter
	kafkaProd   KafkaPublisher
	clock       Clock
	staleWindow time.Duration
	hashTTL     time.Duration
}

// NewService constructs a Location Service.
func NewService(
	redisClient RedisWriter,
	kafkaProd KafkaPublisher,
	clock Clock,
	staleWindow time.Duration,
) *Service {
	if clock == nil {
		clock = RealClock{}
	}
	return &Service{
		redisClient: redisClient,
		kafkaProd:   kafkaProd,
		clock:       clock,
		staleWindow: staleWindow,
		hashTTL:     60 * time.Second, // PX expiry on drv:{id} hash
	}
}

// HandlePing executes the hot-path ingestion pipeline.
// 1. Validates coordinates and staleness.
// 2. Computes H3 cell (res 8).
// 3. Atomically updates Redis (synchronous hot path).
// 4. Publishes to Kafka asynchronously (non-blocking cold path).
// 5. Updates Prometheus metrics.
func (s *Service) HandlePing(ctx context.Context, req *PingRequest) (string, error) {
	if req.DriverID == "" {
		return "", fmt.Errorf("driver_id cannot be empty")
	}
	if req.Lat < -90 || req.Lat > 90 || req.Lng < -180 || req.Lng > 180 {
		return "", fmt.Errorf("coordinates out of bounds: lat=%f, lng=%f", req.Lat, req.Lng)
	}

	now := s.clock.Now().UnixMilli()
	if req.Timestamp == 0 {
		req.Timestamp = now
	}

	// Drop stale pings
	if now-req.Timestamp > s.staleWindow.Milliseconds() {
		stalePingsDropped.Inc()
		return "", fmt.Errorf("ping is stale by %d ms (exceeds window %v)", now-req.Timestamp, s.staleWindow)
	}

	// 1. Convert to H3 Cell at dispatch resolution (8)
	cell, err := geo.LatLngToCell(req.Lat, req.Lng, geo.DispatchResolution)
	if err != nil {
		return "", fmt.Errorf("computing H3 cell: %w", err)
	}
	newCellHex := geo.CellToHex(cell)

	status := req.Status
	if status == "" {
		status = "available"
	}

	// 2. Synchronous Hot-Path Write to Redis
	start := time.Now()
	err = s.redisClient.UpdateLocation(
		ctx,
		req.DriverID,
		req.Lat,
		req.Lng,
		newCellHex,
		req.OldCell,
		req.Timestamp,
		status,
		s.hashTTL,
	)
	if err != nil {
		return "", fmt.Errorf("writing ping to redis: %w", err)
	}
	pingWriteLatency.Observe(time.Since(start).Seconds())

	// 3. Asynchronous Non-Blocking Cold-Path Publish to Kafka
	// Invariant: Kafka is NEVER on the synchronous critical path of the ping.
	if s.kafkaProd != nil {
		event := &kafka.TelemetryEvent{
			DriverID:  req.DriverID,
			Lat:       req.Lat,
			Lng:       req.Lng,
			H3Cell:    newCellHex,
			Timestamp: req.Timestamp,
			Speed:     req.Speed,
			Bearing:   req.Bearing,
		}
		s.kafkaProd.PublishPing(ctx, event)
	}

	return newCellHex, nil
}
