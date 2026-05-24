# Upgrade Runbook: Kruize-era → Native Engine

This document describes how to safely upgrade a running ros-ocp-backend
instance from a Kruize-era database schema to the native engine schema.

**Audience:** Operators performing the upgrade on a live deployment.

**Scope:** Covers migration safety concerns documented in 490-issues.md
(#84, #89, #90, #91, #92, #100). Fresh installations do NOT need this
runbook — all migrations run safely on an empty database.

---

## Prerequisites

- Access to the PostgreSQL database (direct or via port-forward)
- `kubectl`/`oc` access to the deployment namespace
- Familiarity with the Helm chart or ClowdApp deployment

## Overview of Risky Migrations

| Migration | Risk | Duration on populated DB |
|-----------|------|--------------------------|
| 000028 | Heavy DDL+DML: backfills denormalized columns, rebuilds PK | Minutes (proportional to `recommendation_sets` row count) |
| 000041 | `cluster_uuid::uuid` cast — fails if invalid UUID strings exist | Instant if data is clean; fails otherwise |
| 000045 | Creates unique index without CONCURRENTLY — blocks writes | Seconds to minutes (proportional to `gpu_container_digests` size) |
| 000058 | Drops and recreates PK on `node_recommendations` — ACCESS EXCLUSIVE lock | Sub-second (table is small: one row per node per term) |

---

## Step 1: Schedule Maintenance Window

Estimate duration based on data volume:

```sql
-- Check recommendation_sets row count (affects migration 000028)
SELECT count(*) FROM recommendation_sets;

-- Check gpu_container_digests row count (affects migration 000045)
SELECT count(*) FROM gpu_container_digests;

-- Check for invalid cluster_uuid values (affects migration 000041)
SELECT cluster_uuid FROM clusters
WHERE cluster_uuid !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$';

SELECT cluster_uuid FROM recommendation_sets
WHERE cluster_uuid !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$';
```

**Rules of thumb:**
- < 100K rows in `recommendation_sets`: ~30 seconds total
- 100K–1M rows: 1–5 minutes
- > 1M rows: 5–15 minutes (consider off-peak window)

## Step 2: Fix Invalid Data (if any)

If the cluster_uuid validation query in Step 1 returns rows:

```sql
-- Option A: Delete rows with invalid cluster_uuid (recommended if orphaned)
DELETE FROM recommendation_sets
WHERE cluster_uuid !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$';

DELETE FROM clusters
WHERE cluster_uuid !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$';

-- Option B: Fix known bad values (if the correct UUID is known)
UPDATE clusters SET cluster_uuid = '<correct-uuid>' WHERE cluster_uuid = '<bad-value>';
```

## Step 3: Stop Workers

**Critical:** Workers must be stopped BEFORE running migrations to prevent
deadlocks between migration 000058 (PK rebuild) and `PersistNodeRecommendations`
(which INSERTs into `node_recommendations`).

```bash
# Helm deployment
kubectl scale deployment cost-onprem-ros-processor --replicas=0 -n <namespace>

# Or for ClowdApp
oc scale deployment ros-ocp-processor --replicas=0 -n <namespace>

# Verify no active connections from workers
kubectl exec -it <db-pod> -n <namespace> -- psql -U postgres -c \
  "SELECT pid, application_name, state, query FROM pg_stat_activity WHERE application_name LIKE '%ros%';"
```

Wait until all worker connections are gone.

## Step 4: Run Migrations

```bash
# If using the migrate binary directly:
migrate -path migrations/ -database "postgres://..." up

# If using the init container (default Helm chart behavior):
# Simply restart the API pod — the init container runs migrations on startup
kubectl rollout restart deployment cost-onprem-ros-api -n <namespace>

# Monitor migration progress
kubectl logs -f deployment/cost-onprem-ros-api -c migrate -n <namespace>
```

**Expected output:**
```
000028/u alter_recommendation_sets (migrations completed successfully)
...
000041/u alter_clusters_cluster_uuid_to_uuid
000045/u gpu_container_digests_unique_index
...
000058/u node_recommendations_add_term
```

If migration 000041 fails with `invalid input syntax for type uuid`:
- Go back to Step 2, fix the data, then retry.

## Step 5: Verify Migration Success

```sql
-- Check current schema version
SELECT version, dirty FROM schema_migrations;
-- Expected: version=61, dirty=false

-- Verify PK on node_recommendations
SELECT conname, contype FROM pg_constraint
WHERE conrelname = 'node_recommendations' AND contype = 'p';
-- Expected: node_recommendations_pkey (includes term column)

-- Verify cluster_uuid is UUID type
SELECT column_name, data_type FROM information_schema.columns
WHERE table_name = 'clusters' AND column_name = 'cluster_uuid';
-- Expected: data_type = 'uuid'
```

## Step 6: Restart Workers

```bash
kubectl scale deployment cost-onprem-ros-processor --replicas=1 -n <namespace>

# Verify worker is healthy
kubectl logs -f deployment/cost-onprem-ros-processor -n <namespace> --tail=20
```

## Step 7: Verify End-to-End

```bash
# Check API responds
curl -s http://<ros-api-url>/api/cost-management/v1/recommendations/openshift/status

# Check recommendations are being generated (after next ingestion cycle)
curl -s -H "x-rh-identity: <token>" \
  http://<ros-api-url>/api/cost-management/v1/recommendations/openshift/ | python3 -m json.tool
```

---

## ON DELETE CASCADE Consideration (#92)

The `workloads` and `clusters` tables have `ON DELETE CASCADE` foreign keys.
Deleting a cluster (via Sources Kafka `destroy` event) will cascade-delete
all associated recommendations, digests, and history.

**Mitigation:**
- Cluster deletions are rare (manual operator action)
- On large tenants (>100K recommendations per cluster), the cascade could
  take 10-30 seconds and generate WAL pressure
- If this becomes a concern, consider:
  1. Soft-delete pattern (add `deleted_at` column, filter in queries)
  2. Background batch deletion (mark for deletion, sweep in CronJob)

**For now:** This is acceptable for the expected scale (< 50 clusters per
tenant, < 10K recommendations per cluster).

---

## Rollback Procedure

**Warning:** Rollback from native engine to Kruize-era is destructive.
Down migrations (#86, #87) will delete native-engine data.

```bash
# Only if absolutely necessary:
kubectl scale deployment cost-onprem-ros-processor --replicas=0 -n <namespace>
migrate -path migrations/ -database "postgres://..." down <N>
kubectl scale deployment cost-onprem-ros-processor --replicas=1 -n <namespace>
```

After rollback, native engine recommendations are lost and must be
regenerated from scratch on the next ingestion cycle.

---

## Fresh Installation (No Runbook Needed)

For fresh installations (empty database), all migrations run safely:
- Empty tables → no data to cast, no rows to lock, no cascades
- PK rebuilds are instantaneous on empty tables
- Index creation is instantaneous on empty tables

Simply deploy the Helm chart or ClowdApp and migrations will complete
in under 5 seconds.

---

## Future: Kruize-era Table Removal

The following tables are **dead weight** once all Kruize-era data has been
cleaned up (via source deletion or natural aging):

| Table | Purpose | Native engine uses it? |
|-------|---------|------------------------|
| `workloads` | Kruize experiment ↔ cluster mapping | No (native engine uses `recommendation_sets` directly) |
| `workload_metrics` | Raw Kruize metrics snapshots | No |
| `historical_recommendation_sets` | Kruize recommendation history | No (replaced by `recommendation_history`) |

**When to remove:**

1. All clusters have been re-ingested with the native engine (no `workload_id`
   references remain in `recommendation_sets`)
2. No rows exist in `workload_metrics` or `historical_recommendation_sets`

**Verification query:**

```sql
SELECT count(*) FROM workloads;
SELECT count(*) FROM workload_metrics;
SELECT count(*) FROM historical_recommendation_sets;
SELECT count(*) FROM recommendation_sets WHERE workload_id IS NOT NULL;
```

If all return 0, it's safe to create a migration that:
1. `ALTER TABLE recommendation_sets DROP COLUMN workload_id;`
2. `DROP TABLE historical_recommendation_sets;`
3. `DROP TABLE workload_metrics;`
4. `DROP TABLE workloads;`

**Note:** The `cleanupClusterAnalytics` function in `sourcesCleaner.go` has
steps for these tables. Remove those steps in the same PR that drops the tables.

---

## Known Limitation: At-Most-Once Cleanup Delivery

**Context:** When a Kafka `Application.destroy` event arrives, the sources
listener deletes all cluster data in batched steps. However, the Kafka consumer
commits offsets before processing completes (at-most-once delivery). If the
process crashes mid-cleanup, the event will NOT be replayed.

**Current risk level:** Low. Source deletions are rare (manual operator action,
< 1/month in typical deployments). Partial cleanup leaves orphaned digest/sample
rows that waste storage but don't affect correctness — the cluster is already
gone from the `clusters` table, so no API queries will return stale data.

**Detection:** Orphaned rows can be found with:

```sql
-- Digests referencing clusters that no longer exist
SELECT DISTINCT cluster_uuid FROM daily_container_digests d
WHERE NOT EXISTS (SELECT 1 FROM clusters c WHERE c.cluster_uuid = d.cluster_uuid);
```

**Future hardening (if needed at scale):**

1. Add a `pending_cleanups` table:
   ```sql
   CREATE TABLE pending_cleanups (
       id BIGSERIAL PRIMARY KEY,
       cluster_uuid UUID NOT NULL,
       org_id TEXT NOT NULL,
       created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
       completed_at TIMESTAMPTZ
   );
   ```

2. On `Application.destroy`: INSERT a pending row, THEN start cleanup.
   On success: SET `completed_at = NOW()`.

3. Add a periodic sweep (e.g., in the existing housekeeper CronJob or a
   dedicated goroutine) that retries rows with
   `completed_at IS NULL AND created_at < NOW() - INTERVAL '1 hour'`.

This gives at-least-once semantics without changing the Kafka consumer mode.
Defer implementation until scale demands it (> 1000 clusters per tenant or
frequent source churn).

---

## Business Hours (migrations 066–069)

### Prerequisites

- Koku `reship_ros` endpoint must be deployed first (koku branch: `feature/reship-ros-endpoint`)
- `ROS_BUSINESS_HOURS_ENABLED` defaults to `false` — set to `true` to activate

### Migration sequence

1. **000066** — Creates `business_hours_schedules` table
2. **000067** — Adds `schedule_type` ENUM and column to digest tables, extends PKs
3. **000068** — Adds `workload_type` to `container_usage_samples` PK (required for ON CONFLICT)
4. **000069** — Adds `reship_forward_only_since` column to `business_hours_schedules`

### Deploy order

1. Deploy koku with `reship_ros` endpoint
2. Deploy ros-ocp-backend with migrations 066–069
3. Update Helm chart with BH env vars enabled

### First schedule creation

- PUT triggers an immediate reship of historical data (up to `window_days`, default 14)
- Reship calls koku's `reship_ros` → S3 presigned URLs → Kafka → ros re-ingestion
- First-time reship for a large cluster (1000+ containers, 14 days) takes 1–5 minutes

### Rollback

- Set `ROS_BUSINESS_HOURS_ENABLED=false` → kill-switch hides endpoints, stops BH processing
- Existing `business_hours` digests remain in DB but are ignored
- To fully remove: run migration 067 DOWN, then 066 DOWN (drops BH data)
- Migration 068/069 are additive and safe to leave in place

---

## Currency field on API responses (additive)

ROS API responses include a top-level `currency` field (ISO 4217 code from the Koku
cost model, default `"USD"`) alongside existing `_usd` savings and cost JSON fields.
Clients can use `currency` to format monetary values correctly when the cost model uses
a non-USD unit.

Koku's `GET /api/cost-management/v1/effective_rates/` includes the same `currency` field.

### Deploy notes

- Deploy **koku** (Masu `effective_rates` currency field) before or with **ros-ocp-backend**.
- No client migration required — existing `_usd` field names are unchanged.
- No worker stop required; no PostgreSQL schema changes.

---

## Node and PVC savings columns (migration 000070)

### What it adds

Migration **000070** adds `estimated_monthly_savings_usd REAL` to:

- `node_recommendations`
- `pvc_recommendation_sets`

Container recommendations already had this column (migration 000026).

### Deploy notes

- **Safe on live deployments** — additive `ADD COLUMN IF NOT EXISTS`, no data backfill required
- Savings populate on the **next ingestion cycle** after deploy when `KOKU_MASU_URL` is set and `ROS_SAVINGS_ESTIMATES_ENABLED=true` (default)
- No worker stop required (unlike migration 000058 PK rebuild)
- Rollback: run `000070` down migration to drop the columns (savings values are recomputed on re-upgrade)

See [architecture/cost-integration.md](architecture/cost-integration.md) for formulas and plugin matrix.

---

## Node recommendation engines (migration 000071)

### What it adds

Migration **000071** adds an `engine TEXT NOT NULL DEFAULT 'cost'` column to `node_recommendations` and rebuilds the primary key to `(org_id, cluster_uuid, node, term, engine)`. Each node/term now stores separate **cost** and **performance** engine rows, mirroring container `recommendation_engines`.

### Transition period after deploy

1. **Immediately after migration:** Existing rows default to `engine = 'cost'`. Only cost-engine data is present until the next ingestion cycle completes.
2. **Performance engine rows** are written on the **next report ingestion** (typically within one upload cycle, up to ~6 hours on default operator settings).
3. **During this window:** API responses nest `recommendation_terms.*.recommendation_engines.performance` as empty/omitted. This is expected and self-resolving — no manual backfill required.
4. **API shape:** `GET /recommendations/openshift/nodes` returns one object per node with nested `recommendation_terms` / `recommendation_engines` (not flat per-engine rows).

### Deploy notes

- Uses advisory lock **7358001** (same as migration 000058) — workers block briefly rather than deadlocking.
- Rollback (`000071` down) deletes all `engine = 'performance'` rows before dropping the column.
- Pair with migration **000072** (sizing columns) so API engine blocks include `recommended_cpu_cores`, `recommended_memory_gib`, and `node_count_reduction`.

---

## Node sizing columns (migration 000072)

### What it adds

Migration **000072** adds to `node_recommendations`:

- `recommended_cpu_cores REAL`
- `recommended_memory_gib REAL`
- `node_count_reduction INTEGER NOT NULL DEFAULT 0`

These values are persisted at ingestion alongside `estimated_monthly_savings_usd` and exposed under each engine in the nested nodes API response.

### Deploy notes

- Additive `ADD COLUMN IF NOT EXISTS` — safe on live deployments.
- Values populate on the **next ingestion cycle** after deploy (no historical backfill).
- Rollback drops the three columns; values are recomputed on re-upgrade.
