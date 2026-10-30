package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
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
	KafkaGroupID string
	KafkaEnabled bool

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

// Load reads and parses all configuration from .env and the environment.
func Load() (*Config, error) {
	// Automatically load .env if present
	loadDotEnv(".env")

	redisURL := getEnv("REDIS_URL", "127.0.0.1:6379")
	redisPass := getEnv("REDIS_PASSWORD", "")
	redisDB := getEnvInt("REDIS_DB", 0)

	// If REDIS_URL has redis:// scheme, parse it for default password & DB
	if strings.HasPrefix(redisURL, "redis://") || strings.HasPrefix(redisURL, "rediss://") {
		if opt, err := redis.ParseURL(redisURL); err == nil {
			if redisPass == "" && opt.Password != "" {
				redisPass = opt.Password
			}
			if redisDB == 0 && opt.DB != 0 {
				redisDB = opt.DB
			}
		}
	}

	// Postgres URL fallback
	pgURL := getEnv("POSTGRES_URL", "")
	if pgURL == "" {
		pgURL = getEnv("DATABASE_URL", "postgres://postgres:postgres@127.0.0.1:5432/rides?sslmode=disable")
	}

	// Kafka Brokers fallback
	kafkaBrokers := getEnvSlice("KAFKA_BROKERS", nil)
	if len(kafkaBrokers) == 0 {
		kafkaBrokers = getEnvSlice("KAFKA_BOOTSTRAP_SERVERS", []string{"127.0.0.1:9092"})
	}

	cfg := &Config{
		RedisURL:      redisURL,
		RedisPassword: redisPass,
		RedisDB:       redisDB,

		KafkaBrokers: kafkaBrokers,
		KafkaTopic:   getEnv("KAFKA_TOPIC", "telemetry.dhaka"),
		KafkaGroupID: getEnv("KAFKA_GROUP_ID", "telemetry-consumer-group"),
		KafkaEnabled: getEnvBool("KAFKA_ENABLED", true),

		PostgresURL: pgURL,
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

// RedisOptions returns parsed redis.Options supporting both redis:// URL schemes and raw host:port.
func (c *Config) RedisOptions() (*redis.Options, error) {
	if strings.HasPrefix(c.RedisURL, "redis://") || strings.HasPrefix(c.RedisURL, "rediss://") {
		opts, err := redis.ParseURL(c.RedisURL)
		if err != nil {
			return nil, fmt.Errorf("parsing redis URL: %w", err)
		}
		if c.RedisPassword != "" {
			opts.Password = c.RedisPassword
		}
		return opts, nil
	}

	return &redis.Options{
		Addr:     c.RedisURL,
		Password: c.RedisPassword,
		DB:       c.RedisDB,
	}, nil
}

func (c *Config) validate() error {
	if c.RedisURL == "" {
		return fmt.Errorf("REDIS_URL must not be empty")
	}
	if c.KafkaEnabled && len(c.KafkaBrokers) == 0 {
		return fmt.Errorf("KAFKA_BROKERS must not be empty when KAFKA_ENABLED=true")
	}
	if c.StaleWindow <= 0 {
		return fmt.Errorf("STALE_WINDOW must be positive")
	}
	if c.MaxCandidates <= 0 {
		return fmt.Errorf("MAX_CANDIDATES must be positive")
	}
	return nil
}

func loadDotEnv(filename string) {
	file, err := os.Open(filename)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			// Strip optional quotes
			if (strings.HasPrefix(val, "\"") && strings.HasSuffix(val, "\"")) ||
				(strings.HasPrefix(val, "'") && strings.HasSuffix(val, "'")) {
				val = val[1 : len(val)-1]
			}
			// Only set if not already set in environment
			if os.Getenv(key) == "" {
				_ = os.Setenv(key, val)
			}
		}
	}
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

func getEnvBool(key string, defaultVal bool) bool {
	if val := os.Getenv(key); val != "" {
		if b, err := strconv.ParseBool(val); err == nil {
			return b
		}
	}
	return defaultVal
}
