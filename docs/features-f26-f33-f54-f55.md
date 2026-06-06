# Implemented Features: F26, F33, F54, F55

Last updated: 2026-05-07

These features were implemented as "low-hanging fruit" from the
[ros-ocp-backend-superpowers requirements](../../rh/costm/ros%20for%20openshift/performance/ros-ocp-backend-superpowers.md)
(REQ-6.1, REQ-7.6, REQ-10.7, REQ-10.8).

---

## F55: Staleness Detection (REQ-10.8)

### Definition

A recommendation is **stale** when the engine has not received new usage data
for a container or namespace when the cluster has not reported within the
**staleness threshold** (default: 48 hours / `ROS_STALENESS_THRESHOLD_HOURS`).
Staleness is evaluated during each recommendation run via
`isStaleRecommendation` (cluster `last_reported_at` takes precedence over
digest `bucket_date`).

### Behavior

| Condition | Result |
|-----------|--------|
| `now - latest_bucket_date > threshold` | `recommendation_sets.stale = true` |
| New data arrives within threshold | `stale` reset to `false` on next run |
| Stale for > `ROS_STALE_CLEANUP_DAYS` (default 30) | Deleted by retention sweep |

### Notification

- Code: `NotifStaleData` (code 2)
- Emitted when `stale = true`
- Removed when `stale` resets to `false` on next recommendation run

### API Filter

`GET /recommendations/openshift?stale=<value>`

| Value | Behavior |
|-------|----------|
| `false` (default) | Exclude stale recommendations (backward compatible) |
| `true` | Include all recommendations (stale + non-stale) |
| `only` | Return only stale recommendations |

### Configuration

| Env Var | Default | Description |
|---------|---------|-------------|
| `ROS_STALENESS_THRESHOLD_HOURS` | 48 | Hours without data before marking stale |
| `ROS_STALE_CLEANUP_DAYS` | 30 | Days after which stale recs are deleted |

### Key Files

- `internal/engine/recommend_all.go` — `StalenessThreshold()`, staleness evaluation
- `internal/engine/recommend_namespace.go` — Same logic for namespace recs
- `internal/engine/notifications.go` — `NotifStaleData` emission
- `internal/engine/retention.go` — Stale cleanup sweep
- `internal/api/handlers.go` — `?stale` query parameter parsing
- `internal/model/recommendation_set_native.go` — Removed hardcoded `WHERE stale = false`
- `internal/config/config.go` — Config fields

---

## F26: Full Idle/Abandoned Detection (REQ-6.1)

### Definitions

**Idle** — A container whose CPU *and* memory usage are below their respective
thresholds across all digest rows in the recommendation window:
- CPU idle threshold: 10 millicores (`defaultIdleThresholdMC`)
- Memory idle threshold: 10 MiB (10240 KiB, `defaultIdleThresholdMemKiB`)

Both conditions must be true for every row in the window. If *any* row exceeds
*either* threshold, the container is NOT idle.

**Abandoned** — A stricter form of idle: a container with **zero** CPU usage
AND **zero** memory usage across all digest rows. Abandoned implies idle.

### Behavior

| State | Criteria | Savings estimate |
|-------|----------|------------------|
| Active | Any row has CPU ≥ 10mc OR memory ≥ 10 MiB | Normal recommendation-based savings |
| Idle | All rows: CPU < 10mc AND memory < 10 MiB, but not all zero | 100% of current request cost (full recovery) |
| Abandoned | All rows: CPU = 0 AND memory = 0 | 100% of current request cost (full recovery) |

### Notifications

- `NotifIdleWorkload` (code 5) — emitted when idle but NOT abandoned
- `NotifAbandonedWorkload` (code 8) — emitted when abandoned (supersedes idle)

Priority: `abandoned > idle` — a container cannot have both notifications.

### Savings Calculation

For idle/abandoned containers, savings = 100% of current resource requests
(the entire allocation is recoverable):
- `savings_cpu_mc = current_cpu_request_mc`
- `savings_mem_kib = current_mem_request_kib`
- Dollar savings computed from these via cost rates

### Key Files

- `internal/engine/detect_idle.go` — `DetectIdle()` and `DetectAbandoned()`
- `internal/engine/types.go` — `IsAbandoned bool` on `ContainerRec`
- `internal/engine/recommend_all.go` — Wires abandoned detection
- `internal/engine/notifications.go` — Idle/abandoned notification precedence
- `internal/engine/savings.go` — `computeIdleSavings()`, idle savings path

---

## F54: Adoption Detection (REQ-10.7)

### Definition

**Adoption** means a user has applied a prior recommendation. Detection works
by comparing the container's **current resource requests** to the **most recent
prior recommendation**. If both CPU and memory requests are within 15% tolerance
of the previously recommended values, the recommendation is considered "adopted".

### Algorithm

```
tolerance = 0.15 (15%)

adopted = withinTolerance(currentCPURequest, priorRecommendedCPURequest, tolerance)
       && withinTolerance(currentMemRequest, priorRecommendedMemRequest, tolerance)

withinTolerance(actual, expected, tol):
  if expected == 0: return actual == 0
  return abs(actual - expected) <= expected * tol
```

### Behavior

| Condition | Result |
|-----------|--------|
| Current requests within 15% of prior recommendation | `recommendation_applied_at = NOW()` |
| No prior recommendation exists | No action |
| Prior recommendation already marked adopted | No action (idempotent) |
| Tolerance exceeded | No action |

### Database

- Column: `recommendation_sets.recommendation_applied_at TIMESTAMPTZ`
- Migration: `000046_add_recommendation_applied_at.up.sql`
- Only set once (first detection); `WHERE recommendation_applied_at IS NULL`

### Notification

- Code: `NotifRecApplied` (code 6)
- Appended to `notification_codes` array when adoption is detected
- Uses `array_append(array_remove(...))` to avoid duplicates

### Pipeline Integration

Adoption detection runs during report processing (`report_processor.go`):
1. Read old recommendations for the cluster
2. Run engine to compute new recommendations
3. Call `FindAdoptedContainers(results, oldRecs)` to identify adopted containers
4. Call `MarkAdopted(ctx, pool, orgID, clusterUUID, adoptedKeys)` to update DB

### No Backfill (By Design)

Adoption detection only triggers during new data ingestion. Existing
recommendations will **not** be retroactively marked as adopted if the user
applied them before this feature was deployed. This is intentional:
- Adoption requires comparing *current* resource requests (from incoming CSV)
  against *prior* recommendations — both must exist in the same pipeline run.
- Historical CSVs have already been processed and discarded from the pipeline.
- The first ingestion cycle after deployment will detect adoption for any
  container whose requests now match a prior recommendation.

### Key Files

- `internal/engine/adoption.go` — `FindAdoptedContainers()`, `MarkAdopted()`
- `internal/engine/adoption_test.go` — Unit tests
- `internal/engine/quality.go` — `DetectAdoption()` (core comparison logic)
- `internal/services/report_processor.go` — Pipeline integration
- `migrations/000046_add_recommendation_applied_at.up.sql`

---

## F33: Fleet-Level Summary (REQ-7.6)

### Definition

A single aggregate API endpoint that returns organization-wide statistics
across all container recommendations. Unlike listing individual recommendations
(which are paginated and per-container), this endpoint returns one object with
fleet-wide counts and totals.

### Endpoint

`GET /api/cost-management/v1/recommendations/openshift/fleet-summary`

### Response

```json
{
  "total_containers": 1500,
  "active_containers": 1420,
  "idle_containers": 45,
  "abandoned_containers": 12,
  "total_monthly_savings_usd": 4532.17,
  "cluster_count": 8,
  "currency": "USD"
}
```

### Field Definitions

| Field | Description |
|-------|-------------|
| `total_containers` | Count of all recommendation_sets rows (including stale) |
| `active_containers` | Count where `stale = false` |
| `idle_containers` | Count where `notification_codes` contains code 5 (`NotifIdleWorkload`) |
| `abandoned_containers` | Count where `notification_codes` contains code 8 (`NotifAbandonedWorkload`) |
| `total_monthly_savings_usd` | Sum of `estimated_savings_cents` for active recs |
| `cluster_count` | Distinct `cluster_uuid` count |
| `currency` | ISO 4217 code from Koku cost model (default `USD`) |

### Scope

- Filters: `org_id` (from auth), `term = 'medium_term'`, `engine = 'cost'`
- Only available when `UseNativeEngine = true` in config
- No pagination (single aggregate row)

### Key Files

- `internal/api/handlers_fleet.go` — `GetFleetSummary()` handler
- `internal/api/server.go` — Route registration
- `internal/api/handlers_fleet_integration_test.go` — Integration test

---

## Fleet Savings Summary

Cross-plugin aggregate of **persisted** monthly savings (container, node, PVC,
snapshot). Complements fleet-summary (container health counts only).

### Endpoint

`GET /api/cost-management/v1/recommendations/openshift/savings-summary`

### Query Parameters

| Parameter | Default | Description |
|-----------|---------|-------------|
| `engine` | `cost` | Engine profile for container and node totals (`cost` or `performance`). PVC and snapshot totals are engine-agnostic. |

### Response (abbreviated)

```json
{
  "currency": "USD",
  "total_estimated_savings_cents": 12450.00,
  "by_plugin": {
    "container": 8200.00,
    "node": 3100.00,
    "pvc": 950.00,
    "snapshot": 200.00,
    "gpu": 0
  },
  "by_cluster": [
    { "cluster_uuid": "...", "cluster_alias": "prod", "savings": 6200.00, "has_cost_data": true }
  ],
  "gpu_savings_note": "GPU savings are computed at API read time and are excluded from this summary."
}
```

GPU savings are excluded because they are computed at API read time, not persisted
at ingestion. See [architecture/cost-integration.md](architecture/cost-integration.md)
for formulas, negative savings semantics, and v2 roadmap (real-time recalculation,
GPU persistence, COST-7523 snapshot metric).

### Key Files

- `internal/api/handlers_savings_summary.go` — `GetFleetSavingsSummary()` handler
- `internal/api/handlers_savings_summary_integration_test.go` — Integration tests

---

## Notification Code Reference (Updated)

| Code | Constant | Meaning |
|------|----------|---------|
| 2 | `NotifStaleData` | No new data received within staleness threshold |
| 5 | `NotifIdleWorkload` | Container is idle (low but non-zero usage) |
| 6 | `NotifRecApplied` | Prior recommendation was adopted by user |
| 8 | `NotifAbandonedWorkload` | Container has zero usage (stronger than idle) |

---

## Testing

### Unit Tests

- `internal/engine/detect_idle_test.go` — Idle/abandoned detection
- `internal/engine/notifications_test.go` — Notification emission logic
- `internal/engine/retention_test.go` — Stale cleanup sweep
- `internal/engine/adoption_test.go` — Adoption detection
- `internal/engine/recommend_all_test.go` — Configurable staleness

### Integration Tests

- `internal/engine/savings_test.go` — `TestSavingsPipeline_Integration`
- `internal/api/handlers_fleet_integration_test.go` — Fleet summary endpoint

### Manual QE Verification

To test these features manually:

1. **Staleness**: Upload data, wait > `ROS_STALENESS_THRESHOLD_HOURS`, verify
   `?stale=only` returns the container and notification code 7 is present.

2. **Idle/Abandoned**: Generate nise data with near-zero or zero CPU/memory
   usage. Verify notification codes 5 or 8 and 100% savings estimate.

3. **Adoption**: Set container requests to match a prior recommendation
   (within 15%). Re-ingest data. Verify `recommendation_applied_at` is set
   and notification code 9 appears.

4. **Fleet Summary**: Call `/recommendations/fleet-summary` and verify counts
   match the total recommendation list.

5. **Fleet Savings Summary**: Call `/recommendations/savings-summary?engine=cost`
   and verify `by_plugin` totals match persisted savings columns.
