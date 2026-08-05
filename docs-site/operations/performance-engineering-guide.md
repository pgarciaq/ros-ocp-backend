# Performance Engineering Guide

> **Last verified:** 2026-08-05

Operational guidance for sizing, tuning, and monitoring **ROS-OCP-Backend**
(the OpenShift resource optimization service). This document covers the Go
processor, API, and PostgreSQL data path only.

For the full internal audit (code references, risk register, scaling cliffs),
see [`docs/audits/performance-scalability-analysis.md`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/docs/audits/performance-scalability-analysis.md).

**Last updated:** 2026-06-16

---

## Quick diagnosis

| Symptom | Likely cause | First action |
|---------|--------------|--------------|
| Optimizations page slow or timing out | PostgreSQL undersized or `work_mem` too low | Increase DB memory; see [Database sizing](#database-sizing) |
| Recommendations stale after ingest | Kafka lag or processor crash loop | Check processor logs; Kafka consumer lag |
| `too many connections` in ROS logs | Pool × replicas exceeds PostgreSQL limit | Lower replicas or raise `max_connections`; tune `ROS_DB_MAX_CONNS` |
| Pool acquire timeouts (5s) | Connection starvation | Check `rosocp_db_pool_acquired_conns`; reduce concurrent load |
| API 504 on savings / fleet summary | Query exceeds gateway timeout | Auto-lowered in SaaS (28s); on-prem behind gateway: set `ROS_HEAVY_API_STATEMENT_TIMEOUT_MS` = gateway−2000; increase DB `work_mem` |
| Processor OOMKilled | Heap spike during large manifest | Set `GOMEMLIMIT` to ~90% of memory limit; increase processor memory |
| Ingest never completes | Single Kafka partition bottleneck | Increase topic partitions; scale processor replicas |
| All savings `$0.00` | Masu unreachable or cost model empty | Verify `KOKU_MASU_URL`; check masu logs |
| Namespace tab slower than container tab | Expected — namespace list uses heavier SQL | Track P1 backlog; use filters to narrow results |

---

## Architecture at a glance

ROS-OCP-Backend runs as **separate Deployments** (same container image, different commands):

| Deployment | Command | Role |
|------------|---------|------|
| **ros-processor** | `rosocp start processor` | Consumes Kafka, downloads CSVs, writes digests, runs recommendation engine |
| **ros-api** | `rosocp start api` | Serves REST API to the UI |
| **ros-housekeeper** | `rosocp start housekeeper` | Partition retention, Sources cleanup (cron-style) |

Data flow:

```
Cluster operator → Ingress upload → Kafka (hccm.ros.events)
    → ros-processor → PostgreSQL (digests + recommendation_sets)
    → ros-api → UI
```

The **native Go engine** (default) computes recommendations from daily digest
tables. Kruize is legacy; disable it in on-prem for best performance
(`ROS_ENABLED_PLUGINS` without `kruize`).

---

## Deployment modes

### On-prem (cost-onprem Helm chart)

- ROS shares a **single PostgreSQL instance** with Koku, masu, and other services.
- RBAC may be disabled; CSV downloads use internal MinIO/S3 URLs on `ROS_CSV_ALLOWED_HOSTS`.
- Set **`GOMEMLIMIT`** via chart value `ros.goMemLimit` (recommended ~90% of pod memory limit).
- Chart PostgreSQL defaults (512Mi, `work_mem=4MB`) are **demo only** — see [Database sizing](#database-sizing).

### SaaS (console.redhat.com / Clowder)

- Database, Kafka, and RBAC endpoints injected by Clowder.
- RBAC cache (`ROS_RBAC_CACHE_TTL=60`) is important — every list call would otherwise hit RBAC HTTP.
- Ingress/gateway timeout ≈ **30 seconds** — `ROS_HEAVY_API_STATEMENT_TIMEOUT_MS` defaults
  to **28000ms** automatically when Clowder is detected (`ACG_CONFIG` set). No manual
  configuration needed unless the gateway budget changes. Formula: `gateway_budget - 2000ms`
  (accounts for serialization, TCP delivery, and safety margin).

---

## Database sizing

ROS is **PostgreSQL-bound** for both ingest (batched upserts) and API (keyset list queries).

### Minimum production profile (shared on-prem DB, up to ~5k containers)

| Parameter | Demo default | Recommended start |
|-----------|--------------|-------------------|
| PostgreSQL memory | 512Mi | **4Gi+** |
| `max_connections` | 100 | **200+** |
| `work_mem` | 4MB | **16MB–64MB** |
| PVC | 30Gi | **100Gi+** (depends on retention) |

Connection budget formula:

```
ROS connections ≈ ROS_DB_MAX_CONNS × (ros-api pods + ros-processor pods + housekeeper)
Total PG connections ≈ ROS + Koku API + Koku workers + masu + overhead
Keep total < 70% of max_connections
```

Default **`ROS_DB_MAX_CONNS=10`** per process (`internal/config/config.go`). Example: 3 API + 2 processor pods → 50 ROS connections before housekeeper.

### Retention and disk

| Setting | Default | Effect |
|---------|---------|--------|
| `ROS_RETENTION_MONTHS` | 6 | Daily digest partitions |
| `ROS_SAMPLE_RETENTION_DAYS` | 45 | Raw `container_usage_samples` (optional; digests power UI plots) |
| `ROS_HISTORY_RETENTION_DAYS` | 90 | Recommendation history/quality tables |

Housekeeper drops old partitions — ensure cron/`housekeeper --partitions` runs on schedule.

---

## Knobs that matter

### Processor / ingest

| Variable | Default | When to change |
|----------|---------|----------------|
| `ROS_KAFKA_PARALLEL` | `true` | Set `false` only for debugging ordering issues |
| `ROS_KAFKA_WORKERS` | `3` | Match expected partition parallelism; rarely need >5 |
| `ROS_DB_INGEST_STATEMENT_TIMEOUT` | `120` (seconds) | Raise for very large single-file CSVs on slow storage |
| `ROS_INGEST_FLUSH_BATCH_SIZE` | `math.MaxInt32` (disabled) | Intermediate flushes are disabled by default — they degrade recommendation quality by computing percentiles from insufficient samples. Lower only if the pod OOMs during ingest (unlikely: upstream file caps bound memory to ~22–115 MB). |
| `ROS_INGEST_STRICT_ANALYTICS` | `true` | Set `false` to commit recommendations even if history/quality writes fail (degraded mode) |
| `ROS_CSV_MAX_BODY_BYTES` | 524288000 (500 MiB) | Lower if ingress should reject oversized payloads earlier |
| `ROS_CSV_ALLOWED_HOSTS` | (empty) | **Required in production** — comma-separated download host allowlist |
| `ROS_SYNTH_MANIFEST_QUIET_PERIOD` | `30` (seconds) | Debounce for synthesized manifest IDs |

### API

| Variable | Default | When to change |
|----------|---------|----------------|
| `ROS_DB_STATEMENT_TIMEOUT` | `25` (seconds) | API session timeout; rarely change |
| `ROS_HEAVY_API_STATEMENT_TIMEOUT_MS` | `45000` | **Set `28000` on SaaS** to fit ingress budget |
| `ROS_API_MAX_OFFSET` | `10000` | Cap on offset pagination depth |
| `ROS_RBAC_CACHE_TTL` | `60` (seconds) | `0` disables RBAC cache (dev only) |
| `ROS_RBAC_CACHE_MAX_ENTRIES` | `500` | Raise for many concurrent users per org |
| `ROS_FLEET_SUMMARY_CACHE_TTL` | `300` (seconds) | Fleet summary, heatmap, and savings LRU TTL |
| `ROS_FLEET_SUMMARY_CACHE_CAPACITY` | `256` | Max cached fleet/savings summary entries (~1 KB each) |
| `ROS_FLEET_HEATMAP_CACHE_CAPACITY` | `128` | Max cached fleet heatmap entries (~200 B × `ROS_FLEET_HEATMAP_MAX_NODES` each; 128 × 1000 ≈ 25 MB) |

### Recommendation engine

| Variable | Default | When to change |
|----------|---------|----------------|
| `ROS_MAX_LOOKBACK_DAYS` | `90` | Digest query window for recommendations |
| `ROS_THRESHOLD_RECALC_CONCURRENCY` | `3` | Parallel clusters on settings change; raise cautiously |
| `ROS_RESHIP_CONCURRENCY` | `2` | Business-hours masu reship fan-out |
| `ROS_STALENESS_THRESHOLD_HOURS` | `48` | When recs marked stale |

Per-term overrides: `ROS_TERMS_<PLUGIN>_<TERM>_WINDOW_DAYS` etc. — see [Configurability Reference](../architecture/configurability.md).

### Connection pool (all processes)

| Variable | Default | When to change |
|----------|---------|----------------|
| `ROS_DB_MAX_CONNS` | `10` | Raise further **only** if PG headroom exists and pool metrics show sustained acquisition wait |
| `ROS_DB_MIN_CONNS` | `2` | Warm connections; costs baseline PG slots |
| `ROS_DB_ACQUIRE_TIMEOUT_SECS` | `5` | `0` = wait forever (not recommended) |
| `ROS_DB_MAX_CONN_LIFETIME` | `30` (minutes) | |
| `ROS_DB_STATEMENT_CACHE_MODE` | `describe` | pgx prepared-statement caching |

Full list: [Configuration — Performance Tuning](../configuration.md#performance-tuning).

---

## Go runtime memory

| Variable | Recommendation |
|----------|----------------|
| `GOMEMLIMIT` | **~90% of container memory limit**, e.g. `922MiB` for a 1Gi limit. Use Go syntax (`MiB`), not Kubernetes `Mi`. |
| GOMAXPROCS | Automatic via `automaxprocs` (imported in `cmd/start.go`) |

Without `GOMEMLIMIT`, large manifest processing can OOMKill the processor before GC catches up.

---

## Kafka tuning

| Setting | Value | Notes |
|---------|-------|-------|
| `KAFKA_AUTO_COMMIT` | `false` (default) | Manual commit after successful processing — do not enable without understanding duplicate handling |
| `KAFKA_CONSUMER_GROUP_ID` | `ros-ocp` | One group per environment |
| `ROS_KAFKA_MAX_TRANSIENT_RETRIES` | `5` | Then message goes to DLQ |
| `ROS_KAFKA_DLQ_TOPIC` | `hccm.ros.events.dlq` | Monitor for poison messages |
| Topic partitions | ≥ processor replicas | Required for horizontal scale |

**Scaling processors:** Adding pods without adding Kafka partitions does not increase throughput.

---

## What to monitor

Prometheus metrics on **`PROMETHEUS_PORT`** (default **5005**), path `/metrics`.

### Golden signals

| Metric | What it tells you |
|--------|-------------------|
| `rosocp_pipeline_total_duration_seconds` | End-to-end manifest processing time |
| `rosocp_pipeline_phase_duration_seconds{phase=...}` | Which phase is slow (download, recommend, write_digests, …) |
| `rosocp_recommendation_duration_seconds{type="container"}` | Engine compute time per rec type |
| `rosocp_db_pool_acquired_conns` / `rosocp_db_pool_max_conns` | Pool saturation |
| `rosocp_db_pool_acquire_duration_seconds` | Cumulative wait for connections |
| `rosocp_kafka_messages_processed_total` | Throughput |
| `rosocp_kafka_dlq_messages_total` | Poison / exhausted retry messages |
| `rosocp_api_statement_timeout_cancellations_total` | Queries killed by timeout |
| `rosocp_echo_request_duration_seconds` | API latency by route template |

### Cache health

| Metric | Healthy pattern |
|--------|-----------------|
| `rosocp_fleet_summary_cache_hits_total` / misses | Hit ratio >60% on steady-state UI traffic |
| `rosocp_rbac_cache_size` | Stable below max entries |
| `rosocp_cost_cache_size` | Stable; evictions occasional |

### Health endpoints

| Path | Use |
|------|-----|
| `/healthz` | Liveness — goroutine count, GC pause, scheduler |
| `/readyz` | Readiness — PostgreSQL ping |
| `/status` | Basic app status |

Alert if `/healthz` reports `goroutines` > `ROS_HEALTHZ_MAX_GOROUTINES` (5000) or GC pause > `ROS_HEALTHZ_MAX_GC_PAUSE_MS` (100).

### External alerts (not exported by ROS)

- **Kafka consumer lag** (Strimzi / burrow / broker JMX)
- **PostgreSQL**: connections, disk, `pg_stat_user_tables.n_dead_tup`, long-running queries
- **Ingress 5xx rate** on ROS API routes

See also [Monitoring](../monitoring.md) and [Query Performance](../query-performance.md).

---

## Capacity planning

### Rough ingest throughput

Single processor pod on Medium DB, native engine, 3 Kafka workers:

| Cluster size | Containers | Typical manifest time (order of magnitude) |
|--------------|------------|------------------------------------------|
| Small | <1k | 1–3 min |
| Medium | 1k–5k | 3–10 min |
| Large | 5k–15k | 10–30 min |
| Very large | 15k+ | 30+ min — validate with `rosocp_pipeline_total_duration_seconds` |

Times vary with CSV size, enabled plugins (GPU, VM, snapshot), and business-hours dual-stream ingest.

### API capacity

- Default container list uses **`org_container_keys`** — designed for fleets up to tens of thousands of containers with keyset pagination.
- **Fleet summary** and **savings summary** are cached 5 minutes per org — first load after cache miss is expensive.
- Plan **1 API pod per ~50 concurrent UI users** as a starting point; scale on `rosocp_echo_request_duration_seconds` p95.

### When to add resources

| Signal | Scale up |
|--------|----------|
| Kafka lag growing linearly | Processor replicas **and** topic partitions |
| `rosocp_db_pool_acquired_conns` near max sustained | `ROS_DB_MAX_CONNS` or PG `max_connections` |
| Recommend phase >50% of pipeline time | Processor CPU limit |
| API p95 >2s on list routes | API replicas + PostgreSQL `work_mem`/IOPS |
| Processor OOMKilled | Memory limit + `GOMEMLIMIT` |

---

## Troubleshooting workflows

### 1. Ingest stuck / Kafka lag

```bash
# Processor logs (replace label selector for your chart)
kubectl logs -n cost-onprem -l app.kubernetes.io/component=ros-processor --tail=100 | grep -iE 'error|failed|transient'

# Check pipeline phase timing in metrics
# rosocp_pipeline_phase_duration_seconds — which phase dominates?
```

- Transient DB errors → message requeued (up to 5 retries)
- After retries → DLQ topic; fix root cause and replay manually
- Context canceled on rollout → normal; lag should recover

### 2. API slow

1. Check `rosocp_api_statement_timeout_cancellations_total` — if rising, DB tuning needed
2. Check whether query uses **namespace** or **stale** filter (heavier paths)
3. Flush is not needed — ROS has no Redis API cache; fleet summary LRU expires in 5m
4. Verify RBAC cache hit rate if RBAC enabled

### 3. Connection exhaustion

```promql
rosocp_db_pool_acquired_conns / rosocp_db_pool_max_conns > 0.8
```

Reduce replicas or increase PostgreSQL `max_connections`. Do not raise `ROS_DB_MAX_CONNS` without headroom.

### 4. Post cost-model change slowdown

Koku triggers **`POST /internal/recalculate-savings`** — fans out per cluster with coalescing. Expect temporary DB write load. Monitor `rosocp_savings_recalc_*` counters if exposed.

---

## Security vs performance

- **`ROS_CSV_ALLOWED_HOSTS`** must be set in production (fail-closed). Each allowed host skips private-IP DNS deny when matched.
- **`ROS_CSV_DENY_PRIVATE_NETWORKS=true`** (default) blocks SSRF to internal networks for non-allowlisted hosts.
- Internal endpoints (`/internal/tags/sync`, `/internal/recalculate-savings`) should be NetworkPolicy-restricted — not performance-related but critical in multi-tenant SaaS.

---

## Upgrade / rollout notes

- Processor SIGTERM waits up to **`ROS_SHUTDOWN_TIMEOUT_SECONDS`** (30s) for in-flight Kafka handlers before exit.
- After upgrade, **Kafka lag** may spike while consumers rebalance — temporary.
- Schema migrations run separately (`rosocp db migrate`); plan maintenance window for large DDL on big tables.
- See [Upgrade Runbook](upgrade-runbook.md) for ordered steps.

---

## Related documentation

- [Configuration Reference](../configuration.md) — all environment variables
- [Query Performance](../query-performance.md) — API query patterns and indexes
- [Monitoring](../monitoring.md) — Grafana dashboards and alert rules
- [Architecture: Recommendation Engines](../architecture/recommendation-engines.md) — engine behavior
