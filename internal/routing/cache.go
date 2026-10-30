package routing

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	etaCacheCounter = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ride_eta_cache_total",
		Help: "Total count of ETA cache queries labeled by result (hit or miss).",
	}, []string{"result"})
)

// ETACacheReader defines the Redis interface for cached ETA operations.
type ETACacheReader interface {
	GetCachedETA(ctx context.Context, origHex, destHex string, bucket5m int64) (int64, bool, error)
	SetCachedETA(ctx context.Context, origHex, destHex string, bucket5m int64, durationSec int64, ttl time.Duration) error
}

// ETACache wraps Redis caching for H3 cell-to-cell travel times in 5-minute buckets.
type ETACache struct {
	client ETACacheReader
	ttl    time.Duration
}

// NewETACache constructs a new ETACache.
func NewETACache(client ETACacheReader, ttl time.Duration) *ETACache {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &ETACache{
		client: client,
		ttl:    ttl,
	}
}

// Get5MinBucket returns the current 5-minute Unix bucket index.
func Get5MinBucket(t time.Time) int64 {
	return t.Unix() / 300
}

// Get queries cache for cell-to-cell duration.
func (c *ETACache) Get(ctx context.Context, origHex, destHex string, now time.Time) (time.Duration, bool) {
	if c.client == nil {
		etaCacheCounter.WithLabelValues("miss").Inc()
		return 0, false
	}

	bucket := Get5MinBucket(now)
	sec, hit, err := c.client.GetCachedETA(ctx, origHex, destHex, bucket)
	if err != nil || !hit {
		etaCacheCounter.WithLabelValues("miss").Inc()
		return 0, false
	}

	etaCacheCounter.WithLabelValues("hit").Inc()
	return time.Duration(sec) * time.Second, true
}

// Set stores cell-to-cell duration in cache.
func (c *ETACache) Set(ctx context.Context, origHex, destHex string, now time.Time, duration time.Duration) {
	if c.client == nil {
		return
	}
	bucket := Get5MinBucket(now)
	_ = c.client.SetCachedETA(ctx, origHex, destHex, bucket, int64(duration.Seconds()), c.ttl)
}
