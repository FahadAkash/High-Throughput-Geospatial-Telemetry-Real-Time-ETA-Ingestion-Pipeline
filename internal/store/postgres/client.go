package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Ride represents the state of a ride order.
// Money is in integer paisa (BDT).
type Ride struct {
	RideID          string
	RiderID         string
	DriverID        string
	PickupLat       float64
	PickupLng       float64
	DropoffLat      float64
	DropoffLng      float64
	FarePaisa       int64
	SurgeMultiplier float64
	Status          string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// TelemetryRow represents an archived ping row for batch inserting.
type TelemetryRow struct {
	DriverID string
	H3Cell   string
	Lat      float64
	Lng      float64
	Ts       int64
}

// BaselineDriver represents a driver row in the PostGIS control group.
type BaselineDriver struct {
	DriverID   string
	Lat        float64
	Lng        float64
	LastPingAt time.Time
	DistanceM  float64
}

// Client wraps pgxpool.Pool for short, efficient database operations.
type Client struct {
	pool *pgxpool.Pool
}

// NewClient initializes a pgxpool with connection limits.
func NewClient(ctx context.Context, databaseURL string) (*Client, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parsing postgres connection string: %w", err)
	}

	config.MaxConns = 50
	config.MinConns = 5
	config.MaxConnLifetime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("creating postgres connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		// Log warning but allow client creation if Postgres is starting up or in offline testing
		_ = err
	}

	return &Client{pool: pool}, nil
}

// Close releases the database pool.
func (c *Client) Close() {
	if c.pool != nil {
		c.pool.Close()
	}
}

// CreateRide persists a ride record upon successful claim.
// CRITICAL RULE: Pings never touch Postgres; only dispatch outcomes are written here.
func (c *Client) CreateRide(ctx context.Context, r *Ride) error {
	query := `
		INSERT INTO rides (
			ride_id, rider_id, driver_id,
			pickup_lat, pickup_lng, dropoff_lat, dropoff_lng,
			fare_paisa, surge_multiplier, status, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW(), NOW())
	`
	_, err := c.pool.Exec(ctx, query,
		r.RideID, r.RiderID, r.DriverID,
		r.PickupLat, r.PickupLng, r.DropoffLat, r.DropoffLng,
		r.FarePaisa, r.SurgeMultiplier, r.Status,
	)
	if err != nil {
		return fmt.Errorf("inserting ride record %s: %w", r.RideID, err)
	}
	return nil
}

// BatchInsertTelemetry performs an efficient PostgreSQL COPY or UNNEST batch insert for archived pings.
func (c *Client) BatchInsertTelemetry(ctx context.Context, rows []TelemetryRow) error {
	if len(rows) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	query := `
		INSERT INTO telemetry_archive (driver_id, h3_cell, lat, lng, ts, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (driver_id, ts) DO NOTHING
	`

	for _, row := range rows {
		batch.Queue(query, row.DriverID, row.H3Cell, row.Lat, row.Lng, row.Ts)
	}

	br := c.pool.SendBatch(ctx, batch)
	defer br.Close()

	for range rows {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("batch executing telemetry insert: %w", err)
		}
	}
	return nil
}

// BaselineUpdatePing updates driver location in PostGIS (Baseline A control group).
// Every ping runs an UPDATE on Postgres/PostGIS to demonstrate bottleneck vs Redis.
func (c *Client) BaselineUpdatePing(ctx context.Context, driverID string, lat, lng float64, t time.Time) error {
	query := `
		INSERT INTO baseline_drivers (driver_id, location, last_ping_at, status)
		VALUES ($1, ST_SetSRID(ST_MakePoint($2, $3), 4326), $4, 'available')
		ON CONFLICT (driver_id)
		DO UPDATE SET
			location = ST_SetSRID(ST_MakePoint($2, $3), 4326),
			last_ping_at = EXCLUDED.last_ping_at,
			status = EXCLUDED.status
	`
	_, err := c.pool.Exec(ctx, query, driverID, lng, lat, t)
	if err != nil {
		return fmt.Errorf("baseline update driver %s in postgis: %w", driverID, err)
	}
	return nil
}

// BaselineFindNearby queries PostGIS for drivers within radiusMeters using ST_DWithin and orders by ST_Distance.
func (c *Client) BaselineFindNearby(ctx context.Context, lat, lng float64, radiusMeters float64, limit int, staleThreshold time.Time) ([]BaselineDriver, error) {
	query := `
		SELECT 
			driver_id,
			ST_Y(location) as lat,
			ST_X(location) as lng,
			last_ping_at,
			ST_Distance(location::geography, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography) as dist_meters
		FROM baseline_drivers
		WHERE status = 'available'
		  AND last_ping_at >= $3
		  AND ST_DWithin(location::geography, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography, $4)
		ORDER BY dist_meters ASC
		LIMIT $5
	`
	rows, err := c.pool.Query(ctx, query, lng, lat, staleThreshold, radiusMeters, limit)
	if err != nil {
		return nil, fmt.Errorf("baseline find nearby postgis query: %w", err)
	}
	defer rows.Close()

	var drivers []BaselineDriver
	for rows.Next() {
		var d BaselineDriver
		if err := rows.Scan(&d.DriverID, &d.Lat, &d.Lng, &d.LastPingAt, &d.DistanceM); err != nil {
			return nil, fmt.Errorf("scanning baseline driver row: %w", err)
		}
		drivers = append(drivers, d)
	}
	return drivers, nil
}
