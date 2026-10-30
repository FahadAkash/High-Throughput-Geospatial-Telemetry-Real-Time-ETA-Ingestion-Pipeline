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
| **Phase 1-5: Full Project Implementation & Verification** | 2026-10-30T05:17:00Z | 525,461 | 8,348,900 | 76,399 | 8,950,760 | \$0.25463 | \$0.29183 |

### Cost Breakdown as of Latest Milestone
- **Standard Input Cost**: $525,461 \times \$0.00000010 = \$0.05255$
- **Cache Read Cost**: $8,348,900 \times \$0.000000025 = \$0.20872$
- **Output (Reasoning + Generation) Cost**: $76,399 \times \$0.00000040 = \$0.03056$
- **Total Cumulative Cost**: **\$0.29183 USD**

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
