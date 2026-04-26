# Replica Count and Cost Impact / Savings Estimate

**Status**: Phase 1 (replica count) backend complete; **koku-ui changes pending** (display "N replicas" in recommendation detail view). Phase 2 (cost impact) design only.

## Motivation

Users need to see:
1. **"This workload runs N replicas"** -- the replica count of each workload, displayed in the recommendation detail view.
2. **"Applying this recommendation saves $X/month"** -- a dollar estimate of the cost impact of accepting a recommendation.

Replica count is a prerequisite for cost impact: saving 100m CPU on a workload with 5 replicas has 5x the impact of a single-replica workload. Similarly, pod runtime matters: saving 500 MiB on a pod that ran for 1 hour is less impactful than saving 10 MiB on a pod that ran for 730 hours.

## Architecture

### Data Flow

```
Prometheus
  kube_pod_container_status_ready  ──┐
  namespace_workload_pod:relabel   ──┤
                                     ▼
koku-metrics-operator
  New query: count ready pods per (workload, container, namespace)
  Broadcast count to per-pod CSV rows via many-to-one PromQL join
  New CSV column: workload_pod_count
                                     │
                                     ▼
ros-ocp-backend ingestion
  Parse workload_pod_count (primary) and pod name (fallback)
  Compute pod_count_min / pod_count_max / pod_count_avg per day
  Persist in daily_container_digests
                                     │
                                     ▼
ros-ocp-backend engine
  Aggregate pod counts across term window
  Persist in recommendation_sets
                                     │
                                     ▼
ros-ocp-backend API
  Expose { "replicas": { "min": N, "max": N, "avg": N } }
  Include in CSV export
                                     │
                                     ▼
koku-ui
  Display "N replicas" in recommendation detail view
```

---

## Phase 1: Replica Count

### Why kube_pod_container_status_ready in the operator

Pods can appear and disappear within a single hourly collection interval (e.g., HPA scale-up during a traffic spike, followed by scale-down 20 minutes later). Simply counting distinct pod names from existing CSV rows misses transient pods that existed between operator query instants.

`kube_pod_container_status_ready` with a `max_over_time(...[15m])` lookback captures any pod that was ready at any point in the last 15 minutes. Combined with a `count by (container, namespace, workload, workload_type)` aggregation and a broadcast join back to per-pod rows, this gives each pod row an accurate workload-level pod count.

### Operator Changes (koku-metrics-operator)

#### New Prometheus Query

The query follows the existing dual-namespace-label pattern (`label_insights_cost_management_optimizations` / `label_cost_management_optimizations`).

Conceptual structure (formatted for readability; the actual query is a single line):

```
per_pod_ready_with_workload =
  max_over_time(
    kube_pod_container_status_ready{container!='', container!='POD', pod!=''}
    [15m]
  )
  * on(pod, namespace) group_left(workload, workload_type)
    max_over_time(namespace_workload_pod:kube_pod_owner:relabel{pod!=''}[15m])
  * on(namespace) group_left
    kube_namespace_labels{<ns_filter>}

result =
  per_pod_ready_with_workload
  * on(container, namespace, workload, workload_type) group_left()
    count by (container, namespace, workload, workload_type) (
      per_pod_ready_with_workload
    )
```

**How the broadcast works**:
- `kube_pod_container_status_ready` returns 0 or 1 per (container, pod, namespace).
- `max_over_time(...[15m])` captures any pod that was ready in the last 15 minutes.
- `group_left(workload, workload_type)` enriches each pod with its owning workload.
- `count by (container, namespace, workload, workload_type)` produces the workload-level count.
- `on(...) group_left()` is a many-to-one join: each pod (left, many) gets the count (right, one).
- Result: each ready pod has value = 1 * count = count.

**Limitations**:
- The operator queries every hour. The `[15m]` lookback means pods that existed only between minute 15 and minute 45 of an hour are missed. This is a limitation shared with all other ROS metrics and is acceptable.
- If `kube_pod_container_status_ready` is unavailable (very old kube-state-metrics), the column will be empty. ros-ocp-backend falls back to counting distinct pod names.

#### Files Changed

| File | Change |
|------|--------|
| `internal/collector/queries.go` | Add `"ros:workload_pod_count"` to `QueryMap`. Add query entry to `rosContainerQueries` with `RowKey: ["container", "pod", "namespace"]` and `ValName: "workload-pod-count"`. |
| `internal/collector/types.go` | Add `WorkloadPodCount string \`mapstructure:"workload-pod-count"\`` to `rosContainerRow`. Update `csvHeader()` (append `"workload_pod_count"`) and `csvRow()`. |

### Backend Changes (ros-ocp-backend)

#### Ingestion

| File | Change |
|------|--------|
| `internal/ingestion/models.go` | Add `Pod string` and `WorkloadPodCount int64` to `MetricRow`. |
| `internal/ingestion/csvparser.go` | Parse `pod` column (**required**) and `workload_pod_count` column (**optional**, defaults to 0). |
| `internal/ingestion/digest.go` | Add `PodCountMin`, `PodCountMax`, `PodCountAvg` (int64) to `ContainerDigestResult`. Compute in `ComputeContainerDigest` using the two-strategy approach below. |

**Two strategies for computing pod count per day**:

1. **Primary (workload_pod_count column present and non-zero)**: Group rows by `IntervalStart` hour. For each hourly bucket, take the max `WorkloadPodCount` across rows (all pods in a workload should report the same count; max is defensive). Compute min/max/avg of those hourly values across the day.

2. **Fallback (column absent or all zeros)**: Group rows by `IntervalStart` hour. For each hourly bucket, count distinct `row.Pod` names. Compute min/max/avg of those hourly counts.

#### Schema Migration (000039)

```sql
-- daily_container_digests
ALTER TABLE daily_container_digests
  ADD COLUMN IF NOT EXISTS pod_count_min INTEGER,
  ADD COLUMN IF NOT EXISTS pod_count_max INTEGER,
  ADD COLUMN IF NOT EXISTS pod_count_avg INTEGER;

-- recommendation_sets
ALTER TABLE recommendation_sets
  ADD COLUMN IF NOT EXISTS pod_count_min INTEGER,
  ADD COLUMN IF NOT EXISTS pod_count_max INTEGER,
  ADD COLUMN IF NOT EXISTS pod_count_avg INTEGER;

-- namespace_recommendation_sets
ALTER TABLE namespace_recommendation_sets
  ADD COLUMN IF NOT EXISTS pod_count_min INTEGER,
  ADD COLUMN IF NOT EXISTS pod_count_max INTEGER,
  ADD COLUMN IF NOT EXISTS pod_count_avg INTEGER;
```

#### Engine

| File | Change |
|------|--------|
| `internal/engine/types.go` | Add `PodCountMin`, `PodCountMax`, `PodCountAvg` (int64) to `DigestRow` and `ContainerRec`. |
| `internal/engine/recommend_all.go` | Update SELECT from `daily_container_digests` to include pod count columns. Aggregate across term window: min of mins, max of maxes, weighted avg of avgs. Update `WriteRecommendations` upsert. |
| `internal/ingestion/pipeline.go` | Update upsert SQL for `daily_container_digests` to include pod count columns. |

#### API Response

| File | Change |
|------|--------|
| `internal/model/detail_response.go` | Add `Replicas *ReplicaInfo` to `DetailRecommendations`. Define `ReplicaInfo{Min, Max, Avg int}`. Populate in `BuildDetailResponse`. |
| `internal/model/recommendation_set_native.go` | Add pod count columns to `NativeRecommendationRow` and SELECT queries. |
| `internal/api/utils.go` | Update CSV export headers and values. |
| `internal/api/common.go` | Update `NativeCSVHeader` / `NativeNSCSVHeader` if applicable. |

#### API Response Shape

```json
{
  "recommendations": {
    "replicas": {
      "min": 2,
      "max": 5,
      "avg": 3
    },
    "current": { ... },
    "recommendation_terms": { ... }
  }
}
```

---

## Backward Compatibility

### Older operator versions (no workload_pod_count column)

Fully backward compatible. The `workload_pod_count` CSV column is parsed as **optional** in ros-ocp-backend's `csvparser.go`. When the column is absent (or all zeros), the digest computation falls back to counting distinct `pod` names per hourly interval. The `pod` column has existed in the operator's ROS container CSV since the beginning (`rosContainerRow.Pod` in `types.go`), so it is safe to make it a required field in the native parser.

### Kruize (non-native) recommendation engine

No impact. The Kruize and native engine pipelines are completely separate code paths:

| Concern | Kruize path | Native path |
|---------|-------------|-------------|
| **CSV parsing** | Uses `ReadCSVFromUrl` + Gota dataframe + `GetColumnMapping` (in `internal/utils/aggregator.go`). Does **not** call `ParseCSVRows` or `ComputeContainerDigest`. | Uses `ParseCSVRows` → `GroupCSVRows` → `ComputeContainerDigest` (in `internal/ingestion/`). |
| **daily_container_digests** | Never reads or writes this table. | Primary data store for digests. |
| **recommendation_sets** | Writes via GORM `CreateRecommendationSet` using JSONB `recommendations` column + `workload_id`, `container_name`. | Writes via `WriteRecommendations` using relational columns (`rec_cpu_request_mc`, `term`, `engine`, etc.). |
| **API handlers** | `GetRecommendationSetList` / `GetRecommendationSet` (legacy JSONB queries). | `GetRecommendationSetListWithFallback` / `GetRecommendationSetWithFallback` (native relational queries). |

The path is gated by `ROS_USE_NATIVE_ENGINE` (default `true`) in `internal/services/report_processor.go` and `internal/api/server.go`.

**Schema safety**: New columns on `recommendation_sets` must be **nullable** (no `NOT NULL` constraint, no `DEFAULT` clause that could trigger table rewrites). The migration uses `ADD COLUMN IF NOT EXISTS` with implicit NULL default, so Kruize's GORM inserts (which don't set these columns) will simply leave them as NULL. This is safe.

**Schema safety on `daily_container_digests`**: Kruize never touches this table, so any column additions are invisible to the Kruize path.

### Mixed-version deployments

During a rollout where the operator is updated before ros-ocp-backend (or vice versa):
- **New operator + old backend**: The CSV has an extra `workload_pod_count` column. The old parser ignores unknown columns (the `switch` in `buildColumnIndex` simply skips unrecognized headers). No breakage.
- **Old operator + new backend**: The CSV lacks `workload_pod_count`. The new parser treats it as optional (index stays -1). The fallback path counts distinct `pod` names. No breakage.
- **New backend + Kruize mode**: The native parser is never called. Kruize CSV validation (`hasMissingColumnsCSV`) already requires the `pod` column via `CSVColumnMapping`. New DB columns are nullable. No breakage.

---

## Test Strategy

### Operator (koku-metrics-operator)

**Unit tests** (Go, `internal/collector/`):

| Test | What it verifies |
|------|-----------------|
| Query result merge | A `workload-pod-count` query result merges into the correct per-pod row in `mappedResults`, using the same `(container, pod, namespace)` RowKey as other queries. |
| CSV output | `rosContainerRow.csvHeader()` includes `workload_pod_count` at the expected position. `csvRow()` emits the count value. |
| Missing metric gracefully handled | If `kube_pod_container_status_ready` returns no data (old kube-state-metrics), the `workload-pod-count` field is empty, and the row is still valid. |

### ros-ocp-backend: Ingestion

**Unit tests** (Go, `internal/ingestion/`):

| Test | What it verifies |
|------|-----------------|
| `ParseCSVRows` with new column | CSV with `workload_pod_count` column parses `MetricRow.WorkloadPodCount` correctly. |
| `ParseCSVRows` without new column | CSV missing `workload_pod_count` column parses successfully; `MetricRow.WorkloadPodCount` is 0. |
| `ParseCSVRows` with `pod` column | `MetricRow.Pod` is populated from the `pod` CSV column. |
| `ComputeContainerDigest` primary path | Input rows with `WorkloadPodCount > 0`: verify `PodCountMin/Max/Avg` are computed from the max of `WorkloadPodCount` per hourly bucket, then min/max/avg across buckets. |
| `ComputeContainerDigest` fallback path | Input rows with `WorkloadPodCount == 0`: verify `PodCountMin/Max/Avg` are computed from distinct `Pod` names per hourly bucket. |
| HPA scaling scenario | 3 pods in hour 0, 5 pods in hour 1, 3 pods in hour 2: verify `PodCountMin=3, PodCountMax=5, PodCountAvg=~3.67`. |
| Pod restart scenario | Pod "foo-abc" in hour 0, pod "foo-xyz" (replacement) in hour 0: verify count=2 for that hour (two distinct names were observed). |
| Single pod, 24 hours | Verify `PodCountMin=1, PodCountMax=1, PodCountAvg=1`. |

### ros-ocp-backend: Engine

**Unit tests** (Go, `internal/engine/`):

| Test | What it verifies |
|------|-----------------|
| `DigestRow` loading | Pod count columns are read from DB into `DigestRow` struct. |
| Term-window aggregation | Given 7 days of digests with varying pod counts, the `ContainerRec` has correct min (min of mins), max (max of maxes), avg (avg of avgs). |
| NULL pod counts | Digest rows from before the migration (NULL columns) default to 0; engine handles gracefully without panicking. |
| `WriteRecommendations` | Pod count columns are persisted in the `recommendation_sets` upsert. Verify with a read-back query. |

### ros-ocp-backend: API

**Integration tests** (Go, `internal/api/`):

| Test | What it verifies |
|------|-----------------|
| Detail response includes `replicas` | `GET /recommendations/openshift/:id` returns `recommendations.replicas` with `min`, `max`, `avg` fields. |
| List response includes `replicas` | `GET /recommendations/openshift` returns replica info per recommendation. |
| CSV export includes pod count | CSV download includes `pod_count_min`, `pod_count_max`, `pod_count_avg` columns with correct values. |
| No pod count data (NULL) | When pod count columns are NULL (pre-migration data), `replicas` is omitted from the response (not `{"min":0,"max":0,"avg":0}`). |

### ros-ocp-backend: Migration

**Migration test** (run against test DB):

| Test | What it verifies |
|------|-----------------|
| Up migration | Columns exist after migration; existing rows have NULL values; inserts with pod count values succeed. |
| Down migration | Columns are dropped; existing data is preserved (minus dropped columns). |
| Idempotent | Running up migration twice does not error (`IF NOT EXISTS`). |

### End-to-End (manual, on Apollo cluster)

| Test | What it verifies |
|------|-----------------|
| New operator + new backend | Ingest nise data, verify `workload_pod_count` in CSV, verify `replicas` in API response. |
| Old operator CSV + new backend | Upload a tarball from a pre-change operator; verify fallback path produces pod counts from distinct pod names. |
| Kruize mode | Set `ROS_USE_NATIVE_ENGINE=false`, ingest data, verify Kruize recommendations still work (no errors in logs, API returns JSONB recommendations). |

---

## Phase 2: Cost Impact / Savings Estimate (Design Only)

### Goal

Populate the existing `EstimatedSavingsUSD float32` field on `ContainerRec` (already in the engine struct, currently always 0).

### Data Sources

| Data | Source | Access Method |
|------|--------|---------------|
| Cost model rates ($/core-hour, $/GiB-hour) | Koku `cost_model` table (tenant schema) | Cross-DB read-only PostgreSQL query |
| Cloud infrastructure costs (distributed) | Koku `reporting_ocpusagelineitem_daily_summary` (tenant schema) | Cross-DB read-only PostgreSQL query |
| Pod hours per workload | `daily_container_digests.sample_count` (ros-ocp-backend DB) | Already available |
| Replica count | `daily_container_digests.pod_count_*` (ros-ocp-backend DB) | From Phase 1 |

### Cost Calculation

```
cpu_delta_cores = (current_cpu_request - recommended_cpu_request) / 1000
mem_delta_gib   = (current_mem_request - recommended_mem_request) / (1024 * 1024)

# Effective rate from Koku's summary table (namespace-level average)
cpu_rate = SUM(cost_model_cpu_cost + distributed_cpu_cost) / SUM(pod_usage_cpu_core_hours)
           WHERE namespace = <workload_namespace> AND usage_start IN <term_window>

mem_rate = SUM(cost_model_memory_cost + distributed_mem_cost) / SUM(pod_usage_memory_gigabyte_hours)
           WHERE namespace = <workload_namespace> AND usage_start IN <term_window>

# Total savings: delta * rate * pod_hours_in_month
estimated_savings = (cpu_delta_cores * cpu_rate + mem_delta_gib * mem_rate) * monthly_pod_hours
```

**Positive savings** = recommendation reduces costs. **Negative savings** = recommendation increases costs (e.g., under-provisioned workload needs more resources).

### Cross-DB Read Architecture

```go
type CostDataProvider interface {
    GetNamespaceCostRates(ctx context.Context, orgID, clusterUUID, namespace string,
        startDate, endDate time.Time) (*CostRates, error)
}

type CostRates struct {
    CPURatePerCoreHour float64
    MemRatePerGiBHour  float64
}
```

**Implementation A (DBCostProvider)**: Direct read-only PostgreSQL connection to Koku's database. Config via `KOKU_DB_HOST`, `KOKU_DB_PORT`, `KOKU_DB_NAME`, `KOKU_DB_USER`, `KOKU_DB_PASSWORD`. Connection enforces `default_transaction_read_only=on`.

**Implementation B (APICostProvider, future)**: REST API call to a new Koku endpoint. Requires Koku changes; cleaner for SaaS.

### Important Caveats

1. **Cloud savings are approximate**: Reducing one workload's CPU request only reduces infrastructure costs if an entire node can be de-provisioned. Otherwise, the freed capacity remains as unused overhead. Show as "estimated infrastructure impact" with appropriate caveats.

2. **Cost model savings are accurate**: If a cost model defines $/core-hour, the savings calculation is exact.

3. **No cost model = no cost savings**: If no cost model is configured and no cloud provider integration exists, `EstimatedSavingsUSD` stays 0 (with a notification explaining why).

4. **Namespace-level rates**: Costs are aggregated at namespace level in Koku's summary tables. Per-workload rates are an approximation based on the namespace average. This is acceptable because cost model rates are uniform within a namespace, and cloud costs are distributed proportionally to usage.
