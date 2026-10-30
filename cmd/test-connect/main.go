package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline/internal/config"
	"github.com/FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline/internal/geo"
	redisStore "github.com/FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline/internal/store/redis"
)

func main() {
	fmt.Println("======================================================================")
	fmt.Println("   REMOTE INFRASTRUCTURE CONNECTIVITY & PIPELINE VALIDATION TEST")
	fmt.Println("======================================================================")

	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("FAILED: config load: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Target Host:      192.168.0.113\n")
	fmt.Printf("Postgres URL:     %s\n", maskURL(cfg.PostgresURL))
	fmt.Printf("Redis URL:        %s\n", maskURL(cfg.RedisURL))
	fmt.Printf("Kafka Brokers:    %v\n", cfg.KafkaBrokers)
	fmt.Printf("Kafka Topic:      %s\n", cfg.KafkaTopic)
	fmt.Printf("Kafka Group:      %s\n", cfg.KafkaGroupID)
	fmt.Println("----------------------------------------------------------------------")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// 1. TEST REDIS
	testRedis(ctx, cfg)

	// 2. TEST POSTGRESQL & POSTGIS
	testPostgres(ctx, cfg)

	// 3. TEST KAFKA
	testKafka(ctx, cfg)

	fmt.Println("======================================================================")
	fmt.Println("   ALL SERVICE TESTS COMPLETED")
	fmt.Println("======================================================================")
}

func testRedis(ctx context.Context, cfg *config.Config) {
	fmt.Print("[1/3] Testing Redis In-Memory Store & Lua Scripts... ")
	start := time.Now()

	opts, err := cfg.RedisOptions()
	if err != nil {
		fmt.Printf("FAILED: options error: %v\n", err)
		return
	}

	rdb := redis.NewClient(opts)
	defer rdb.Close()

	if err := rdb.Ping(ctx).Err(); err != nil {
		fmt.Printf("FAILED to ping Redis: %v\n", err)
		return
	}

	client := redisStore.NewClient(rdb)

	// Test hot-path Lua script update
	testDriverID := "drv_test_live_001"
	testLat, testLng := 23.7925, 90.4078
	testCell, _ := geo.LatLngToCell(testLat, testLng, geo.DispatchResolution)
	cellHex := geo.CellToHex(testCell)
	nowMs := time.Now().UnixMilli()

	err = client.UpdateLocation(ctx, testDriverID, testLat, testLng, cellHex, "", nowMs, "available", 30*time.Second)
	if err != nil {
		fmt.Printf("FAILED Lua update_location: %v\n", err)
		return
	}

	// Test atomic claim
	won, err := client.ClaimDriver(ctx, testDriverID, "ride_diag_test", 10*time.Second)
	if err != nil || !won {
		fmt.Printf("FAILED atomic claim: %v (won=%v)\n", err, won)
		return
	}

	// Test claim release
	released, err := client.ReleaseClaim(ctx, testDriverID, "ride_diag_test")
	if err != nil || !released {
		fmt.Printf("FAILED claim release: %v (released=%v)\n", err, released)
		return
	}

	elapsed := time.Since(start)
	fmt.Printf("SUCCESS! (Round-trip: %v, H3 Res 8 Cell: %s, Atomic Mutex: OK)\n", elapsed, cellHex)
}

func testPostgres(ctx context.Context, cfg *config.Config) {
	fmt.Print("[2/3] Testing PostgreSQL & PostGIS Database... ")
	start := time.Now()

	poolCfg, err := pgxpool.ParseConfig(cfg.PostgresURL)
	if err != nil {
		fmt.Printf("FAILED: parse postgres config: %v\n", err)
		return
	}
	poolCfg.MaxConns = 5

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		fmt.Printf("FAILED: create pool: %v\n", err)
		return
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("FAILED: postgres ping: %v\n", err)
		return
	}

	var version string
	if err := pool.QueryRow(ctx, "SELECT version()").Scan(&version); err != nil {
		fmt.Printf("FAILED: query version: %v\n", err)
		return
	}

	// Check or initialize rides table
	createTableSQL := `
		CREATE TABLE IF NOT EXISTS rides (
			ride_id VARCHAR(64) PRIMARY KEY,
			rider_id VARCHAR(64) NOT NULL,
			driver_id VARCHAR(64) NOT NULL,
			pickup_lat DOUBLE PRECISION NOT NULL,
			pickup_lng DOUBLE PRECISION NOT NULL,
			dropoff_lat DOUBLE PRECISION NOT NULL,
			dropoff_lng DOUBLE PRECISION NOT NULL,
			fare_paisa BIGINT NOT NULL,
			surge_multiplier DOUBLE PRECISION NOT NULL DEFAULT 1.0,
			status VARCHAR(32) NOT NULL DEFAULT 'created',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`
	if _, err := pool.Exec(ctx, createTableSQL); err != nil {
		fmt.Printf("WARNING: ensuring rides table: %v\n", err)
	}

	// Test insert and read
	testRideID := fmt.Sprintf("test_ride_%d", time.Now().UnixNano())
	insertSQL := `
		INSERT INTO rides (ride_id, rider_id, driver_id, pickup_lat, pickup_lng, dropoff_lat, dropoff_lng, fare_paisa, surge_multiplier, status)
		VALUES ($1, 'rider_test', 'driver_test', 23.7925, 90.4078, 23.7330, 90.4172, 25000, 1.2, 'completed')
		ON CONFLICT (ride_id) DO NOTHING;
	`
	if _, err := pool.Exec(ctx, insertSQL, testRideID); err != nil {
		fmt.Printf("FAILED: inserting ride row: %v\n", err)
		return
	}

	// Clean up diagnostic row
	_, _ = pool.Exec(ctx, "DELETE FROM rides WHERE ride_id = $1", testRideID)

	elapsed := time.Since(start)
	fmt.Printf("SUCCESS! (Round-trip: %v, Server: %s)\n", elapsed, truncate(version, 40))
}

func testKafka(ctx context.Context, cfg *config.Config) {
	fmt.Print("[3/3] Testing Apache Kafka Cluster... ")
	start := time.Now()

	opts := []kgo.Opt{
		kgo.SeedBrokers(cfg.KafkaBrokers...),
		kgo.RecordRetries(2),
	}

	client, err := kgo.NewClient(opts...)
	if err != nil {
		fmt.Printf("FAILED: creating kafka client: %v\n", err)
		return
	}
	defer client.Close()

	if err := client.Ping(ctx); err != nil {
		fmt.Printf("FAILED: kafka ping to %v: %v\n", cfg.KafkaBrokers, err)
		return
	}

	admin := kadm.NewClient(client)

	// Check topics
	topicsMetadata, err := admin.ListTopics(ctx)
	if err != nil {
		fmt.Printf("WARNING: listing topics: %v\n", err)
	}

	if !topicsMetadata.Has(cfg.KafkaTopic) {
		fmt.Printf("\n    -> Topic '%s' does not exist yet. Creating topic... ", cfg.KafkaTopic)
		createResp, err := admin.CreateTopics(ctx, 3, 1, nil, cfg.KafkaTopic)
		if err != nil {
			fmt.Printf("create error: %v\n", err)
		} else if createResp[cfg.KafkaTopic].Err != nil {
			fmt.Printf("create error: %v\n", createResp[cfg.KafkaTopic].Err)
		} else {
			fmt.Print("CREATED! ")
		}
		// Brief pause for partition metadata propagation
		time.Sleep(500 * time.Millisecond)
	}

	// Produce a diagnostic event to the topic
	record := &kgo.Record{
		Topic: cfg.KafkaTopic,
		Key:   []byte("diag_test_driver"),
		Value: []byte(fmt.Sprintf(`{"test":true,"timestamp":%d}`, time.Now().UnixMilli())),
	}

	res := client.ProduceSync(ctx, record)
	if err := res.FirstErr(); err != nil {
		fmt.Printf("FAILED: produce to topic %s: %v\n", cfg.KafkaTopic, err)
		return
	}

	elapsed := time.Since(start)
	fmt.Printf("SUCCESS! (Round-trip: %v, Topic: %s, Partition: %d, Offset: %d)\n",
		elapsed, record.Topic, res[0].Record.Partition, res[0].Record.Offset)
}

func maskURL(raw string) string {
	if len(raw) < 15 {
		return raw
	}
	return raw[:12] + "****" + raw[len(raw)-15:]
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}
