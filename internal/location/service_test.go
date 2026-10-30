package location

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline/internal/store/kafka"
)

type mockClock struct {
	now time.Time
}

func (m *mockClock) Now() time.Time {
	return m.now
}

type mockRedisWriter struct {
	called     bool
	lastDriver string
	lastCell   string
}

func (m *mockRedisWriter) UpdateLocation(
	ctx context.Context,
	driverID string,
	lat, lng float64,
	newCellHex, oldCellHex string,
	tsMs int64,
	status string,
	ttl time.Duration,
) error {
	m.called = true
	m.lastDriver = driverID
	m.lastCell = newCellHex
	return nil
}

type mockKafkaPublisher struct {
	published bool
	lastEvent *kafka.TelemetryEvent
}

func (m *mockKafkaPublisher) PublishPing(ctx context.Context, event *kafka.TelemetryEvent) {
	m.published = true
	m.lastEvent = event
}

func TestHandlePingSuccess(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	clock := &mockClock{now: now}
	mockRedis := &mockRedisWriter{}
	mockKafka := &mockKafkaPublisher{}

	svc := NewService(mockRedis, mockKafka, clock, 15*time.Second)

	req := &PingRequest{
		DriverID:  "drv_123",
		Lat:       23.7925,
		Lng:       90.4078,
		Timestamp: now.UnixMilli(),
		Status:    "available",
	}

	cellHex, err := svc.HandlePing(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cellHex == "" {
		t.Fatal("expected non-empty cell hex")
	}

	if !mockRedis.called || mockRedis.lastDriver != "drv_123" {
		t.Fatalf("redis write was not properly executed: %+v", mockRedis)
	}

	if !mockKafka.published || mockKafka.lastEvent == nil || mockKafka.lastEvent.DriverID != "drv_123" {
		t.Fatalf("kafka publish was not properly executed: %+v", mockKafka)
	}
}

func TestHandlePingStale(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	clock := &mockClock{now: now}
	mockRedis := &mockRedisWriter{}
	mockKafka := &mockKafkaPublisher{}

	svc := NewService(mockRedis, mockKafka, clock, 15*time.Second)

	// Ping from 30 seconds ago (exceeds 15s window)
	staleReq := &PingRequest{
		DriverID:  "drv_stale",
		Lat:       23.7925,
		Lng:       90.4078,
		Timestamp: now.Add(-30 * time.Second).UnixMilli(),
	}

	_, err := svc.HandlePing(context.Background(), staleReq)
	if err == nil {
		t.Fatal("expected error for stale ping, got nil")
	}

	if !strings.Contains(err.Error(), "stale") {
		t.Fatalf("expected stale error message, got: %v", err)
	}

	if mockRedis.called {
		t.Fatal("redis write should NOT be called for stale ping")
	}
}

func TestHandlePingInvalidCoordinates(t *testing.T) {
	clock := &mockClock{now: time.Now()}
	mockRedis := &mockRedisWriter{}
	svc := NewService(mockRedis, nil, clock, 15*time.Second)

	badReq := &PingRequest{
		DriverID: "drv_bad",
		Lat:      195.0, // Invalid latitude
		Lng:      90.0,
	}

	_, err := svc.HandlePing(context.Background(), badReq)
	if err == nil {
		t.Fatal("expected out of bounds error, got nil")
	}
}
