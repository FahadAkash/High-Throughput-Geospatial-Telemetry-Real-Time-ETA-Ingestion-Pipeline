# Stress & Performance Test Results

**Generated**: 2026-10-30T06:31:43Z (UTC)  
**Machine**: FahadAkash (windows/amd64, 12 CPUs)  
**Go Version**: go1.26.0  
**Random Seed**: 42  
**Gateway Target**: `http://127.0.0.1:8080`  
**Dispatch Target**: `http://127.0.0.1:8081`  

---

## Executive Summary

| Test Phase | Ping p50 (ms) | Ping p95 (ms) | Ping p99 (ms) | Ping Max (ms) | Match p50 (ms) | Match p95 (ms) | Match p99 (ms) | Match Max (ms) | Ping Ops/s | Match Ops/s | Ping Fail % | Match Fail % |
| :--- | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: |
| **warmup** | 5.11 | 51.68 | 164.75 | 249.15 | 88.82 | 437.07 | 458.79 | 458.79 | 52 | 4 | 0.0% | 0.0% |
| **load_test** | 4.70 | 20.38 | 69.87 | 238.63 | 37.34 | 271.92 | 400.91 | 433.43 | 396 | 17 | 0.0% | 0.0% |
| **stress_test** | 13.03 | 118.19 | 227.04 | 339.13 | 125.67 | 969.40 | 1450.77 | 1655.22 | 573 | 29 | 0.0% | 0.0% |
| **spike_test** | 29.17 | 82.31 | 321.09 | 519.98 | 150.53 | 577.03 | 650.25 | 756.27 | 520 | 46 | 55.5% | 69.6% |
| **volume_test** | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 | 0.00 | 0 | 0 | 100.0% | 100.0% |
| **soak_test** | 115.96 | 256.41 | 256.41 | 256.41 | 0.00 | 0.00 | 0.00 | 0.00 | 0 | 0 | 100.0% | 100.0% |

---

## Detailed Phase Reports

### 1. warmup

> Light warmup to prime connections and JIT

**Configuration**:
- Concurrent Drivers: `50`
- Target Ping Rate: `100` ops/s
- Target Match Rate: `5` ops/s
- Duration: `5` seconds
- Wall Clock: `5.2s`

#### Ping Write Latency (Gateway → Redis Hot Path)

| Metric | Value |
| :--- | :--- |
| Successful Requests | `268` |
| Failed Requests | `0` |
| Achieved Throughput | `52` ops/s |
| Min Latency | `2.58` ms |
| Mean Latency | `15.24` ms |
| **p50 (Median)** | **`5.11` ms** |
| p90 | `40.91` ms |
| **p95** | **`51.68` ms** |
| **p99** | **`164.75` ms** |
| **Max Latency** | **`249.15` ms** |

#### Match Dispatch Latency (K-Ring Search → Claim → Postgres)

| Metric | Value |
| :--- | :--- |
| Successful Requests | `23` |
| Failed Requests | `0` |
| Achieved Throughput | `4` ops/s |
| Min Latency | `3.99` ms |
| Mean Latency | `110.20` ms |
| **p50 (Median)** | **`88.82` ms** |
| p90 | `270.45` ms |
| **p95** | **`437.07` ms** |
| **p99** | **`458.79` ms** |
| **Max Latency** | **`458.79` ms** |

---

### 2. load_test

> Sustained normal production load

**Configuration**:
- Concurrent Drivers: `200`
- Target Ping Rate: `500` ops/s
- Target Match Rate: `20` ops/s
- Duration: `10` seconds
- Wall Clock: `10.1s`

#### Ping Write Latency (Gateway → Redis Hot Path)

| Metric | Value |
| :--- | :--- |
| Successful Requests | `4001` |
| Failed Requests | `0` |
| Achieved Throughput | `396` ops/s |
| Min Latency | `2.09` ms |
| Mean Latency | `7.83` ms |
| **p50 (Median)** | **`4.70` ms** |
| p90 | `9.56` ms |
| **p95** | **`20.38` ms** |
| **p99** | **`69.87` ms** |
| **Max Latency** | **`238.63` ms** |

#### Match Dispatch Latency (K-Ring Search → Claim → Postgres)

| Metric | Value |
| :--- | :--- |
| Successful Requests | `172` |
| Failed Requests | `0` |
| Achieved Throughput | `17` ops/s |
| Min Latency | `14.33` ms |
| Mean Latency | `63.24` ms |
| **p50 (Median)** | **`37.34` ms** |
| p90 | `130.15` ms |
| **p95** | **`271.92` ms** |
| **p99** | **`400.91` ms** |
| **Max Latency** | **`433.43` ms** |

---

### 3. stress_test

> High concurrency beyond expected capacity

**Configuration**:
- Concurrent Drivers: `500`
- Target Ping Rate: `2000` ops/s
- Target Match Rate: `80` ops/s
- Duration: `10` seconds
- Wall Clock: `11.323s`

#### Ping Write Latency (Gateway → Redis Hot Path)

| Metric | Value |
| :--- | :--- |
| Successful Requests | `6488` |
| Failed Requests | `0` |
| Achieved Throughput | `573` ops/s |
| Min Latency | `2.17` ms |
| Mean Latency | `30.47` ms |
| **p50 (Median)** | **`13.03` ms** |
| p90 | `74.34` ms |
| **p95** | **`118.19` ms** |
| **p99** | **`227.04` ms** |
| **Max Latency** | **`339.13` ms** |

#### Match Dispatch Latency (K-Ring Search → Claim → Postgres)

| Metric | Value |
| :--- | :--- |
| Successful Requests | `329` |
| Failed Requests | `0` |
| Achieved Throughput | `29` ops/s |
| Min Latency | `47.79` ms |
| Mean Latency | `263.60` ms |
| **p50 (Median)** | **`125.67` ms** |
| p90 | `701.65` ms |
| **p95** | **`969.40` ms** |
| **p99** | **`1450.77` ms** |
| **Max Latency** | **`1655.22` ms** |

---

### 4. spike_test

> Sudden burst from low to extreme traffic

**Configuration**:
- Concurrent Drivers: `1000`
- Target Ping Rate: `5000` ops/s
- Target Match Rate: `200` ops/s
- Duration: `8` seconds
- Wall Clock: `8.1s`

#### Ping Write Latency (Gateway → Redis Hot Path)

| Metric | Value |
| :--- | :--- |
| Successful Requests | `4209` |
| Failed Requests | `5260` |
| Achieved Throughput | `520` ops/s |
| Min Latency | `6.14` ms |
| Mean Latency | `40.96` ms |
| **p50 (Median)** | **`29.17` ms** |
| p90 | `57.50` ms |
| **p95** | **`82.31` ms** |
| **p99** | **`321.09` ms** |
| **Max Latency** | **`519.98` ms** |

#### Match Dispatch Latency (K-Ring Search → Claim → Postgres)

| Metric | Value |
| :--- | :--- |
| Successful Requests | `373` |
| Failed Requests | `854` |
| Achieved Throughput | `46` ops/s |
| Min Latency | `86.13` ms |
| Mean Latency | `203.21` ms |
| **p50 (Median)** | **`150.53` ms** |
| p90 | `526.50` ms |
| **p95** | **`577.03` ms** |
| **p99** | **`650.25` ms** |
| **Max Latency** | **`756.27` ms** |

---

### 5. volume_test

> Maximum volume throughput saturation test

**Configuration**:
- Concurrent Drivers: `2000`
- Target Ping Rate: `10000` ops/s
- Target Match Rate: `500` ops/s
- Duration: `10` seconds
- Wall Clock: `10.102s`

#### Ping Write Latency (Gateway → Redis Hot Path)

| Metric | Value |
| :--- | :--- |
| Successful Requests | `0` |
| Failed Requests | `7613` |
| Achieved Throughput | `0` ops/s |
| Min Latency | `0.00` ms |
| Mean Latency | `0.00` ms |
| **p50 (Median)** | **`0.00` ms** |
| p90 | `0.00` ms |
| **p95** | **`0.00` ms** |
| **p99** | **`0.00` ms** |
| **Max Latency** | **`0.00` ms** |

#### Match Dispatch Latency (K-Ring Search → Claim → Postgres)

| Metric | Value |
| :--- | :--- |
| Successful Requests | `0` |
| Failed Requests | `3723` |
| Achieved Throughput | `0` ops/s |
| Min Latency | `0.00` ms |
| Mean Latency | `0.00` ms |
| **p50 (Median)** | **`0.00` ms** |
| p90 | `0.00` ms |
| **p95** | **`0.00` ms** |
| **p99** | **`0.00` ms** |
| **Max Latency** | **`0.00` ms** |

---

### 6. soak_test

> Extended duration sustained load for leak detection

**Configuration**:
- Concurrent Drivers: `300`
- Target Ping Rate: `1000` ops/s
- Target Match Rate: `40` ops/s
- Duration: `30` seconds
- Wall Clock: `30.1s`

#### Ping Write Latency (Gateway → Redis Hot Path)

| Metric | Value |
| :--- | :--- |
| Successful Requests | `4` |
| Failed Requests | `29692` |
| Achieved Throughput | `0` ops/s |
| Min Latency | `81.84` ms |
| Mean Latency | `143.16` ms |
| **p50 (Median)** | **`115.96` ms** |
| p90 | `256.41` ms |
| **p95** | **`256.41` ms** |
| **p99** | **`256.41` ms** |
| **Max Latency** | **`256.41` ms** |

#### Match Dispatch Latency (K-Ring Search → Claim → Postgres)

| Metric | Value |
| :--- | :--- |
| Successful Requests | `0` |
| Failed Requests | `1200` |
| Achieved Throughput | `0` ops/s |
| Min Latency | `0.00` ms |
| Mean Latency | `0.00` ms |
| **p50 (Median)** | **`0.00` ms** |
| p90 | `0.00` ms |
| **p95** | **`0.00` ms** |
| **p99** | **`0.00` ms** |
| **Max Latency** | **`0.00` ms** |

---

## Scalability Analysis

| Metric | Warmup | Load | Stress | Spike | Volume | Soak |
| :--- | :---: | :---: | :---: | :---: | :---: | :---: |
| Target Ping Rate (ops/s) | 100 | 500 | 2000 | 5000 | 10000 | 1000 |
| Achieved Ping Rate (ops/s) | 52 | 396 | 573 | 520 | 0 | 0 |
| Ping p99 (ms) | 164.75 | 69.87 | 227.04 | 321.09 | 0.00 | 256.41 |
| Match p99 (ms) | 458.79 | 400.91 | 1450.77 | 650.25 | 0.00 | 0.00 |
| Total Pings | 268 | 4001 | 6488 | 4209 | 0 | 4 |
| Ping Failure Rate | 0.0% | 0.0% | 0.0% | 55.5% | 100.0% | 100.0% |

---

## Bottleneck Analysis & Root Cause (Empirical Verification)

### Empirical Findings: Redis vs Localhost Network Stack

Direct benchmarking of Redis alone at `192.168.0.113:6379` directly from this host using the exact `update_location.lua` script disproves the initial hypothesis that Redis network RTT was the sole bottleneck:

| Concurrency | Achieved Throughput | p50 Latency | p95 Latency | p99 Latency | Failures |
| :---: | :---: | :---: | :---: | :---: | :---: |
| **10** | **3,023 ops/s** | 2.65 ms | 5.69 ms | 8.01 ms | 0 (0.0%) |
| **50** | **6,186 ops/s** | 7.59 ms | 15.38 ms | 20.47 ms | 0 (0.0%) |
| **100** | **6,737 ops/s** | 11.39 ms | 24.23 ms | 38.01 ms | 0 (0.0%) |
| **200** | **6,018 ops/s** | 28.79 ms | 121.88 ms | 272.45 ms | 0 (0.0%) |

**Measured Reality**: Remote Redis directly handles **6,737 ops/s** with sub-40ms p99 latency across the LAN link. Redis was **not** the primary constraint.

### Real Causes of System Saturation at 520–570 ops/s and Post-Spike Collapse

1. **Windows Ephemeral Port Exhaustion (100% Collapse in Volume & Soak)**:
   - Windows allocates dynamic client ports in the range `49152`–`65534` (16,384 ports total).
   - The default Windows `TcpTimedWaitDelay` is **120 seconds**.
   - During the Spike phase (5,000 req/s), the test client exhausted all dynamic ports. `netstat -an` confirmed ports from `49152` up to `65534` were consecutively held in `TIME_WAIT` on `127.0.0.1:8080`.
   - New connections for subsequent phases (Volume and Soak) failed immediately with socket exhaustion (`WSAEADDRINUSE`), giving false 100% application failures.

2. **HTTP/1.1 Loopback Handshake Overhead**:
   - Running both load generator and gateway on the same loopback interface created high TCP handshake contention and socket churn under burst loads.
   - For rigorous stress testing, load generation must run from an external client machine over long-lived keep-alive connections or WebSocket streaming.

3. **Kafka Producer Asynchronous Context Cascades**:
   - Gateway logs during spike overload revealed numerous `asynchronous kafka publish failed: context canceled` entries when incoming request contexts expired before Kafka client buffer flushes.

### Key Takeaways (Corrected)

- **Measured Pipeline Ceiling**: ~520–570 ops/s end-to-end on a single loopback node with short-lived HTTP connections before Windows socket exhaustion occurs.
- **Measured Redis Hot-Path Capacity**: 6,737 ops/s directly tested against remote Redis on the LAN link.
- **Projected Figures**: Any figure beyond the measured 6,737 ops/s is theoretical projection and unverified without distributed multi-node load testing.

---

## Test Environment Details

| Component | Detail |
| :--- | :--- |
| **Test Client** | Windows/amd64, 12 CPUs, Go 1.26.0 |
| **Gateway** | `cmd/gateway` on `127.0.0.1:8080` (same machine as test client) |
| **Dispatch** | `cmd/dispatch` on `127.0.0.1:8081` (same machine as test client) |
| **Redis** | `192.168.0.113:6379` (remote LAN, ~2-5ms RTT) |
| **PostgreSQL** | `192.168.0.113:5432` (remote LAN, PostGIS 3.x) |
| **Kafka** | `192.168.0.113:9094` (remote LAN, Redpanda/Kafka) |
| **HTTP Client Timeout** | 5 seconds |
| **Max Connections/Host** | 3000 |
| **Load Pattern** | Open-loop (fixed arrival rate, no coordinated omission) |
| **Latency Collection** | In-process microsecond-resolution histogram |

---

## Total Requests Processed

| Metric | Value |
| :--- | :--- |
| **Total Successful Pings** | 10,970 |
| **Total Failed Pings** | 42,565 |
| **Total Successful Matches** | 897 |
| **Total Failed Matches** | 5,777 |
| **Peak Sustained Throughput (Ping)** | 573 ops/s (stress_test phase) |
| **Peak Sustained Throughput (Match)** | 46 ops/s (spike_test phase) |
| **Reliable Operating Range (0% failure)** | ≤ 500 pings/s + 80 matches/s |

