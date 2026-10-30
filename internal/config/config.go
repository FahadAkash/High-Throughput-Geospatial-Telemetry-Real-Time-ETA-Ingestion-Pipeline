package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all environment-based configuration parameters.
// Parsed once at service initialization.
type Config struct {
	// Redis
	RedisURL      string
	RedisPassword string
	RedisDB       int

	// Kafka / Redpanda
	KafkaBrokers []string
	KafkaTopic   string

	// PostgreSQL
	PostgresURL string

	// OSRM Routing Engine
	OSRMURL string

	// Ports
	GatewayPort  int
	DispatchPort int
	MetricsPort  int

	// Business & Operational Limits
	City          string
	StaleWindow   time.Duration
	MaxCandidates int
	ClaimTTL      time.Duration
}

// Load reads and parses all configuration from the environment with production defaults.
func Load() (*Config, error) {
	cfg := &Config{
		RedisURL:      getEnv("REDIS_URL", "127.0.0.1:6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		RedisDB:       getEnvInt("REDIS_DB", 0),

		KafkaBrokers: getEnvSlice("KAFKA_BROKERS", []string{"127.0.0.1:9092"}),
		KafkaTopic:   getEnv("KAFKA_TOPIC", "telemetry.dhaka"),

		PostgresURL: getEnv("POSTGRES_URL", "postgres://postgres:postgres@127.0.0.1:5432/rides?sslmode=disable"),
		OSRMURL:     getEnv("OSRM_URL", "http://127.0.0.1:5000"),

		GatewayPort:  getEnvInt("GATEWAY_PORT", 8080),
		DispatchPort: getEnvInt("DISPATCH_PORT", 8081),
		MetricsPort:  getEnvInt("METRICS_PORT", 9090),

		City:          getEnv("CITY", "dhaka"),
		StaleWindow:   getEnvDuration("STALE_WINDOW", 15*time.Second),
		MaxCandidates: getEnvInt("MAX_CANDIDATES", 10),
		ClaimTTL:      getEnvDuration("CLAIM_TTL", 10*time.Second),
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return cfg, nil
}

func (c *Config) validate() error {
	if c.RedisURL == "" {
		return fmt.Errorf("REDIS_URL must not be empty")
	}
	if len(c.KafkaBrokers) == 0 {
		return fmt.Errorf("KAFKA_BROKERS must not be empty")
	}
	if c.StaleWindow <= 0 {
		return fmt.Errorf("STALE_WINDOW must be positive")
	}
	if c.MaxCandidates <= 0 {
		return fmt.Errorf("MAX_CANDIDATES must be positive")
	}
	return nil
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

func getEnvDuration(key string, defaultVal time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
	}
	return defaultVal
}

func getEnvSlice(key string, defaultVal []string) []string {
	if val := os.Getenv(key); val != "" {
		parts := strings.Split(val, ",")
		res := make([]string, 0, len(parts))
		for _, p := range parts {
			if s := strings.TrimSpace(p); s != "" {
				res = append(res, s)
			}
		}
		if len(res) > 0 {
			return res
		}
	}
	return defaultVal
}
