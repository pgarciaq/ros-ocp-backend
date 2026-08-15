# librobne extract baseline (`841639f3`)

Frozen **P0.5** numbers for GitHub [#94](https://github.com/pgarciaq/ros-ocp-backend/issues/94).
Later phases compare **against these files**, not against the older table in
[`docs/native-engine-performance.md`](../../native-engine-performance.md).

Engine compute at this SHA is identical to phase17 HEAD at P0
(`0394902e`, docs-only). Recorded 2026-08-15 on the developer laptop
(22 cores, 62 GiB RAM, Podman 5.8.4). See `environment.txt`.

## Gate table (`cmd/bench`, all-in-memory)

Default path: `RecommendAllWorkloads` then `WriteRecommendationsAndRefreshOrg`.

| Containers | Digest rows | Recs | Recommend (ms) | Write (ms) | List p50 (ms) | Peak Sys (MB) | File |
|------------|-------------|------|----------------|------------|---------------|---------------|------|
| 1,000 | 30,000 | 6,000 | 436 | 559 | 15.4 | 20.3 | `cmd-bench.txt` |
| 10,000 | 300,000 | 60,000 | 4,536 | 6,638 | 36.8 | 422.8 | `cmd-bench.txt` |
| 100,000 | 3,000,000 | 600,000 | 38,529 | 74,401 | 590.5 | 4,301 | `cmd-bench-100k.txt` |

**Use the 10k row as the extract gate** (Recommend/Write ≤5%, Peak ≤10%).
100k was recorded on this machine; it is optional on smaller boxes.

### P4 matched 10k (2026-08-15, same laptop)

All-in-memory `cmd/bench` after the nested module move (`ROS_BENCH_STREAM` unset,
`ROS_MAX_DIGEST_ROWS_PER_CLUSTER=0`):

| Containers | Recommend (ms) | Write (ms) | Peak Sys (MB) | vs 841639f3 Recommend | vs Peak |
|------------|----------------|------------|---------------|----------------------|---------|
| 1,000 | 942 | 636 | 36.3 | noisy (cold 1k) | — |
| 10,000 | 4,261 | 8,440 | 365.7 | −6.1% (pass ≤5% slower) | −13.5% (pass ≤10% fatter) |

Write is noisy (documented). Rec count 60,000 matches. Copy detector: Peak Sys
dropped vs 422.8 MB (no second `[]DigestRow`).

These runs are **slower / fatter** than the published native-engine table
(10k: 857 ms / 176 MB). Do not mix the two. Likely contributors: 30 days of
seed data (not 14), `MemStats.Sys` vs true RSS, and a different Go toolchain
(`go1.26.5`).

## What “Peak RSS” in this harness actually is

The column is `runtime.MemStats.Sys` **delta** around recommend (kept as-is so
the harness matches historical `cmd/bench` output). It is **not** `/proc`
RSS and **not** PostgreSQL’s memory.

## Harness fixes (not engine math)

Do **not** treat these as native-engine bugs:

1. **Migration 000179** `ALTER`s `node_recommendation_history`, but no migration
   `CREATE TABLE`s it. Fresh testcontainers PostgreSQL fails `migrate.Up()`.
   Workaround: stub the table **before** migrate in `cmd/bench` and
   `internal/testutil/testdb.go`. Do not edit shipped `000179` (round-trip tests).
2. **`createPartitions`** used to create only `now±3` months. Seed dates are
   hardcoded `2026-03-01` plus 30 days. In August 2026 that month has no
   partition → 0 digest rows and fake 0 ms / 0 RSS. Partitions now include the
   seed month.
3. List `OrderBy: "cluster"` is an API key. `GetNativeRecommendations` needs the
   mapped column `clusters.cluster_alias`.
4. Default `ROS_MAX_DIGEST_ROWS_PER_CLUSTER=500000` truncates 100k × 30 days
   (3M rows). Baseline runs use `ROS_MAX_DIGEST_ROWS_PER_CLUSTER=0`.
5. `go test -bench=... -count=10` **without** `-run '^$'` re-runs the whole
   engine test suite. Always pass `-run '^$'`.

## Streaming path (production-shaped RSS)

```bash
ROS_BENCH_STREAM=1 ROS_MAX_DIGEST_ROWS_PER_CLUSTER=0 \
  go run ./cmd/bench/ 1000,10000
```

Uses `RecommendWorkloadsStreaming` and writes each 500-container batch in
`emit`. **Recommend (ms) includes persist**; Write prints as `in-emit`.
Peak Sys includes write buffers, so **do not** use stream Peak vs the
all-in-memory 422.8 MB as the §8.6 RSS gate. Stream numbers live in
`cmd-bench-stream.txt` (telemetry for production RSS, not the copy-detection gate).

| Containers | Recs | Recommend+write (ms) | List p50 (ms) | Peak Sys (MB) |
|------------|------|----------------------|---------------|---------------|
| 1,000 | 6,000 | 1,141 | 14.4 | 41.0 |
| 10,000 | 60,000 | 16,212 | 36.1 | 385.1 |

10k all-in-memory was 4,536 + 6,638 = 11,174 ms recommend-then-write and 422.8 MB
(recs retained). Stream is slower wall-clock (writes interleaved) and slightly
leaner Sys. Rec counts match. Detail is not comparable (one sample rec).

## `go test` benches

Raw log: `go-test-bench.txt` (includes config WARN noise).

benchstat-friendly lines: `go-test-bench.clean.txt`.

```bash
go test -run '^$' \
  -bench='BenchmarkSavingsCalculation_1000Containers$|BenchmarkNodeSavings_100Nodes$|BenchmarkDualRecommendation_Overhead$|BenchmarkThresholdResolution_SingleOrg$' \
  -benchmem -count=10 -timeout 30m ./internal/engine/
```

`BenchmarkThresholdResolution_SingleOrg` logs on every iteration and splits
the Go bench line; the clean file stitches name + `ns/op` back together.

## Compute-only canary

First on-disk recording is **P4** (2026-08-15): `compute-only-bench.txt`.
P3 introduced `BenchmarkRecommendWorkloads_ComputeOnly`; the file was written
when the nested module landed. Parent wrapper and `librobne/engine` share
allocs/op (14006 at 1k, 140006 at 10k) — copy detector pass.

```bash
go test -run '^$' -bench='BenchmarkRecommendWorkloads_ComputeOnly$' \
  -benchmem -count=6 ./internal/engine/
go test -C librobne -run '^$' -bench='BenchmarkRecommendWorkloads_ComputeOnly$' \
  -benchmem -count=6 ./engine/
```

ns/op is laptop-noisy; use **allocs/op** and **B/op** as the copy gate (≤2%).
The official extract gate remains **10k `cmd/bench`** vs the table above.

## Re-run

```bash
export DOCKER_HOST=unix:///run/user/$(id -u)/podman/podman.sock
export TESTCONTAINERS_RYUK_DISABLED=true
export ROS_MAX_DIGEST_ROWS_PER_CLUSTER=0

# all-in-memory gate (default)
go run ./cmd/bench/ 1000,10000

# 100k (optional; several minutes, multi-GiB Sys)
go run ./cmd/bench/ 100000
```
