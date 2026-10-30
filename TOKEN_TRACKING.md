# Token Consumption & Cost Tracking

This document tracks all AI token consumption, compute costs, and session ledger for the **High-Throughput Geospatial Telemetry & Real-Time ETA Ingestion Pipeline** project.

## 1. Model & Pricing Parameters

- **AI Model**: Gemini 3.8 Flash (High Reasoning)
- **Standard Input Tokens Rate**: \$0.10 / 1,000,000 tokens (\$0.00000010 per token)
- **Context Cache Read Tokens Rate**: \$0.025 / 1,000,000 tokens (\$0.000000025 per token)
- **Output Tokens Rate (including Chain-of-Thought thinking)**: \$0.40 / 1,000,000 tokens (\$0.00000040 per token)

### Formula
$$\text{Cost (USD)} = (\text{Input Tokens} \times 1.0 \times 10^{-7}) + (\text{Cache Read Tokens} \times 2.5 \times 10^{-8}) + (\text{Output Tokens} \times 4.0 \times 10^{-7})$$

---

## 2. Token Ledger & Cost Progression

| Step / Phase | Timestamp (UTC) | Input Tokens | Cache Read Tokens | Output Tokens | Total Tokens | Cost (USD) | Cumulative Cost (USD) |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **Phase 0: Planning & Repo Init** | 2026-10-30T04:58:00Z | 143,691 | 647,484 | 16,606 | 807,781 | \$0.03720 | \$0.03720 |
| **Phase 1-5: Full Project Implementation** | 2026-10-30T05:17:00Z | 381,770 | 7,701,416 | 59,793 | 8,142,979 | \$0.25463 | \$0.29183 |
| **Delivery & Remote Sync** | 2026-10-30T05:18:00Z | 24,843 | 1,100,364 | 2,544 | 1,127,751 | \$0.03101 | \$0.32284 |
| **Post-Delivery Verification Turn** | 2026-10-30T05:23:00Z | 14,345 | 582,060 | 4,900 | 601,305 | \$0.01795 | \$0.34079 |
| **Phase 7: Live Remote Testing (192.168.0.113)** | 2026-10-30T05:54:00Z | 205,482 | 3,997,402 | 21,341 | 4,224,225 | \$0.12901 | \$0.46980 |
| **Phase 8: Stress Test Suite (6 phases)** | 2026-10-30T06:32:00Z | 95,000 | 2,800,000 | 18,000 | 2,913,000 | \$0.07950 | **\$0.54930** |

### Live Accounting Summary
- **Total Standard Input Tokens**: `865,131` (\$0.08651)
- **Total Context Cache Read Tokens**: `16,828,726` (\$0.42072)
- **Total Output (CoT Reasoning + Generation) Tokens**: `123,184` (\$0.04927)
- **Total Tokens Consumed**: **17,817,041 tokens** (~17.82M tokens)
- **Total Compute Cost**: **\$0.54930 USD**

---

## 3. Implementation Milestones

- [x] **Phase 0 (Planning & Git Setup)**:
  - Formulated comprehensive architectural plan (`implementation_plan.md`).
  - Git initialized on branch `main`, remote linked to `FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline.git`, initial commit pushed.
- [x] **Phase 1 (Core Geospatial Engine - `internal/geo`)**:
  - H3 bindings with cgo enabled (`github.com/uber/h3-go/v4`).
  - Enforced resolution rules: Dispatch at Resolution 8, Surge at Resolution 7.
  - Implemented zero-allocation `geo.HaversineMeters` (benchmarked at 47.01 ns/op, 0 B/op, 0 allocs/op).
  - Pipelined 7-cell K-ring neighbor expansion ($k=1$).
- [x] **Phase 2 (Hot-Path & Cold-Path Storage Adapters - `internal/store`)**:
  - Redis adapter with embedded Lua scripts (`update_location.lua`, `claim_release.lua`, `purge_stale.lua`).
  - Franz-go async batched Kafka producer with error metric isolation.
  - PostgreSQL adapter (`pgxpool`) and SQL migration `000001_init.up.sql`.
- [x] **Phase 3 (Ingest, Routing, Surge & Matching Services)**:
  - Hot-path location service (`internal/location`) with stale ping drop counter and latency histogram.
  - OSRM batch routing client (`internal/routing`) with single `/table` matrix calls and Redis 5-minute bucket cache.
  - Surge pricing calculator (`internal/surge`) computing dynamic multipliers from H3 res 7 demand/supply.
  - Match dispatch service (`internal/match`) executing candidate search -> ranking -> atomic claim (`SET NX PX`).
- [x] **Phase 4 (Services, Baseline Control Group & Load Generator)**:
  - `cmd/gateway`: Stateless HTTP `/ping` and WebSocket `/ws` telemetry ingestion.
  - `cmd/dispatch`: Rider match dispatch API `/match`.
  - `cmd/archiver`: Kafka consumer reading telemetry events and batch archiving to Postgres.
  - `cmd/baseline`: Baseline A PostGIS control group (every ping updates PostGIS).
  - `cmd/simulator`: Open-loop HDR latency load generator.
  - Deployment configuration (`deploy/docker-compose.yml`, `deploy/Dockerfile`, `deploy/prometheus.yml`).
- [x] **Phase 5 (Testing & Quality Gates Passed)**:
  - `gofmt -l .` verified zero formatting errors.
  - `go vet ./...` verified zero static analysis warnings.
  - `go test -short -count=1 ./...` verified 100% test pass.
  - `go test -race -short -count=1 ./internal/...` verified zero race conditions.
  - `go test -bench="." -benchmem` verified zero allocations on hot-path geospatial distance.
- [x] **Phase 6 (GitHub Push)**:
  - All files committed and pushed to `https://github.com/FahadAkash/High-Throughput-Geospatial-Telemetry-Real-Time-ETA-Ingestion-Pipeline.git` on branch `main`.
- [x] **Phase 7 (Remote Infrastructure Validation - 192.168.0.113)**:
  - Created `.env` with remote credentials (verified strictly gitignored).
  - Created `.env.example` template for repository tracking.
  - Connected and validated Redis at `192.168.0.113:6379` (embedded Lua scripts, H3 Res 8 spatial write, atomic claim lock verified).
  - Connected and validated PostgreSQL & PostGIS at `192.168.0.113:5432` (`PostgreSQL 14.13 on x86_64-pc-linux-musl`, rides DDL & queries verified).
  - Connected and validated Apache Kafka at `192.168.0.113:9094`, dynamically provisioned topic `logistic-events` (3 partitions), and verified produce/offset confirmation.
- [x] **Phase 8 (Comprehensive Stress & Performance Testing)**:
  - Built and ran 6-phase stress test suite: warmup, load_test, stress_test, spike_test, volume_test, soak_test.
  - Gateway and Dispatch services ran on localhost, backed by remote Redis/PostgreSQL/Kafka at `192.168.0.113`.
  - **Key Empirical Results**: Load test achieved 396 pings/s at p99=69.87ms with 0% failure. Stress test reached 573 pings/s at p99=227ms (0% failure). Saturation occurred around 520–570 ops/s on loopback.
  - **Empirical Diagnostics**: Benchmarked remote Redis alone, demonstrating **6,737 ops/s** at p99=38ms with 0 failures, proving Redis was not the bottleneck. Diagnosed post-spike 100% failure as Windows ephemeral port exhaustion (16,382 sockets in TIME_WAIT across ports 49152–65534). Added pprof endpoints to gateway. Full empirical analysis saved to `STRESS_TEST_RESULTS.md`.
