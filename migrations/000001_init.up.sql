-- 000001_init.up.sql: Schema for Production Rides, Telemetry Archive, and Baseline PostGIS Control Group

-- 1. Rides State Table (Postgres holds ride and payment state only; pings NEVER touch this table)
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

CREATE INDEX IF NOT EXISTS idx_rides_driver_id ON rides(driver_id);
CREATE INDEX IF NOT EXISTS idx_rides_created_at ON rides(created_at);

-- 2. Cold-Path Telemetry Archive (Feed for ML, demand forecast, and analytics)
CREATE TABLE IF NOT EXISTS telemetry_archive (
    driver_id VARCHAR(64) NOT NULL,
    h3_cell VARCHAR(32) NOT NULL,
    lat DOUBLE PRECISION NOT NULL,
    lng DOUBLE PRECISION NOT NULL,
    ts BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (driver_id, ts)
);

CREATE INDEX IF NOT EXISTS idx_telemetry_cell_ts ON telemetry_archive(h3_cell, ts);

-- 3. Baseline A Control Group Tables (Naive PostGIS approach for performance comparison)
CREATE EXTENSION IF NOT EXISTS postgis;

CREATE TABLE IF NOT EXISTS baseline_drivers (
    driver_id VARCHAR(64) PRIMARY KEY,
    location GEOMETRY(Point, 4326) NOT NULL,
    last_ping_at TIMESTAMPTZ NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'available'
);

CREATE INDEX IF NOT EXISTS idx_baseline_drivers_location ON baseline_drivers USING GIST(location);

