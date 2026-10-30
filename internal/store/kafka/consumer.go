package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

// TelemetryHandler handles processed telemetry batches.
type TelemetryHandler func(ctx context.Context, events []*TelemetryEvent) error

// Consumer wraps franz-go consumer with at-least-once processing and deduplication.
type Consumer struct {
	client  *kgo.Client
	handler TelemetryHandler

	// Deduplication cache to ensure idempotency on (driver_id + ts)
	mu       sync.Mutex
	seenKeys map[string]time.Time
}

// NewConsumer initializes a Kafka consumer for the archiver service.
func NewConsumer(brokers []string, groupID, topic string, handler TelemetryHandler) (*Consumer, error) {
	opts := []kgo.Opt{
		kgo.SeedBrokers(brokers...),
		kgo.ConsumerGroup(groupID),
		kgo.ConsumeTopics(topic),
		kgo.DisableAutoCommit(), // Manual commits after successful batch processing
	}

	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("creating Franz-go kafka consumer: %w", err)
	}

	c := &Consumer{
		client:   client,
		handler:  handler,
		seenKeys: make(map[string]time.Time, 100000),
	}

	return c, nil
}

// Run begins polling records until context is cancelled.
func (c *Consumer) Run(ctx context.Context) error {
	// Periodic cache cleanup routine
	cleanupTicker := time.NewTicker(2 * time.Minute)
	defer cleanupTicker.Stop()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-cleanupTicker.C:
				c.evictOldKeys(now)
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		fetches := c.client.PollFetches(ctx)
		if fetches.IsClientClosed() {
			return nil
		}

		if errs := fetches.Errors(); len(errs) > 0 {
			slog.Error("kafka poll errors", "count", len(errs), "first_error", errs[0].Err)
			continue
		}

		var batch []*TelemetryEvent
		fetches.EachRecord(func(r *kgo.Record) {
			var event TelemetryEvent
			if err := json.Unmarshal(r.Value, &event); err != nil {
				slog.Warn("skipping invalid telemetry payload", "error", err)
				return
			}

			// Idempotent deduplication on (driver_id + ts)
			dedupeKey := fmt.Sprintf("%s:%d", event.DriverID, event.Timestamp)
			if c.isDuplicate(dedupeKey) {
				return
			}

			batch = append(batch, &event)
		})

		if len(batch) > 0 {
			if err := c.handler(ctx, batch); err != nil {
				slog.Error("failed to process telemetry batch", "batch_size", len(batch), "error", err)
				// Do not commit on failure; consumer will retry on next poll or rebalance
				continue
			}
		}

		// At-least-once guarantee: commit after processing batch
		if err := c.client.CommitUncommittedOffsets(ctx); err != nil {
			slog.Warn("error committing offsets", "error", err)
		}
	}
}

func (c *Consumer) isDuplicate(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.seenKeys[key]; exists {
		return true
	}
	c.seenKeys[key] = time.Now()
	return false
}

func (c *Consumer) evictOldKeys(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Retain dedupe window for 10 minutes
	cutoff := now.Add(-10 * time.Minute)
	for key, addedAt := range c.seenKeys {
		if addedAt.Before(cutoff) {
			delete(c.seenKeys, key)
		}
	}
}

// Close gracefully closes the consumer.
func (c *Consumer) Close() {
	if c.client != nil {
		c.client.Close()
	}
}
