# AGENTS.md

Instructions for AI coding agents working in this repository. Claude Code reads this through `CLAUDE.md`, which imports it. Other agents read it directly.

## Project

Real-time ride-hailing proof of concept in Go, modeled on a Pathao-style ride-sharing business. It ingests driver GPS pings, keeps live driver state in Redis indexed by Uber H3 cells, matches riders to nearby drivers, and computes ETAs.

Business goals every change must serve:

1. Cost reduction: lower infrastructure cost per 1,000 completed rides.
2. Latency reduction: lower p99 request-to-match time.
3. Profit increase: higher match rate, fewer pre-pickup cancellations, shorter pickups, better driver utilization.
4. Throughput increase: more pings/s and matches/s per node before p99 breaks the target.

The deliverable is a POC with measured before/after numbers: Baseline A (every ping updates Postgres/PostGIS) versus Design B (Redis + H3 hot path, Kafka cold path). A claim without a benchmark result is not a result.

## Architecture

- Hot path (synchronous): MQTT/WebSocket gateway -> location service -> Redis. Dispatch reads Redis, gets ETAs from routing, claims a driver, writes one row to Postgres.
- Cold path (async): the location service also publishes each ping to Kafka/Redpanda. Consumers archive to a time-series store and feed ML (ETA correction, demand forecast, surge).
- Postgres holds ride and payment state only. Pings never touch Postgres.
- Kafka is never in front of Redis on the hot path. Redis write first, Kafka publish second and non-blocking.

## Repository layout

Target layout. Create directories when needed. Do not scaffold empty ones.

```
cmd/gateway/       MQTT/WebSocket ingest, stateless
cmd/dispatch/      match API (request -> ranked drivers -> claim)
cmd/archiver/      Kafka consumer -> time-series store
cmd/simulator/     driver and rider load generator (seeded, open-loop)
cmd/baseline/      Baseline A: naive Postgres/PostGIS version (control group)
internal/geo/      H3 helpers, k-ring, haversine, resolution constants
internal/location/ ping write path
internal/match/    candidate filter, ranking, driver claim
internal/routing/  OSRM client and ETA cache
internal/surge/    per-cell demand/supply and multiplier
internal/store/    redis/, kafka/, postgres/ adapters (Lua in redis/lua/)
internal/config/   env-based config, parsed once
bench/             scenarios/*.yaml and results/*.json (results are generated)
migrations/        numbered SQL files
deploy/            docker-compose.yml and service configs
```

## Commands

```
go build ./...
go vet ./...
gofmt -l .
go test -short -count=1 ./...
go test -race -count=1 ./...
go test -tags=integration -count=1 ./...     # needs docker compose up -d redis redpanda postgres
go test -bench=. -benchmem ./internal/...
```

Hooks run `gofmt`/`goimports`/`gofumpt` after every Go edit, and `go build`, `go vet` and unit tests before a turn ends and before every `git commit`.

## Go conventions

- Use the Go version in `go.mod`. Do not bump it.
- `context.Context` is the first parameter of anything that does I/O. Do not call `context.Background()` outside `main` and tests.
- Wrap errors with `fmt.Errorf("doing x: %w", err)`. Use `errors.Is`/`errors.As`. No `panic` in library code.
- No global mutable state. Inject dependencies through constructors. Define small interfaces at the consumer.
- Every goroutine has an owner and a shutdown path (`errgroup` plus context cancellation). Senders close channels.
- Logging uses `log/slog` with JSON output and structured keys (`driver_id`, `cell`, `request_id`). No Info-level log per ping.
- Config comes from environment variables, parsed once in `internal/config`. No hardcoded hosts, ports or credentials.
- Time is UTC. Inject a `Clock` interface wherever staleness logic exists. Stored timestamps are Unix milliseconds (`int64`).
- Units: lat/lng are `float64` degrees, distances are meters, durations are `time.Duration`, money is integer paisa (BDT). Never `float32` for coordinates or money.
- Prefer the standard library. Add a dependency only with a stated reason, using `go get`.
- Hot-path functions avoid allocations. Verify with `-benchmem` before and after any change.

## Geo and Redis rules

- H3 uses `github.com/uber/h3-go/v4`, which needs cgo. Builds and Docker images need `CGO_ENABLED=1` and a C toolchain.
- Resolution constants live in `internal/geo` only. Defaults: dispatch 8, surge 7. Never hardcode a resolution elsewhere.
- Candidate search reads the rider's cell plus its k-ring neighbors (default k=1) in one pipelined round trip. Never query a single cell: riders near a hexagon border get no drivers.
- Rank by distance, not by Redis score. The ZSET score is a last-ping timestamp, so score order is recency order, not proximity.
- Pipeline: haversine prefilter to the N nearest (default 10), then one OSRM `table` call for all candidates. Never one route call per driver.
- Keys: `cell:{h3hex}` ZSET (member `driverID`, score last-ping Unix ms), `drv:{driverID}` hash (`lat`, `lng`, `cell`, `ts`, `status`), `claim:{driverID}` string, `demand:{h3hex}:{minuteBucket}` counter, `eta:{originCell}:{destCell}:{bucket5m}` cache. Change these only together with the docs.
- Redis has no per-member TTL. Staleness is enforced by score: drivers older than the stale window (default 15 s, configurable) are excluded on read and purged with `ZREMRANGEBYSCORE`. `drv:*` hashes also carry a `PX` expiry.
- A ping that moves a driver to a new cell runs one Lua script that removes them from the old cell, adds them to the new one, and updates `drv:{id}`. Scripts receive every key through `KEYS[]` and never build key names internally. Embed scripts with `//go:embed` and call them with `EVALSHA`.
- Driver claim is atomic: `SET claim:{id} {rideID} NX PX 10000`. Release with a compare-and-delete Lua script on decline, timeout or ride end. Two riders must never hold the same driver.
- The POC assumes single-node Redis. Document cluster implications in `docs/` instead of coding for them early.

## Kafka rules

- Client: `github.com/twmb/franz-go`. Topics per city (`telemetry.dhaka`), keyed by driver ID so order holds per driver and one city is not one hot partition.
- Publishing is async and batched. A publish failure never fails a ping write. Count it in a metric.
- Consumers are at-least-once and idempotent (dedupe on `driver_id` + `ts`).

## Postgres rules

- `pgx/v5` with `pgxpool`. Migrations are sequential numbered `.sql` files. Never edit an applied migration.
- Transactions are short. No `SELECT ... FOR UPDATE` on hot rows. Driver claims live in Redis.
- `cmd/baseline` is the control group. Keep it naive but correct. Do not optimize it.

## Benchmarks and numbers

- Load generation is open-loop (fixed arrival rate) to avoid coordinated omission.
- Latency uses an HDR histogram. Report p50, p95, p99 and max.
- Each run writes `bench/results/<date>-<scenario>-<design>.json` with Go version, commit SHA, machine, seed and scenario. README tables are generated from these files, never typed by hand.
- Scenario assumptions (driver count, ping interval, request rate) live in `bench/scenarios/*.yaml`.
- Never invent or estimate results. If something was not measured, write "not measured". Label assumptions as assumptions.

## Observability

- Prometheus metrics prefixed `ride_`: `ride_ping_write_seconds`, `ride_match_seconds`, `ride_eta_seconds` (histograms), `ride_eta_cache_total{result}`, `ride_claim_conflicts_total`, `ride_stale_pings_dropped_total`, `ride_kafka_publish_errors_total`, `ride_online_drivers` (gauge).
- Every service exposes `/metrics` and `/healthz`.

## Testing

- Table-driven tests with the standard `testing` package. Use `t.Parallel()` where safe and `go-cmp` for diffs.
- Pure logic (geo, ranking, surge math) has unit tests with no network.
- Integration tests carry `//go:build integration`.
- Every Lua script has tests for: stale driver purge, cell change, and claim contention (N goroutines claim one driver, exactly one wins).
- A bug fix starts with a failing test.

## Git

- Conventional commits (`feat:`, `fix:`, `perf:`, `test:`, `docs:`). Small, focused commits.
- Do not push unless asked. Do not amend published commits. Never use `--no-verify`.

## Definition of done

- `go build ./...`, `go vet ./...` and `go test -short ./...` pass.
- New behavior has tests. New hot-path code has metrics and a benchmark.
- Docs are updated when a Redis key, topic, metric or env var changes.
- No TODO without an owner or issue.

## Never do

- Edit `go.sum`, generated files (`*.pb.go`, `*_gen.go`, anything marked `Code generated ... DO NOT EDIT.`), `.env` files or `bench/results/*`.
- Write to Postgres from the ping path, or put Kafka in front of Redis on the hot path.
- Commit secrets. Configuration comes from the environment.
- Run destructive commands (`rm -rf`, `git reset --hard`, `docker compose down -v`, `FLUSHALL`, `DROP TABLE`). Hooks block most of them. Ask the user instead.

## How to work

- Read the code before editing. Make the smallest change that solves the problem.
- Plan first (plan mode) for anything touching Lua scripts, claim logic or the Redis key schema.
- When something fails, work from the exact failing command and its actual output. Do not guess. Ask for the console output when it is missing.
- When showing code in chat, give complete, copy-paste-ready files. Never use "rest unchanged" placeholders.
- State plainly what is measured and what is assumed.
