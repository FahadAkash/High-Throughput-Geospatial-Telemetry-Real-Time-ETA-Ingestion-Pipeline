package config

import (
	"os"
	"testing"
	"time"
)

func TestConfigDefaults(t *testing.T) {
	os.Clearenv()
	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if cfg.RedisURL != "127.0.0.1:6379" {
		t.Errorf("expected 127.0.0.1:6379, got %s", cfg.RedisURL)
	}
	if cfg.StaleWindow != 15*time.Second {
		t.Errorf("expected 15s stale window, got %v", cfg.StaleWindow)
	}
	if cfg.MaxCandidates != 10 {
		t.Errorf("expected 10 max candidates, got %d", cfg.MaxCandidates)
	}
	if cfg.ClaimTTL != 10*time.Second {
		t.Errorf("expected 10s claim TTL, got %v", cfg.ClaimTTL)
	}
}

func TestConfigCustomEnv(t *testing.T) {
	os.Clearenv()
	t.Setenv("REDIS_URL", "redis.prod:6380")
	t.Setenv("STALE_WINDOW", "20s")
	t.Setenv("MAX_CANDIDATES", "25")
	t.Setenv("KAFKA_BROKERS", "k1:9092, k2:9092")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	if cfg.RedisURL != "redis.prod:6380" {
		t.Errorf("expected redis.prod:6380, got %s", cfg.RedisURL)
	}
	if cfg.StaleWindow != 20*time.Second {
		t.Errorf("expected 20s, got %v", cfg.StaleWindow)
	}
	if cfg.MaxCandidates != 25 {
		t.Errorf("expected 25, got %d", cfg.MaxCandidates)
	}
	if len(cfg.KafkaBrokers) != 2 || cfg.KafkaBrokers[0] != "k1:9092" || cfg.KafkaBrokers[1] != "k2:9092" {
		t.Errorf("unexpected kafka brokers: %v", cfg.KafkaBrokers)
	}
}
