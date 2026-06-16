# Performance Engineering Guide

Operational guidance for sizing, tuning, and monitoring Red Hat Cost Management
and ROS-OCP-Backend in **on-prem** and **SaaS** deployments.

For the full technical audit (code references, risk register, priority matrix),
see the internal document:
[`docs/audits/performance-scalability-analysis.md`](../../docs/audits/performance-scalability-analysis.md).

**Last updated:** 2026-06-16

---

## Quick diagnosis

| Symptom | Likely cause | First action |
|---------|--------------|--------------|
| All API costs `0.00` after ingest | Cost model not applied | Update cost model; trigger recalc |
| UI shows old data after backend fix | 1-hour API cache | Flush Valkey; hard-refresh browser |
| ROS API timeouts on large fleets | PostgreSQL `work_mem` too low | Apply Medium DB profile (below) |
| Ingest never completes | Listener CPU / Kafka lag | Scale listener; check consumer lag |
| `too many connections` | Connection pool exhaustion | Lower HPA max replicas or add PgBouncer |
| Summary queue growing | Under-provisioned workers | Scale summary/OCP Celery workers |

---

## Architecture at a glance

**On-prem** (cost-onprem Helm chart):

- Single PostgreSQL hosts Koku, ROS, Kruize, and RBAC databases.
- OCP data only; ingestion is **push** (operator → ingress → Kafka → listener).
- Aggregation runs in **PostgreSQL** (no Trino).

**SaaS** (console.redhat.com):

- **Schema-per-tenant** PostgreSQL (`org{org_id}`) for reporting data.
- Cloud providers aggregated via **Trino** over Parquet in S3, then written to summary tables.
- Separate read/write API tiers; workers scale with **KEDA** on queue depth.

---

## On-prem sizing

### Do not use chart defaults in production

The Helm chart defaults (`512Mi` PostgreSQL, `30Gi` PVC, `work_mem=4MB`) are
**demo/dev only**. The chart comments and
[`database-tuning.md`](https://github.com/redhatinsights/cost-onprem-chart/blob/main/docs/operations/database-tuning.md)
document production profiles.

### Recommended starting profile (5k–15k containers)

```yaml
database:
  resources:
    requests:
      memory: "4Gi"
      cpu: "1000m"
    limits:
      memory: "4Gi"
      cpu: "4000m"
  storage:
    size: "100Gi"
  postgresqlConfiguration:
    shared_buffers: "1GB"
    work_mem: "32MB"
    effective_cache_size: "3GB"
    max_connections: "200"
    maintenance_work_mem: "256MB"
    random_page_cost: "1.1"
```

### Connection budget

Default `max_connections=200`. Stay within this budget:

| Component | Connections (defaults) |
|-----------|----------------------|
| ROS API | 5 × max HPA replicas (4) = 20 |
| ROS processor + housekeeping | ~12 |
| Koku API + Masu | ~10 |
| Koku Celery | ~20 |
| RBAC + Kruize | ~10 |
| **Total (approx.)** | **~67** |

If using external DBaaS with a lower cap, reduce `ros.dbMaxConns` and
`ros.api.autoscaling.maxReplicas` together.

### Worker and API scaling

| Component | Demo | Production suggestion |
|-----------|------|------------------------|
| Koku API | 1 replica, 2 workers | 2 replicas, 3 workers, 2 CPU |
| Summary Celery | 1 × concurrency 5 | 2 replicas, 4Gi memory each |
| OCP Celery | 1 × concurrency 5 | 2 replicas for multi-source |
| Listener | 1 replica | 2 replicas if Kafka partitions ≥ 2 |
| Valkey | default | 1Gi memory limit |

---

## SaaS operations notes

### Enable the read replica for report traffic

Production deploys a read-only database (`cost-db-ro`) but **`USE_READREPLICA`
defaults to `false`**. Setting it to `true` on `koku-api-reads` offloads report
queries from the write primary.

### Worker autoscaling

Summary workers scale **1–10 replicas** via KEDA when Redis queue depth exceeds
threshold (~13.5). Sustained max replicas indicates tenant overload — check
Unleash **large-customer** / **penalty-customer** flags.

### Large tenants

Unleash flags route heavy tenants to XL or penalty worker queues and limit
concurrent summary tasks to **2** with a **4-hour** lock timeout (vs 1 hour
default).

---

## Caching

### API response cache

Report endpoints cache responses for **3600 seconds (1 hour)** in Valkey/Redis.

After any backend change that affects API output:

1. Restart `koku-server` (or rollout API deployment).
2. Flush cache: `redis-cli FLUSHALL` (or scoped flush of `api:*` keys).
3. Hard-refresh the browser (`Ctrl+Shift+R`).

Set `CACHED_VIEWS_DISABLED=True` only for debugging — it removes a critical
latency optimization.

### RBAC cache

RBAC permissions cache for **300 seconds** per user/org (configurable via
`RBAC_CACHE_TIMEOUT`).

---

## Celery queues

### On-prem active queues

| Queue | Purpose |
|-------|---------|
| `ocp` | OCP report processing |
| `summary` | Daily summary table population |
| `cost_model` | Rate application |
| `priority` | Delayed / high-priority work |
| `celery` | Default (beat tasks, maintenance) |

Download, HCS, refresh, and subs queues are **disabled** (replicas: 0) in OCP-only mode.

### Tuning knobs

| Setting | Default | Purpose |
|---------|---------|---------|
| `CELERY_WORKER_PREFETCH_MULTIPLIER` | 1 | One task in flight per worker slot |
| `MAX_CELERY_TASKS_PER_WORKER` | 10 | Recycle worker child processes (memory) |
| `pollingTimer` | 300s (on-prem) | Orchestrator poll interval |

Monitor queue depth via Masu metrics (`collect_queue_metrics`).

---

## ROS-OCP-Backend

### Database pool

Each ROS process uses **pgxpool** with `ROS_DB_MAX_CONNS=5` (Helm:
`ros.dbMaxConns`). Total ROS connections = **5 × number of ROS pods**.

Set `GOMEMLIMIT` to ~90% of container memory limit (chart default: `922MiB` for
1Gi pods).

### Retention

- **`ROS_SAMPLE_RETENTION_DAYS=45`** — raw usage samples (primary disk driver).
- **`ROS_RETENTION_MONTHS=6`** — daily digests.

Lowering sample retention reduces PostgreSQL disk use by roughly **60–80%**
compared to the legacy 180-day default.

### Query performance

See [Query Performance](../query-performance.md) for index and pagination
rules. At 200k+ containers per org, always filter on denormalized `org_id` —
never scope through `clusters → rh_accounts` joins.

---

## SLOs and alerting (starting points)

| Signal | Target | Alert when |
|--------|--------|------------|
| OCP ingest → manifest complete | p95 < 30 min | > 45 min |
| Report API latency (cached) | p95 < 3s | > 5s for 5 min |
| PostgreSQL connections | < 70% of max | > 85% for 5 min |
| Celery summary queue depth | steady state < 10 | > 50 for 15 min |
| Kafka consumer lag (listener) | < 100 | > 1000 |
| ROS `/readyz` | 99.9% | failing |

### Useful checks

```bash
# On-prem: PostgreSQL connections
kubectl exec -n cost-onprem statefulset/cost-onprem-database -- \
  psql -U postgres -c "SELECT count(*) FROM pg_stat_activity;"

# On-prem: top tables by size
kubectl exec -n cost-onprem statefulset/cost-onprem-database -- \
  psql -U postgres -d costonprem_ros -c \
  "SELECT relname, pg_size_pretty(pg_total_relation_size(oid))
   FROM pg_class WHERE relkind='r'
   ORDER BY pg_total_relation_size(oid) DESC LIMIT 10;"

# Flush API cache (on-prem)
kubectl exec -n cost-onprem deploy/cost-onprem-cache -- redis-cli FLUSHALL
```

---

## Priority improvements

| Priority | Action | Benefit |
|----------|--------|---------|
| **P0** | Size on-prem PostgreSQL (Medium+ profile) | Prevents OOM, timeouts, disk full |
| **P0** | Enable SaaS read replica for API reads | Isolates report load from writes |
| **P1** | PgBouncer in front of PostgreSQL | Prevents connection exhaustion |
| **P1** | Scale listener + summary workers under load | Faster ingest |
| **P2** | Automate cache flush on deploy | Eliminates stale UI |
| **P2** | Remove Kruize when native ROS engine suffices | Frees DB connections and memory |

---

## Related documentation

- [Configuration](configuration.md) — Helm values and external infrastructure
- [Query Performance](../query-performance.md) — ROS API index and pagination
- [Monitoring](../monitoring.md) — Metrics and health endpoints
- [Upgrade Runbook](upgrade-runbook.md) — Safe rollout procedures
- Cost-onprem [database-tuning.md](https://github.com/redhatinsights/cost-onprem-chart/blob/main/docs/operations/database-tuning.md) — PostgreSQL profiles

---

## Document history

| Date | Change |
|------|--------|
| 2026-06-16 | Initial guide derived from platform performance audit |
