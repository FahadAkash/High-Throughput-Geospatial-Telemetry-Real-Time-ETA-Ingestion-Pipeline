package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/pkg/kgo"
)

var (
	kafkaPublishErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ride_kafka_publish_errors_total",
		Help: "Total number of asynchronous Kafka telemetry publish failures.",
	})
)

// TelemetryEvent represents a single driver GPS location ping published to Kafka.
type TelemetryEvent struct {
	DriverID  string  `json:"driver_id"`
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	H3Cell    string  `json:"h3_cell"`
	Timestamp int64   `json:"timestamp"`
	Speed     float64 `json:"speed,omitempty"`
	Bearing   float64 `json:"bearing,omitempty"`
}

// Producer manages non-blocking, batched asynchronous publishing to Kafka topics.
type Producer struct {
	client *kgo.Client
	topic  string
}

// NewProducer creates and connects a new Franz-go Kafka producer.
func NewProducer(brokers []string, topic string) (*Producer, error) {
	opts := []kgo.Opt{
		kgo.SeedBrokers(brokers...),
		kgo.ProducerLinger(10), // Small linger to enable batching
		kgo.RecordRetries(3),
	}

	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("creating Franz-go kafka producer: %w", err)
	}

	return &Producer{
		client: client,
		topic:  topic,
	}, nil
}

// PublishPing sends a telemetry ping to Kafka asynchronously.
// CRITICAL INVARIANT: A Kafka publish failure must NEVER block or fail the hot-path Redis write.
func (p *Producer) PublishPing(ctx context.Context, event *TelemetryEvent) {
	if p == nil || p.client == nil {
		return
	}

	payload, err := json.Marshal(event)
	if err != nil {
		kafkaPublishErrors.Inc()
		slog.Error("failed to serialize telemetry event", "driver_id", event.DriverID, "error", err)
		return
	}

	record := &kgo.Record{
		Topic: p.topic,
		Key:   []byte(event.DriverID), // Keyed by driver ID so order holds per driver
		Value: payload,
	}

	p.client.Produce(ctx, record, func(r *kgo.Record, err error) {
		if err != nil {
			kafkaPublishErrors.Inc()
			slog.Warn("asynchronous kafka publish failed",
				"driver_id", string(r.Key),
				"topic", r.Topic,
				"error", err,
			)
		}
	})
}

// Close flushes buffered records and closes the producer.
func (p *Producer) Close() {
	if p != nil && p.client != nil {
		p.client.Close()
	}
}
