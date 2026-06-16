# Cost Management Platform — Performance & Scalability Analysis

**Date:** 2026-06-16  
**Scope:** Koku backend, ROS-OCP-Backend, koku-metrics-operator, cost-onprem-chart  
**Modes:** On-prem (self-hosted, PostgreSQL-only, OCP-only) vs SaaS (console.redhat.com, multi-tenant, Trino/S3)

This document is grounded in the current codebase and deployment configuration. Values cited below come from `koku/koku/settings.py`, `koku/koku/celery.py`, `koku/koku/database.py`, `koku/gunicorn_conf.py`, `cost-onprem-chart/cost-onprem/values.yaml`, `koku/deploy/clowdapp.yaml`, and `ros-ocp-backend/internal/config/config.go` unless noted otherwise.

---

## Executive Summary

Red Hat Cost Management is a **dual-path system** by design: SaaS runs heavy aggregation through **Trino over Parquet in S3** and serves UI queries from **PostgreSQL summary tables**; on-prem replaces Trino with **PostgreSQL-only SQL** (`self_hosted_sql/`) and supports **OCP only**. Performance characteristics diverge sharply at three cliffs:

1. **On-prem PostgreSQL saturation** — The Helm chart ships **512Mi RAM / 30Gi PVC / `work_mem=4MB`** defaults explicitly labeled demo-only. A single unified PostgreSQL instance hosts Koku, ROS, Kruize, and RBAC. Connection budgeting (`max_connections=200`) is documented but **Koku has no explicit Django `CONN_MAX_AGE` or pool limits**; Gunicorn (2 workers × 4 threads) and Celery (5 concurrency × 4 active queues) can exhaust connections under concurrent ingest + API load.

2. **SaaS schema-per-tenant metadata tax** — `django-tenants` with `TENANT_MULTIPROCESSING_MAX_PROCESSES=2` makes tenant migrations and autovacuum tuning **O(tenants)** operations. The `autovacuum_tune_schemas` beat task fans out one Celery task per schema daily. At thousands of tenants this becomes a **control-plane bottleneck** independent of query performance.

3. **API caching vs correctness** — Nearly all report endpoints use `cache_page(CACHE_TIMEOUT=3600)` on the `api` Redis cache with **`IGNORE_EXCEPTIONS=True`**. This improves p95 latency but creates a **1-hour staleness window** after backend fixes unless Valkey is flushed. There is **no general API rate limiting**; only opt-in tag-query throttling (1 request / 12 hours per schema, feature-flagged).

**What is well-engineered:**

- Celery `CELERY_WORKER_PREFETCH_MULTIPLIER=1` and per-provider task deduplication via `WorkerCache` prevent duplicate summary work.
- Large-customer isolation via Unleash flags routes to XL/Penalty KEDA-scaled worker queues with reduced concurrency (`WORKER_CACHE_LARGE_CUSTOMER_CONCURRENT_TASKS=2`).
- Monthly **RANGE partitioning** on `usage_start` for summary tables keeps hot data bounded.
- ROS-OCP-Backend uses explicit **pgxpool limits** (`ROS_DB_MAX_CONNS=5`), **statement timeouts**, and **GOMEMLIMIT** — more mature than Koku's DB connection story.
- SaaS separates **reads** (3× koku-api-reads, optional read replica) from **writes** (3× koku-api-writes).

**Highest-impact recommendations (cross-cutting):**

| Priority | Recommendation | Mode |
|----------|----------------|------|
| P0 | Size on-prem PostgreSQL per `database-tuning.md` Medium/Large profiles before production fleets | On-prem |
| P0 | Enable `USE_READREPLICA=true` on koku-api-reads in SaaS prod | SaaS |
| P1 | Add connection pool budgeting / PgBouncer for Koku API + workers | Both |
| P1 | Instrument and alert on Celery queue depth (`collect_queue_metrics`) and PostgreSQL `pg_stat_activity` | Both |
| P2 | Reduce API cache TTL for mutating workflows or add targeted cache invalidation on cost model updates | Both |
| P2 | Raise `TENANT_MULTIPROCESSING_MAX_PROCESSES` for migration windows only | SaaS |

---

## Architecture Reference

```
On-Prem:
  Operator → Ingress → Kafka → Listener → Celery (ocp/summary/cost_model)
                ↓                              ↓
           PostgreSQL (self_hosted_sql) ←──────┘
                ↓
           Koku API ← ROS API (same PG host, different DBs)

SaaS:
  Provider CUR / OCP Ingress → Download workers → Parquet/S3
                ↓
           Trino aggregation → PostgreSQL summaries (per-tenant schema)
                ↓
           Koku API (reads replica optional) ← Redis cache/RBAC
```

---

## On-Prem Recommendations

### A. Database Performance

**Current state (`cost-onprem/values.yaml`):**

```yaml
database.resources.limits.memory: 512Mi   # "demo/dev default only"
database.storage.size: 30Gi
postgresqlConfiguration:
  shared_buffers: 128MB
  work_mem: 4MB
  effective_cache_size: 384MB
  max_connections: 200
```

**Connection budget (documented in chart, ~67/200 used at defaults):**

| Component | Connections |
|-----------|-------------|
| ROS API | 5 × HPA maxReplicas(4) = 20 |
| ROS processor + housekeeping | ~12 |
| Koku Celery workers | ~20 |
| Koku API + Masu | ~10 |
| RBAC + Kruize | ~10 |

**Issues:**

1. **`work_mem=4MB` is insufficient for ROS aggregation** at 5k+ containers. ROS list queries on `recommendation_sets` (6 rows/container) benefit from 16–32MB (`database-tuning.md` Medium profile). Spill-to-disk sorts show up as multi-second API latency before OOM.

2. **Koku Django has no `CONN_MAX_AGE` or pool cap** in `database.config()`. Each Gunicorn worker thread and Celery child process opens independent connections. With `GUNICORN_WORKERS=2` and `GUNICORN_THREADS=4`, the API alone can hold up to **8 concurrent connections per pod** without bound on idle lifetime.

3. **Unified PostgreSQL is a single point of failure and I/O contention.** Koku summary INSERTs, ROS ingest COPY, and Kruize JDBC (c3p0 max 5) compete for the same 512Mi instance.

4. **On-prem uses `self_hosted_sql/`** — every Trino aggregation becomes a PostgreSQL query against line-item tables. Large OCP clusters shift CPU load from Trino workers to PostgreSQL. This is correct architecturally but **changes the sizing unit**: you size PostgreSQL like a warehouse, not like a metadata catalog.

**Recommendations:**

- **Minimum production profile:** 4Gi RAM, 100Gi PVC, `shared_buffers=1GB`, `work_mem=32MB`, `maintenance_work_mem=256MB` (see `cost-onprem-chart/docs/operations/database-tuning.md`).
- **Fleet ≥15k containers:** 8Gi+ RAM, 200Gi+ PVC, consider `max_connections=300` and lower `ros.dbMaxConns` if using DBaaS caps.
- **Deploy PgBouncer** (transaction pooling) in front of PostgreSQL for Koku API + Celery; keep session pooling for ROS if using prepared statements heavily.
- **Monitor:** `pg_stat_activity` count by `application_name`, `pg_stat_user_tables.n_dead_tup` on `reporting_ocpusagelineitem_daily_summary` and ROS digest tables.
- **Run Koku's `autovacuum_tune_schemas` equivalent:** Port the SaaS beat task or schedule manual `autovacuum_vacuum_scale_factor` tuning on high-churn tables (on-prem chart does not expose `VACUUM_DATA_DAY_OF_WEEK` by default).

### B. Caching Strategy

**Current state:**

| Cache | Backend | TTL | Notes |
|-------|---------|-----|-------|
| `default`, `api` | Redis/Valkey | 3600s | `MAX_ENTRIES=1000`, `IGNORE_EXCEPTIONS=True` |
| `rbac` | Redis/Valkey | 300s (`RBAC_CACHE_TIMEOUT`) | Per user+org |
| `worker` | **PostgreSQL `worker_cache_table`** | 86400s | Cross-pod task coordination |

**Issues:**

1. **Worker cache in PostgreSQL** adds write load to the same saturated database during ingest storms. `rate_limit_tasks()` runs `SELECT count(*) FROM public.worker_cache_table` with LIKE patterns — not index-friendly at scale.

2. **API `cache_page` 1-hour TTL** (`CACHED_VIEWS_DISABLED=False` in chart) means UI can show stale costs for up to an hour after cost model recalculation unless Valkey is flushed.

3. **Single Valkey instance** serves Celery broker, result backend, and Django caches (`REDIS_DB=1` for caches; broker uses same URL).

**Recommendations:**

- Set `CACHE_TIMEOUT=900` (15 min) for on-prem if operators frequently iterate on cost models; or document mandatory `FLUSHALL` after recalc.
- Consider moving `worker` cache to Valkey (same as SaaS would benefit) to remove hot-row contention on `worker_cache_table`.
- Size Valkey: **512Mi request / 1Gi limit** minimum for production; monitor memory when Celery result expiry is 28800s (8h).

### C. Celery / Task Queue Performance

**Current state (`cost-onprem/values.yaml`):**

| Queue | Replicas | Concurrency | Memory limit |
|-------|----------|-------------|--------------|
| `ocp` | 1 | 5 | 1Gi |
| `summary` | 1 | 5 | 2Gi |
| `cost_model` | 1 | 5 | 512Mi |
| `priority` | 1 | 5 | 2Gi |
| `celery` (default) | 1 | 5 | 400Mi |
| `download`, `hcs`, `refresh`, `subs_*` | **0** | — | Disabled (OCP-only) |

Global settings: `CELERY_WORKER_PREFETCH_MULTIPLIER=1`, `MAX_CELERY_TASKS_PER_WORKER=10` (worker child recycle), `pollingTimer=300`.

**Issues:**

1. **Single replica per active queue** — Ingest + summary + cost model cannot scale horizontally without chart edits. SaaS uses **KEDA** on summary (min 1, max 10, trigger `koku:celery:summary_queue` threshold 13.5).

2. **`reportDownloadSchedule: "*/5 * * * *"`** with `scheduleReportChecks: true` is low impact on-prem (download workers disabled) but still schedules beat work.

3. **Priority queue starvation risk:** If `priority` worker is busy, delayed tasks poll every 30 min (`DELAYED_TASK_POLLING_MINUTES` default in celery.py; chart uses 300s polling timer for orchestrator).

**Recommendations:**

- For clusters with **>1 OCP source** or **>500 namespaces**, raise `summary` and `ocp` replicas to 2 and increase memory limits to 4Gi for summary workers (pandas/SQL heavy).
- Set `MAX_CELERY_TASKS_PER_WORKER=50` only after verifying memory stability — default 10 is aggressive recycling (good for leak mitigation, bad for cold-start overhead).
- Alert when `ocp` + `summary` queue depth > 10 for >15 minutes (expose via Masu `collect_queue_metrics` pattern).

### D. API Layer

**Current state:**

- `GUNICORN_WORKERS=2`, `GUNICORN_THREADS=4`, `POD_CPU_LIMIT=1`, timeout **90s** (`gunicorn_conf.py`).
- `MAX_GROUP_BY=3` limits query cardinality.
- Pagination: `ReportPagination.default_limit=100`, `max_limit=1000`; `ResourceTypePaginator.max_limit=20000`.
- **No global rate limiting.** `ENHANCED_ORG_ADMIN=False` in chart (RBAC enforced).

**Issues:**

1. **90s Gunicorn timeout** with threaded workers: one slow report query blocks a thread; 8 concurrent slots exhaust quickly on wide `group_by` + tag queries.

2. **`ResourceTypePaginator.max_limit=20000`** enables enormous list responses (cluster/project enumeration) — memory pressure on API pod.

3. **Middleware stack** hits RBAC HTTP service per request (300s cache). RBAC pod shares PostgreSQL.

**Recommendations:**

- Increase API CPU limit to **2 cores** and `GUNICORN_WORKERS=3` for production dashboards.
- Add **ingress rate limiting** at OAuth2-proxy/nginx for on-prem (not present in chart).
- Cap `ResourceTypePaginator.max_limit` to 5000 for on-prem via env if UI does not need 20k.

### E. Data Pipeline (Ingestion)

**Current state:**

- OCP **push model:** Operator → ingress (100MB max upload) → Kafka `platform.upload.announce` → listener (1 replica, 300m CPU limit).
- Listener is **CPU-bound** during tar extraction and manifest validation; E2E tests boost listener CPU during IQE (`--listener-cpu max`).
- On-prem: Parquet → **direct PostgreSQL** line items via self-hosted processors (no S3/Trino in critical path after upload).

**Issues:**

1. **Single listener replica** — upload bursts queue in Kafka; lag increases time-to-dashboard.
2. **100MB ingress limit** — large monthly payloads may require operator `packaging.max_size_MB` tuning and split uploads.
3. **Memory:** CSV/pandas processing in Celery `ocp`/`summary` workers with 1Gi limit can OOM on GPU or large cluster reports.

**Recommendations:**

- Scale listener to **2 replicas** when Kafka partition count ≥2 (match SaaS `LISTENER_REPLICAS=2`).
- Monitor Kafka consumer lag on `cost-mgmt-listener-group`.
- Pre-allocate **4Gi summary worker memory** for clusters reporting >10k pods/month.

### F. Infrastructure Sizing (On-Prem)

| Component | Demo (chart default) | Production (single cluster, 5k containers) |
|-----------|------------------------|-----------------------------------------------|
| PostgreSQL | 512Mi / 30Gi | 4Gi / 100Gi |
| Koku API | 1Gi / 1 CPU | 2Gi / 2 CPU |
| Summary worker | 2Gi / 0.5 CPU | 4Gi / 2 CPU |
| Listener | 600Mi / 0.3 CPU | 1Gi / 1 CPU |
| Valkey | (chart default) | 1Gi |
| ROS API | 1Gi | 1Gi (HPA max 4) |
| ROS processor | 1Gi | 2Gi |

Network: Plan **≥100 Mbps** sustained during monthly upload windows (operator default 6h cycle).

### G. Observability and SLOs (On-Prem)

| SLI | Suggested SLO | Alert threshold |
|-----|---------------|-----------------|
| OCP upload → manifest complete | 95% < 30 min | >45 min p95 |
| API `/reports/openshift/costs/` p95 | < 3s | > 5s for 5 min |
| PostgreSQL connections | < 70% of max | > 140/200 |
| ROS `/readyz` | 99.9% | 2 failures |
| Kafka consumer lag | < 100 messages | > 1000 |

Use existing probes: Koku `/readyz` :9000, ROS `/readyz`, `RequestTimingMiddleware` logs `response_time` ms.

### H. Known Bottlenecks and Risks (On-Prem)

| Risk | Severity | Notes |
|------|----------|-------|
| Unified PostgreSQL SPOF | High | No HA in default chart |
| Demo DB sizing in production | Critical | Documented but easy to miss |
| Worker cache on PostgreSQL | Medium | Contention during parallel ingest |
| Kruize still deployed | Medium | Deprecated; consumes DB connections + memory |
| `imagePullPolicy: IfNotPresent` | Medium | Stale images after upgrades |
| Cache staleness after fixes | Medium | 1h TTL + no invalidation |

---

## SaaS Recommendations

### A. Database Performance

**Current state:**

- **Schema-per-tenant** (`org{org_id}`) for `reporting` and `cost_models` apps.
- **Partitioned summary tables** (`*SummaryP`) with monthly RANGE partitions; runtime partition creation via `partitioned_tables` registry.
- **`USE_READREPLICA=false`** by default in `clowdapp.yaml` despite `KOKU_READ_ONLY_DB=cost-db-ro` — reads hit primary.
- Migrations: `TENANT_MULTIPROCESSING_MAX_PROCESSES=2`, `TENANT_MULTIPROCESSING_CHUNKS=2`.
- **`autovacuum_tune_schema`:** Adjusts `autovacuum_vacuum_scale_factor` per table based on `n_live_tup` thresholds (10M→0.01, 1M→0.02, 100k→0.05).

**Issues:**

1. **Thousands of schemas** inflate pg_catalog size, migration duration, and autovacuum fan-out. Each new tenant creates full partition sets.

2. **No connection pooling in Django** — 3 read replicas × 3 Gunicorn workers × threads = **27+ connections** per API tier before workers.

3. **Read replica unused by default** — Primary serves both heavy Trino-driven writes (summary INSERT) and concurrent UI reads.

4. **`cascade_delete()` in `database.py`** walks FK graph with raw SQL — expensive for tenant teardown, locks tables.

5. **Cross-schema queries** are avoided in ORM (good) but migration checks hit `public` functions.

**Recommendations:**

- **Enable read replica** for `koku-api-reads` (`USE_READREPLICA=true`) — low-risk win for report query isolation.
- **PgBouncer** (transaction mode) between app tiers and RDS/Aurora; target **≤100 app connections** per API pod.
- **Migration windows:** Temporarily set `TENANT_MULTIPROCESSING_MAX_PROCESSES=8` during release migrations; revert after.
- **Per-tenant autovacuum:** Keep beat task but batch schemas (e.g., 100/schema/hour) to avoid Celery storms at midnight UTC.
- **Index hygiene:** Audit `reporting_ocpusagelineitem_daily_summary` and `*SummaryP` for tenant schemas >100GB — ensure partition pruning in all SQL templates.

### B. Caching Strategy

Same Redis architecture as on-prem with production Valkey/ElastiCache sizing.

**Additional SaaS concerns:**

- **`MAX_ENTRIES=1000`** per cache backend may evict hot keys under multi-tenant load — monitor Redis `evicted_keys`.
- **Tenant-scoped cache keys** via `django_tenants.cache.make_key` — good isolation.
- **RBAC 300s cache** × many users → consider raising to 600s for stable tenants if RBAC service p95 is high.

**Recommendations:**

- Split Redis: **broker**, **cache**, **results** on separate instances/clusters at scale.
- Export cache hit ratio metric (django-redis does not by default — wrap or use Redis INFO).
- On cost model PUT, **publish cache invalidation** for affected provider's report prefixes (today: manual flush or wait 1h).

### C. Celery / Task Queue Performance

**Current state (SaaS `clowdapp.yaml` highlights):**

| Worker tier | Scaling | Memory limit | Queue |
|-------------|---------|--------------|-------|
| summary | KEDA 1–10 (fallback 3) | 750Mi | summary |
| summary-xl | KEDA | Higher | summary-xl |
| summary-penalty | KEDA | Higher | summary-penalty |
| download | KEDA | 1Gi+ | download |
| cost_model | KEDA | 750Mi | cost_model |
| ocp | KEDA | 750Mi | ocp |

`CELERY_WORKER_PREFETCH_MULTIPLIER=1`, `MAX_CELERY_TASKS_PER_WORKER=10`.

**Large customer controls (`settings.py`):**

- `WORKER_CACHE_LARGE_CUSTOMER_CONCURRENT_TASKS=2`
- `WORKER_CACHE_LARGE_CUSTOMER_TIMEOUT=14400` (4h) vs default 3600s
- Unleash: `cost-management.backend.large-customer`, `.rate-limit`, `.penalty-customer`

**Issues:**

1. **Summary worker memory 750Mi** may be tight for AWS CUR months with wide tag cardinality — XL queue exists but requires Unleash flagging.

2. **Beat scheduler load:** Multiple daily/hourly tasks (download check, autovacuum, HCS, subs, currency, Azure scrape, account hierarchy, delayed tasks, ROS tag sync every 6h).

3. **`CELERY_INSPECT` in hot paths** (`is_task_currently_running`) — Redis round-trip per check; fails open on `OperationalError`.

**Recommendations:**

- Tune KEDA **`WORKER_SUMMARY_TRIGGER_THRESHOLD=13.5`** against observed queue depth at steady state; alert if max replicas sustained >1h.
- **Separate beat scheduler** pod is single-replica — protect it ( PDB, priority class).
- Document runbook for **penalty queue** assignment when tenant overwhelms shared workers.

### D. API Layer

**Current state:**

- **3× koku-api-reads**, **3× koku-api-writes**, nginx 3 replicas.
- `GUNICORN_WORKERS=3`, `GUNICORN_THREADS=True` (thread count = `cpu*2+1`).
- **`cache_page` on ~50+ report/tag endpoints** (`api/urls.py`).
- Tag query throttle: **1 req / 12h** per schema when feature flag enabled and query is "heavy" (monthly scope + tag filter).

**Issues:**

1. **No 3scale/rate-limit on general reports** — large customers can hammer API; caching helps only repeat identical queries.

2. **`PACK_DEFINITIONS` nested JSON** assembly in Python post-SQL — CPU cost on large responses.

3. **90s timeout** insufficient for Trino-backed live queries if cache miss on complex explorer queries.

**Recommendations:**

- Enable **read replica** + keep cache — dual benefit.
- Add **per-org request budget** at gateway (console.redhat.com infrastructure).
- Precompute **top-N ranked views** (`ReportRankedPagination.default_limit=5`) — already optimized; extend pattern.

### E. Data Pipeline (Ingestion)

**SaaS path:**

1. Download workers pull CUR/Azure/GCP exports (KEDA-scaled).
2. Parquet to S3/MinIO.
3. **Trino** runs `trino_sql/` templates.
4. Results INSERT into tenant PostgreSQL summaries.

**Issues:**

1. **Trino is stateless but S3-listing heavy** — partition metadata in Hive metastore can lag deletes (`HIVE_PARTITION_DELETE_RETRIES=5`).

2. **Dual-path maintenance** — every SQL change needs `trino_sql/` + `sql/` (+ `self_hosted_sql/` for OCP-on-prem code reuse).

3. **Initial ingest:** `INITIAL_INGEST_NUM_MONTHS=2`, `POLLING_COUNT=21` — long tail for new large AWS accounts.

**Recommendations:**

- Monitor Trino **query queued time** and **S3 GET rate** per tenant schema.
- Cap concurrent Trino aggregation per tenant via existing worker cache locks (extend to Trino submission layer).
- For OCP-on-cloud, correlation jobs are **cross-provider** — ensure `refresh` workers scaled appropriately (penalty tiers).

### F. Infrastructure Sizing (SaaS)

SaaS uses OpenShift **Clowder** parameterized resources. Baseline per `clowdapp.yaml`:

| Tier | Replicas | CPU limit | Memory limit |
|------|----------|-----------|--------------|
| koku-api-reads | 3 | 500m | 1Gi |
| worker-summary | 1–10 (KEDA) | 200m | 750Mi |
| listener | 2 | 300m | 600Mi |
| masu | 1+ | varies | varies |

**Horizontal scaling:** Worker tiers use **KEDA** on Redis queue length metrics (`koku:celery:*`). API tiers use fixed replica counts — consider HPA on CPU for reads during month-close.

**Storage:** S3 for Parquet (unbounded); PostgreSQL per-tenant schema growth **~100MB–10GB+/year** depending on tag cardinality and providers.

### G. Observability and SLOs (SaaS)

| SLI | Suggested SLO |
|-----|---------------|
| Report API availability | 99.9% |
| Freshness (data ≤48h old for daily providers) | 95% tenants |
| Ingest pipeline success | 99% manifests |
| p95 report latency (cached) | < 2s |
| p95 report latency (uncached) | < 15s |
| Celery queue age | < 1h p95 |

Existing metrics: `django_prometheus`, `celery_errors`, `hccm_unique_account`, queue gauges from `collect_queue_metrics`, `RequestTimingMiddleware` structured logs.

### H. Known Bottlenecks and Risks (SaaS)

| Cliff | When | Symptom |
|-------|------|---------|
| Schema count | >2000 tenants | Migration/autovacuum hours-long |
| Large AWS tenant | >500M line items/month | Summary XL queue, 4h rate limit |
| Trino cluster capacity | Month-close | Query queue depth ↑ |
| Redis single-cluster | Broker+cache shared | Latency spikes, OOM |
| Cache stampede | Monday AM UTC | PostgreSQL overload on cache miss |
| Beat task fan-out | Daily 00:00 UTC | Celery default queue flood |

---

## Shared Recommendations

### Database

1. **Implement PgBouncer** (or RDS Proxy) for all Django connection pools — highest ROI change for both modes.
2. **Add `CONN_MAX_AGE=60`** and monitor — reduces connection churn without unbounded idle connections.
3. **Partition retention:** `RETAIN_NUM_MONTHS=3` (on-prem chart) vs `4` (SaaS default) — align with operator upload retention to avoid orphan partitions.

### Caching

1. Document **cache flush runbook** (already in AGENTS.md) — automate post-deploy Job to `FLUSHDB` api cache prefix.
2. Move worker coordination off PostgreSQL to Redis when feasible.
3. Set `IGNORE_EXCEPTIONS=False` in staging to surface Redis failures early.

### Celery

1. **`CELERY_WORKER_PREFETCH_MULTIPLIER=1`** — keep; correct for long-running tasks.
2. **`MAX_CELERY_TASKS_PER_WORKER=10`** — profile memory; may increase worker churn cost on short tasks.
3. Expose **queue depth dashboards** from existing `collect_queue_metrics` Prometheus gauges.

### API

1. **`RequestTimingMiddleware`** already logs latency — aggregate to histogram metrics (not just logs).
2. Review **`MAX_GROUP_BY=3`** for explorer UX vs query cost tradeoff.
3. Extend throttling pattern from `OcpTagQueryThrottle` to **cost explorer** endpoints if abuse observed.

### Data Pipeline

1. **Idempotent manifests** — worker cache prevents duplicate summary; verify ingress dedup for replayed uploads.
2. **pandas `copy_on_write`** enabled in settings — good; still profile peak RSS during parquet conversion.

### ROS-OCP-Backend (both modes)

1. Keep **`ros.dbMaxConns=5`** coordinated with HPA max replicas (documented in chart).
2. Use **`ROS_DB_STATEMENT_TIMEOUT`** and API-specific timeouts — already in `config.go`; verify on-prem env injection.
3. **`ROS_SAMPLE_RETENTION_DAYS=45`** — maintain; primary disk driver for ROS DB.
4. **Native Go engine** — eliminate Kruize from critical path when chart allows (`kruize` replicas → 0).

### koku-metrics-operator

1. **Upload cycle default 6h** with 14-day Prometheus lookback — CPU/network spikes during packaging.
2. **`packaging.max_size_MB`** vs ingress 100MB limit — must align.
3. Operator is **single-replica** reconciliation — cluster-side bottleneck is Prometheus query rate, not operator CPU.

---

## Priority Matrix (Effort vs Impact)

| Item | Impact | Effort | Mode |
|------|--------|--------|------|
| On-prem DB sizing (Medium profile) | Critical | Low | On-prem |
| Enable SaaS read replica | High | Low | SaaS |
| PgBouncer for Koku | High | Medium | Both |
| Listener HPA / 2 replicas | High | Low | On-prem |
| KEDA-style summary scaling in chart | High | Medium | On-prem |
| Cache invalidation on cost model update | Medium | Medium | Both |
| Worker cache → Redis | Medium | Medium | Both |
| Split Redis broker/cache | Medium | High | SaaS |
| Migration multiprocessing bump | Medium | Low | SaaS |
| API rate limiting at gateway | Medium | Medium | Both |
| Remove Kruize from on-prem | Medium | Low | On-prem |
| Trino query governance | High | High | SaaS |
| Per-tenant connection budgets | High | High | SaaS |

---

## Monitoring and SLO Recommendations

### Golden signals per component

**Koku API:**
- Request rate, p50/p95/p99 latency (`RequestTimingMiddleware`)
- 5xx rate, 424 Failed Dependency (RBAC/DB)
- Gunicorn worker utilization, `worker_abort` log rate

**PostgreSQL:**
- `numbackends`, `xact_commit`, `blks_hit/read`, `temp_files`
- Per-schema size growth (tenant schemas SaaS; `org*` + ROS DB on-prem)
- Autovacuum duration (`log_autovacuum_min_duration=1000` ms already set in dev compose)

**Celery:**
- Queue depth by name (`collect_queue_metrics`)
- Task runtime p95 for `update_summary_tables`, `update_cost_model_costs`
- Worker child recycle rate (`worker_max_tasks_per_child=10`)

**ROS:**
- `/healthz` goroutine count, GC pause
- Ingest batch duration, Kafka lag
- DB acquire wait time (pgxpool)

**Kafka:**
- Consumer lag `cost-mgmt-listener-group`, `ros-processor`
- Broker disk usage

### Alerting thresholds (starting points)

| Alert | Condition | Severity |
|-------|-----------|----------|
| PG connections critical | >85% max_connections for 5m | P1 |
| Summary queue backlog | depth > 50 for 15m | P2 |
| API p95 latency | > 10s for 5m | P2 |
| Listener lag | > 500 messages | P2 |
| Redis evictions | > 0/s sustained | P2 |
| Celery worker zero | `celery_errors` + no active workers | P1 |
| Disk > 80% (on-prem PVC) | | P1 |

---

## Risk Register

| ID | Risk | Likelihood | Impact | Mitigation |
|----|------|------------|--------|------------|
| R1 | On-prem prod deployed with 512Mi PostgreSQL | High | Critical | Pre-install checklist, CI guard |
| R2 | Connection exhaustion under ingest+API | Medium | High | PgBouncer, pool limits |
| R3 | SaaS migration duration blocks releases | Medium | High | Batched migrations, off-hours |
| R4 | Stale API cache masks regressions | Medium | Medium | Flush automation, lower TTL |
| R5 | Single listener on-prem | Medium | High | Scale to 2 replicas |
| R6 | Trino/S3 dependency failure (SaaS) | Low | Critical | Retry logic exists; monitor queue age |
| R7 | Large tenant noisy neighbor | Medium | High | XL/penalty queues, rate-limit flag |
| R8 | Unified DB ROS+Koku contention | High (on-prem) | High | Split DB or size for combined load |
| R9 | Worker cache table bloat | Low | Medium | Periodic cleanup, move to Redis |
| R10 | No API rate limits | Medium | Medium | Gateway throttling |

---

## Appendix: Key Configuration References

| Setting | Value | File |
|---------|-------|------|
| `CACHE_TIMEOUT` / `CACHE_MIDDLEWARE_SECONDS` | 3600 | `settings.py` |
| `RBAC_CACHE_TIMEOUT` | 300 (on-prem chart) | `values.yaml` |
| `CELERY_WORKER_PREFETCH_MULTIPLIER` | 1 | `settings.py` |
| `MAX_CELERY_TASKS_PER_WORKER` | 10 | `celery.py` |
| `GUNICORN timeout` | 90s | `gunicorn_conf.py` |
| `TENANT_MULTIPROCESSING_MAX_PROCESSES` | 2 | `settings.py` |
| `KOKU_READS_REPLICAS` | 3 | `clowdapp.yaml` |
| `USE_READREPLICA` | false (default) | `clowdapp.yaml` |
| `WORKER_SUMMARY_MAX_REPLICAS` | 10 | `clowdapp.yaml` |
| `ros.dbMaxConns` | 5 | `values.yaml` |
| `database max_connections` | 200 | `values.yaml` |
| `MAX_GROUP_BY` | 3 | `settings.py` |
| `ReportPagination.max_limit` | 1000 | `pagination.py` |

---

## Document History

| Date | Author | Change |
|------|--------|--------|
| 2026-06-16 | Performance audit | Initial comprehensive analysis |
