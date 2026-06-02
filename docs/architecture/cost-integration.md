# Cost Data Integration Contract

This document describes the integration between ROS-OCP-Backend and Koku for cost/savings estimation.

For recommendation thresholds and term configuration (not cost-specific), see
[Recommendation Engine Reference](recommendation-engines.md).

## Overview

ROS-OCP-Backend fetches cost model rates from Koku to compute estimated monthly savings for each recommendation. The integration uses Koku's internal `effective_rates` endpoint.

## Endpoint

```
GET {KOKU_MASU_URL}/api/cost-management/v1/effective_rates/
```

### Query Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `org_id` | string | Organization ID (without `org` prefix) |
| `cluster_id` | string | Cluster UUID |
| `start_date` | string | Start date (YYYY-MM-DD, UTC) |
| `end_date` | string | End date (YYYY-MM-DD, UTC) |

### Response Schema

```json
{
  "cluster_id": "abc123-...",
  "provider_uuid": "def456-...",
  "currency": "USD",
  "distribution_type": "cpu",
  "markup_pct": 10.0,
  "configured_rates": {
    "cpu_core_usage_per_hour": {
      "infrastructure": 0.0,
      "supplementary": 0.007
    },
    "cpu_core_request_per_hour": {
      "infrastructure": 0.0,
      "supplementary": 0.2
    },
    "memory_gb_usage_per_hour": {
      "infrastructure": 0.0,
      "supplementary": 0.009
    },
    "memory_gb_request_per_hour": {
      "infrastructure": 0.0,
      "supplementary": 0.05
    },
    "node_cost_per_month": {
      "infrastructure": 1000.0,
      "supplementary": 0.0
    },
    "gpu_cost_per_month": {
      "infrastructure": 1800.0,
      "supplementary": 0.0
    },
    "storage_gb_request_per_month": {
      "infrastructure": 0.0,
      "supplementary": 0.01
    },
    "storage_gb_usage_per_month": {
      "infrastructure": 0.0,
      "supplementary": 0.01
    }
  },
  "namespace_aggregates": {
    "my-namespace": {
      "cost_model_cpu_cost": 150.25,
      "cost_model_memory_cost": 80.50,
      "infrastructure_cost": 500.00,
      "distributed_cost": 200.00,
      "cpu_usage_hours": 720.0,
      "cpu_request_hours": 1440.0,
      "mem_usage_hours": 360.0,
      "mem_request_hours": 720.0
    }
  }
}
```

## How ROS Uses Cost Data

### OCP-on-cloud clusters (OCP on AWS / Azure / GCP)

When Koku has **both** the OpenShift source and the matching cloud source configured
and OCP-on-cloud correlation has run, Koku back-populates `infrastructure_raw_cost`
on `reporting_ocpusagelineitem_daily_summary` with amortized cloud infrastructure
spend (for example EC2/EBS on AWS). The masu `effective_rates` endpoint sums this
into `namespace_aggregates[].infrastructure_cost` (plus markup in the same field).

ROS consumes that aggregate in [`internal/engine/savings.go`](../../internal/engine/savings.go)
alongside cost-model CPU/memory costs and distributed overhead. **No extra ROS work
is required for OCP-on-cloud** — container savings already reflect real correlated
cloud spend when Koku ingestion and correlation are healthy.

Prerequisites on the Koku side:

- OCP and cloud sources ingested for the same cluster
- OCP-on-cloud correlation processed (see Koku `back_populate_ocp_infrastructure_costs`)
- An OCP cost model assigned (supplementary rates and distribution type)

On-prem OCP-only clusters use the same code path; `infrastructure_cost` reflects
cost-model infrastructure rates (node/cluster monthly costs) rather than cloud CUR
data.

Koku implementation: `koku/masu/api/effective_rates.py` (sibling repository).

### Stale OCP sources (SaaS operations)

Separate from cost/savings, Koku can detect OCP sources with no manifest
upload for **3+ days** and emit a `cm-operator-stale` platform notification
(Celery task `check_for_stale_ocp_source`). That path is **SaaS-only** (Kafka
to console.redhat.com); on-prem relies on the operator CRD and ROS staleness
(notification code **2**). Manual run: Masu
`GET /api/cost-management/v1/notifications/?stale_ocp_check`. Details:
[Stale detection — Koku-side](../operations/stale-detection.md#koku-side-stale-source-detection-saas).

### Container Savings

1. Fetch effective rates once per ingestion cycle per cluster ([`report_processor.go`](../../internal/services/report_processor.go))
2. Look up per-namespace aggregates in `namespace_aggregates` (includes `infrastructure_cost` from OCP-on-cloud correlation when available)
3. Compute savings in [`ApplySavingsEstimates()`](../../internal/engine/savings.go) and persist `estimated_monthly_savings_usd` on ingest (exposed as `estimated_monthly_savings_usd` in API JSON)

**Formula** (`hours_per_month = 730`, replica count from `desired_replicas` or `pod_count_avg`):

```
cpu_delta_cores = (current_cpu_request_mc - rec_cpu_request_mc) / 1000
mem_delta_gib   = (current_mem_request_kib - rec_mem_request_kib) / (1024 × 1024)

model_cpu_rate = cost_model_cpu_cost / cpu_request_hours
model_mem_rate = cost_model_memory_cost / mem_request_hours
model_savings  = (cpu_delta_cores × model_cpu_rate + mem_delta_gib × model_mem_rate)
                 × hours_per_month × replicas

infra_pool = infrastructure_cost + distributed_cost
infra_rate = infra_pool / cpu_request_hours   (distribution_type = cpu)
          or infra_pool / mem_request_hours   (distribution_type = memory)
infra_savings = (cpu_delta_cores or mem_delta_gib) × infra_rate × hours_per_month × replicas

estimated_monthly_savings = round(model_savings + infra_savings, 2)
```

Idle/abandoned workloads use the same rates but treat **100%** of current CPU + memory allocation (plus apportioned infra/distributed overhead) as recoverable savings.

If the namespace is missing from `namespace_aggregates`, savings stay `$0` and `NotifNoCostData` (code **25**) is appended.

### Node Savings (CPU/memory utilization)

Computed at **ingestion** (same cycle as container recommendations). Each node produces **two engine rows** per term (`cost` and `performance`), mirroring the container `recommendation_engines` pattern:

| Engine | Target utilization | Consolidation |
|--------|-------------------|---------------|
| `cost` | 80% | Recommend node consolidation when underutilized (workloads fit at 80% target) |
| `performance` | 55% | Consolidate only with extreme waste (underutilized + full spare node of headroom) |

Shared classification (underutilized, overcommitted, stranded) uses the same thresholds for both engines.

1. [`runNodeRecommendations()`](../../internal/services/report_processor.go) reuses the cluster's `effective_rates` fetch from container processing
2. [`RecommendNodes()`](../../internal/engine/recommend_nodes.go) classifies once per (node, term), then sizes per engine via [`sizeNodeForEngine()`](../../internal/engine/recommend_nodes.go)
3. [`ApplyNodeSavings()`](../../internal/engine/node_savings.go) compares current vs recommended node CPU cores and memory GiB per engine row
4. Rates from `configured_rates`: `cpu_core_usage_per_hour`, `memory_gb_usage_per_hour`, `node_cost_per_month`
5. When underutilized, `node_cost_per_month` is included per row as `node_count_reduction × node_cost_per_month`. Reduction counts come from [`applyInstanceTypeConsolidation`](../../internal/engine/recommend_nodes.go) (Level 3: group by `instance_type` when present) or per-node binary logic when `instance_type` is empty. **`machineset_name`** on digests is surfaced in list JSON for fleet/UI grouping; consolidation math still keys off `instance_type` or capacity keys until Tier 2 MachineSet plugins ship.
6. **Instance type suggestions** (migration **000122**): for stranded nodes, [`applyFleetInstanceTypeSuggestions`](../../internal/engine/recommend_nodes.go) sets `suggested_instance_type` / `instance_type_reason` using instance types already seen in the cluster (ratio-based, no external catalog).
7. Persisted on `node_recommendations` with PK `(org_id, cluster_uuid, node, term, engine)` (migration **000071**): `estimated_monthly_savings_usd`, plus sizing fields from migration **000072** (`recommended_cpu_cores`, `recommended_memory_gib`, `node_count_reduction`)

**Node allocatable capacity:** Ingestion prefers **`node_allocatable_cpu_cores`** and
**`node_allocatable_memory_bytes`** from koku-metrics-operator ROS container CSV rows
(when present). These populate `daily_node_digests` allocatable columns used for
utilization ratios. If absent, the pipeline falls back to
`ROS_NODE_ALLOCATABLE_FACTOR` × observed request totals (default **0.93**), preserving
backward compatibility with older operator builds.

**List API:** `GET /recommendations/openshift/nodes` returns one object per node with nested `recommendation_terms.<term>.recommendation_engines.{cost,performance}`. Shared classification and metrics come from the medium-term cost row when present. Pagination and default sort (`order_by=estimated_monthly_savings_usd`) operate on distinct nodes.

Fleet savings summary aggregates container and node savings for the selected **`engine`** and **`term`** query parameters on `GET /recommendations/openshift/savings-summary` (defaults: **`cost`**, **`medium`**), consistent with container list behavior. PVC and snapshot totals are engine-agnostic.

**Formula** (rates = infrastructure + supplementary from `configured_rates`):

```
cpu_savings  = (current_cpu_cores - recommended_cpu_cores) × cpu_core_usage_per_hour × 730
mem_savings  = (current_memory_gib - recommended_memory_gib) × memory_gb_usage_per_hour × 730
node_savings = node_count_reduction × node_cost_per_month

estimated_monthly_savings = round(cpu_savings + mem_savings + node_savings, 2)
```

### PVC Savings

Computed at **ingestion** during storage CSV processing:

1. [`processStorageCSVNative()`](../../internal/services/report_processor.go) fetches `effective_rates` when savings are enabled
2. [`ApplyPVCSavings()`](../../internal/engine/pvc_savings.go) uses `(request_gib - recommended_gib) × storage_rate_per_month`
3. Prefers `storage_gb_request_per_month`; falls back to `storage_gb_usage_per_month`
4. Persisted on `pvc_recommendation_sets.estimated_monthly_savings_usd` (migration **000070**; API field `estimated_monthly_savings_usd`)

**Formula** (`storage_rate` = `storage_gb_request_per_month`, falling back to `storage_gb_usage_per_month` when request rate is zero):

```
current_gib     = request_bytes (or capacity_bytes if request is zero) / 1024³
recommended_gib = recommended_bytes / 1024³
delta_gib       = current_gib - recommended_gib

estimated_monthly_savings = round(delta_gib × storage_rate, 2)
```

Positive `delta_gib` means the PVC is oversized; near-full/orphaned PVCs with no shrink recommendation return `$0`.

### GPU Savings

Computed at **API read time** (not stored on ingest):

1. [`internal/api/gpu_enrichment.go`](../../internal/api/gpu_enrichment.go) fetches `effective_rates` per cluster when listing container recommendations with GPU data
2. Use `gpu_cost_per_month` (infrastructure + supplementary) from `configured_rates`
3. For idle GPUs: savings = full `gpu_cost_per_month`
4. For MIG candidates: savings = `(1 - recommended_slices / total_slices) × gpu_cost_per_month`
5. Node GPU time-slicing: [`internal/api/handlers_node_recs.go`](../../internal/api/handlers_node_recs.go) calls [`ComputeNodeTimeslicingRec()`](../../internal/engine/gpu_timeslicing.go) with the same rates → `total_node_savings_usd` and per-container `estimated_monthly_timeslicing_savings_usd`

GPU API enrichment does **not** append `NotifNoCostData`; savings fields are omitted or `$0` when Masu is unavailable.

### Snapshot cost (dynamic default from effective-rates)

Snapshot recommendations estimate recoverable monthly cost as
`restore_size_bytes × cost_per_gib_month_usd`. The rate is resolved at **ingestion**
with this priority:

1. Per-org Settings API value (`cost_per_gib_month_usd` in `snapshot_settings`) — user explicitly configured
2. `ROS_SNAPSHOT_COST_PER_GIB_MONTH` env var — admin override (locked in Settings API)
3. `storage_gb_usage_per_month` from Masu `effective_rates` — sum of infrastructure + supplementary from the cluster's OCP cost model (PVC storage usage rate; a better proxy than a hardcoded default)
4. Compiled default `$0.05`/GiB/month

Step 3 runs only when `ROS_SAVINGS_ESTIMATES_ENABLED=true` and the Masu fetch
succeeds ([`processSnapshotCSVNative()`](../../internal/services/report_processor.go)
→ [`ResolveSnapshotSettings()`](../../internal/engine/snapshot_settings.go)).
When the kill-switch is off or Masu is unreachable, ingestion uses steps 1, 2, and 4 only.

The Settings API GET/PUT path does **not** expose the dynamic effective-rates
value — it returns the stored org setting, env-locked value, or compiled default.
See [features-f-snapshot-staleness.md](../features-f-snapshot-staleness.md).

**v1 placeholder:** `cost_per_gib_month_usd` (Settings API / env / compiled default)
is an **approximate** holding-cost knob for FinOps prioritization, not billing truth.
Even step 3 (`storage_gb_usage_per_month`) is a PVC usage proxy, not cloud snapshot
line items.

**Future ([COST-7523](https://redhat.atlassian.net/browse/COST-7523)):** Accurate
recoverable cost should be derived from:

- **Cloud provider billing** ingested by Koku (CUR and equivalent exports), where
  snapshot or backup storage charges exist; and/or
- **OCP cost model** user-defined storage rates (today's proxy; future dedicated
  snapshot metric such as `snapshot_gb_per_month`).

Koku work under COST-7523 introduces an **effective cost internal endpoint** (Masu
or successor to today's `effective_rates` path). ROS will call that endpoint to
resolve cluster- and StorageClass-aware snapshot rates instead of relying on the flat
`cost_per_gib_month_usd` chain. Per-StorageClass cost overrides in snapshot settings v2
are tracked in [COST-7563](https://redhat.atlassian.net/browse/COST-7563). See
[features-f-snapshot-staleness.md](../features-f-snapshot-staleness.md) for component
breakdown (Koku, koku-ui, operator, ROS). Until COST-7523 ships, keep tuning
`/settings/snapshot` for on-prem and demo clusters.

## Plugin savings coverage

| Plugin | Dollar estimates | When computed | Data source |
|--------|------------------|---------------|-------------|
| **Container** | Yes | Ingestion | Masu `effective_rates` → DB `estimated_monthly_savings_usd` (API: `estimated_monthly_savings_usd`) |
| **GPU** (container detail) | Yes | API read | Masu `effective_rates` → `estimated_monthly_gpu_savings_usd` |
| **Node GPU time-slicing** | Yes | API read | Masu `effective_rates` → `total_node_savings_usd` / per-container share |
| **Node** (CPU/memory utilization) | Yes | Ingestion | Masu `effective_rates` → DB per engine (`cost` 80%, `performance` 55%) → nested API `estimated_monthly_savings_usd` |
| **Namespace** | No | — | CPU/memory recommendations only; no USD field |
| **PVC** | Yes | Ingestion | Masu `effective_rates` → DB `estimated_monthly_savings_usd` (API: `estimated_monthly_savings_usd`) |
| **Snapshot** | Yes (recoverable cost) | Ingestion | Settings API / env / effective-rates `storage_gb_usage_per_month` / default |

Migration **000070** adds `estimated_monthly_savings_usd` to `node_recommendations` and
`pvc_recommendation_sets`. Container savings use the existing column on `recommendation_sets`
(since migration 000026).

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `KOKU_MASU_URL` | `""` | Koku masu API base URL (e.g., `http://cost-onprem-masu:5042`) |
| `ROS_SAVINGS_ESTIMATES_ENABLED` | `true` | Kill-switch — set `false` to skip all Masu cost fetches |
| `GLOBAL_HTTP_CLIENT_TIMEOUT_SECS` | 30 | HTTP timeout for cost data requests |

## Currency field

Koku's `effective_rates` response includes a top-level `currency` field (ISO 4217,
default `"USD"`). ROS propagates it to API responses that expose monetary values.
Existing `_usd` JSON field names are unchanged for backward compatibility — use
`currency` to format amounts when the cost model uses a non-USD unit.

| Endpoint | `currency` location |
|----------|---------------------|
| Container list / detail | Top-level on each recommendation object |
| GPU blocks on container detail | Top-level on each `gpu.<model>` block |
| `GET .../nodes` (CPU/memory utilization) | `meta.currency` |
| `GET .../gpu/timeslicing` | Top-level on list response |
| `GET .../pvcs` | `meta.currency` |
| `GET .../snapshots` | `meta.currency` |
| `GET .../fleet-summary` | Top-level |
| `GET .../savings-summary` | Top-level |

When Masu is unavailable or savings are disabled, ROS defaults to `"USD"`.
Deploy **koku** (Masu `effective_rates` currency field) before or with
ros-ocp-backend. See [upgrade-runbook.md](../upgrade-runbook.md).

Set `ROS_SAVINGS_ESTIMATES_ENABLED=false` on **ros-processor** and **ros-api** to
disable Masu-based dollar savings:

- No HTTP calls to Masu `effective_rates` for container, node, PVC ingest or GPU API enrichment
- Container, node, PVC, and GPU recommendations still produced; savings fields are `$0` / omitted
- `NotifNoCostData` (notification code **25**) on **container, node, and PVC** when savings cannot be computed (GPU omits dollar fields without code 25)
- Snapshot recoverable-cost estimates skip the dynamic effective-rates default (steps 1, 2, and 4 of the snapshot cost chain still apply)

Implementation: [`internal/config/config.go`](../../internal/config/config.go),
[`getCostDataProvider()`](../../internal/services/report_processor.go),
[`getGPUCostProvider()`](../../internal/api/gpu_enrichment.go).

When `KOKU_MASU_URL` is empty (and savings are enabled), a `NilCostDataProvider`
is used — same behavior as the kill-switch: all savings values are $0.00, and
`NotifNoCostData` is appended.

## Error Handling

| Scenario | Behavior |
|----------|----------|
| `ROS_SAVINGS_ESTIMATES_ENABLED=false` | No Masu HTTP calls; ingestion savings `$0`; `NotifNoCostData` on container, node, PVC |
| `KOKU_MASU_URL` empty | Same as kill-switch — `NilCostDataProvider` |
| Koku/Masu unreachable | Log warning, use `NilCostDataProvider` for this cycle |
| Non-200 response | Log error with status + body, skip savings for this cluster |
| JSON decode failure | Log error, skip savings for this cluster |
| Empty `configured_rates` | Savings computed as `$0.00` (no cost model assigned) |
| Namespace missing from aggregates | Container savings `$0`, `NotifNoCostData` for that workload |

### Notification code 25 — `NotifNoCostData`

Defined in migration `000040_add_no_cost_data_notification` as `NO_COST_DATA` (severity **INFO**):

> No cost data available — savings estimate not computed

Emitted on **container**, **node**, and **PVC** recommendations when Masu cost data is unavailable,
`ROS_SAVINGS_ESTIMATES_ENABLED=false`, or (for containers) the workload namespace is absent from
`namespace_aggregates`. API responses expose this as notification code **25** in the
`notifications` object (key `"25"`) or `notification_codes` array.

GPU enrichment skips this notification; dollar fields are simply null/zero.

## Authentication

The `effective_rates` endpoint is an **internal masu API** endpoint — it does NOT require `x-rh-identity` authentication. It is only accessible within the cluster network (service-to-service communication). In on-prem deployments, network policies restrict access.

## Freshness

| Savings type | Refresh cadence |
|--------------|-----------------|
| Container (`estimated_monthly_savings_usd`) | Ingestion per cluster; also on Koku cost model update when savings recalculation is enabled |
| Node CPU/memory (`estimated_monthly_savings_usd`) | Ingestion per cluster; also on Koku cost model update when savings recalculation is enabled |
| PVC (`estimated_monthly_savings_usd`) | Storage ingestion per cluster; also on Koku cost model update when savings recalculation is enabled |
| Quota / cluster-quota (`estimated_savings` / `savings_dollars_monthly`) | Ingestion per cluster; also on Koku cost model update when savings recalculation is enabled (tighten rows only) |
| GPU / node time-slicing | On each API request that enriches GPU data |
| Snapshot recoverable cost | On each snapshot ingestion cycle (resolved rate from settings / env / effective-rates / default) |

The `effective_rates` date range covers the most recent 30 days (configurable via
lookback). Koku cost model updates trigger a savings-only recalculation for
container, node, PVC, quota, and cluster-quota when configured (see [Savings recalculation after cost model changes](#savings-recalculation-after-cost-model-changes)); GPU still refreshes on the next API call.

## Negative savings

Savings values (`estimated_monthly_savings_usd`, fleet summary totals, and
per-plugin aggregates) may be **negative**. This is intentional, not a bug.

When a recommendation suggests **more** CPU, memory, storage, or node capacity
than the workload currently requests, the delta is negative and the savings
field reflects an **additional monthly cost** to implement the recommendation.
These upsizing recommendations typically target reliability or performance
(for example, reducing OOM risk, addressing CPU throttling, or expanding a
near-full PVC).

**UI guidance:** koku-ui should display negative savings as an additional cost
(for example, "Additional cost: $X/month") rather than "Savings: -$X/month".
Positive values remain standard savings opportunities.

**Product documentation note for technical writers:** Red Hat Lightspeed Cost
Management end-user documentation should explain that negative savings indicate
a known cost increase when adopting a recommendation that improves performance
or reliability.

## Fleet savings summary API

`GET /api/cost-management/v1/recommendations/openshift/savings-summary` aggregates
persisted savings across all clusters for the authenticated organization.

Implementation: [`internal/api/handlers_savings_summary.go`](../../internal/api/handlers_savings_summary.go).

| Field | Source |
|-------|--------|
| `by_plugin.container` | `SUM(estimated_monthly_savings_usd)` on active `recommendation_sets` (medium/cost) |
| `by_plugin.node` | `SUM(estimated_monthly_savings_usd)` on `node_recommendations` (medium term) |
| `by_plugin.pvc` | `SUM(estimated_monthly_savings_usd)` on `pvc_recommendation_sets` (medium term) |
| `by_plugin.snapshot` | `SUM(estimated_monthly_cost_usd)` on `snapshot_recommendation_sets` (recoverable holding cost) |
| `by_plugin.gpu` | Always `$0` — see GPU limitation below |
| `by_cluster.has_cost_data` | `false` when **every** container, node, and PVC recommendation in that cluster has notification code **25** (`NotifNoCostData`) |

The response includes `gpu_savings_note` explaining that GPU dollar estimates
are excluded because they are computed at API read time (see below).

## Savings recalculation after cost model changes

When Koku finishes applying cost model rates to `OCPUsageLineItemDailySummary`,
the masu cost model updater notifies ROS to refresh **persisted dollar savings
only** — recommendation sizing and classification are unchanged.

### Endpoint

```
POST {ROS_API}/api/cost-management/v1/internal/recalculate-savings
```

Implementation: [`internal/api/handlers_savings_recalculate.go`](../../internal/api/handlers_savings_recalculate.go),
[`internal/engine/savings_recalculate.go`](../../internal/engine/savings_recalculate.go).

### Request body

| Field | Required | Description |
|-------|----------|-------------|
| `org_id` | Yes | Organization ID **without** the `org` schema prefix (e.g. `1234567`) |
| `cluster_uuid` | No | Scope recalculation to one cluster (Koku provider UUID) |
| `recommendation_types` | No | Subset of `container`, `node`, `pvc`, `quota`, `cluster-quota` (default: all five) |

Example:

```json
{
  "org_id": "1234567",
  "cluster_uuid": "02059694-68ab-4d58-8809-de1e91f1d0e5",
  "recommendation_types": ["container", "node", "pvc", "quota", "cluster-quota"]
}
```

### Response

`202 Accepted` with `status: "accepted"`. Work runs asynchronously in a background
goroutine ([`TriggerSavingsRecalculationAsync()`](../../internal/engine/savings_recalculate.go)).

### What recalculation does

1. Re-fetches Masu `effective_rates` for each affected cluster (same provider as ingestion).
2. Recomputes savings on existing recommendation rows for the requested types (container/node/PVC `estimated_monthly_savings_usd`; quota `estimated_savings_cents`; cluster-quota `savings_dollars_monthly`).
3. Does **not** re-run classification, percentile sizing, or CSV ingestion.

GPU and snapshot dollar fields are out of scope for this path (GPU remains API read-time;
snapshot uses its own cost chain).

### When Koku calls it

After [`OCPCostModelCostUpdater.update_summary_cost_model_costs()`](https://github.com/project-koku/koku/blob/main/koku/masu/processor/ocp/ocp_cost_model_cost_updater.py)
completes (usage costs, markup, monthly costs, distribution, and UI summary refresh),
Koku invokes [`notify_ros_savings_recalculation()`](https://github.com/project-koku/koku/blob/main/koku/masu/processor/ros_savings_recalc.py)
inline (10s HTTP timeout). The call is **non-blocking for the cost model task** in the
sense that failures are logged as warnings and do not fail the updater; the HTTP call
itself is synchronous with a short timeout.

**Cross-repo dependency:** The Koku-side trigger lives at
`koku/masu/processor/ros_savings_recalc.py` (wired from
[`ocp_cost_model_cost_updater.py`](https://github.com/project-koku/koku/blob/main/koku/masu/processor/ocp/ocp_cost_model_cost_updater.py)).
That module may exist only in a local koku working tree until it is committed and merged
in the **koku** repository separately from ros-ocp-backend releases. On-prem/SaaS
deployments need both repos aligned for savings to refresh after cost model edits.

### Configuration

| Side | Variable | Default | Description |
|------|----------|---------|-------------|
| **Koku** | `ROS_API_HOST` | (unset) | ROS API hostname (e.g. `cost-onprem-ros-api.cost-onprem.svc.cluster.local`). No notification when unset unless `ROS_OCP_BACKEND_URL` is set |
| **Koku** | `ROS_API_PORT` | `8000` | ROS API port when using `ROS_API_HOST` |
| **Koku** | `ROS_OCP_BACKEND_URL` | `http://cost-onprem-ros-api:8000` | Fallback base URL when `ROS_API_HOST` is unset (on-prem chart) |
| **Koku** | `ROS_SERVICE_TOKEN` | (unset) | Optional bearer token; otherwise Koku uses the same service-account token path as ROS tag sync |
| **ROS** | `ROS_SAVINGS_RECALCULATION_ENABLED` | `true` | Gate for this endpoint (requires `ROS_SAVINGS_ESTIMATES_ENABLED` and `KOKU_MASU_URL`) |
| **ROS** | `ROS_SAVINGS_ESTIMATES_ENABLED` | `true` | Must be enabled for recalculation to run |
| **ROS** | `KOKU_MASU_URL` | (empty) | Masu base URL for fresh `effective_rates` during recalculation |

### Authentication

Same service-account bearer validation as internal tag sync
([`tags.ValidateBearerToken`](../../internal/tags/auth.go) — see `internal/tags/`).
User JWTs from the console are **not** accepted on this route.

Koku sends `Authorization: Bearer <token>` when:

1. `ROS_SERVICE_TOKEN` is set (static token for dev or explicit Helm override), or
2. The Celery worker reads the projected Kubernetes service account token at
   `ROS_TAGS_SA_TOKEN_PATH` (default `/var/run/secrets/kubernetes.io/serviceaccount/token`),
   same path used by [`ros_tag_sync._read_bearer_token()`](https://github.com/project-koku/koku/blob/main/koku/masu/processor/ros_tag_sync.py).

On ROS, `ROS_TAGS_ALLOWED_SERVICE_ACCOUNTS` (comma-separated) restricts which SA
usernames pass TokenReview when not using `ROS_TAGS_DEV_TOKEN`. The Koku worker
ServiceAccount name must appear in that list when tag-sync-style auth is enforced.

### On-prem Helm chart (`cost-onprem-chart`)

The chart wires Koku workers and API pods with `ROS_OCP_BACKEND_URL` pointing at
`http://<release>-ros-api:<port>` via `costManagement.rosIntegration` in
[`cost-onprem/values.yaml`](https://github.com/project-koku/cost-onprem-chart/blob/main/cost-onprem/values.yaml)
and [`templates/_helpers-koku.tpl`](https://github.com/project-koku/cost-onprem-chart/blob/main/cost-onprem/templates/_helpers-koku.tpl).

**Example overrides** (`openshift-values.yaml` or `-f` values file):

```yaml
costManagement:
  rosIntegration:
    # Optional: full URL instead of chart default
    # ocpBackendUrl: "http://cost-onprem-ros-api.cost-onprem.svc:8000"
    # Optional: host/port form (takes precedence over ocpBackendUrl in Koku)
    # apiHost: "cost-onprem-ros-api.cost-onprem.svc.cluster.local"
    # apiPort: "8000"

ros:
  api:
    savingsEstimatesEnabled: true
    savingsRecalculationEnabled: true
    # When using SA token auth from Koku worker, allow its SA username:
    # tagsAllowedServiceAccounts: "system:serviceaccount:cost-onprem:cost-onprem-koku"
```

**Koku Celery:** Savings notification runs from the **cost-model** queue worker
after `update_summary_cost_model_costs`. All workers inherit `ROS_OCP_BACKEND_URL`
from `cost-onprem.koku.commonEnv`; no separate worker-only env block is required.

**Verify end-to-end:**

```bash
# After a cost model PUT, cost-model worker logs should show:
# "ROS savings recalculation triggered"
kubectl logs -n cost-onprem -l cost-onprem.io/celery-type=worker,cost-onprem.io/worker-queue=cost-model --tail=50 | grep -i savings

# ROS API should accept the callback (202):
kubectl logs -n cost-onprem -l app.kubernetes.io/component=ros-api --tail=50 | grep -i recalc
```

### Failure mode

| Scenario | Koku behavior | ROS / savings behavior |
|----------|---------------|------------------------|
| ROS not configured | Debug log, skip POST | N/A |
| HTTP error / timeout | Warning log | Savings unchanged until next ingestion or retry |
| ROS returns 404 (recalc disabled) | Warning log | Savings unchanged |
| ROS accepts 202 | Info log | Background recalc updates persisted columns |

Savings will still refresh on the next successful operator upload and ingestion cycle
even when notification fails.

## Future: GPU savings persistence (v2)

GPU savings (container detail enrichment and node GPU time-slicing) are computed
at **API read time** via [`gpu_enrichment.go`](../../internal/api/gpu_enrichment.go).
That approach always uses fresh Masu rates on each request but cannot be
aggregated cheaply for fleet-level summaries.

| Approach | Pros | Cons |
|----------|------|------|
| Read-time (current) | Always accurate rates | Not aggregatable; excluded from `savings-summary` |
| Persisted at ingestion (v2) | Fleet aggregatable; faster list APIs | Potentially stale until recalculation trigger (see v2 above) |

v2 could persist GPU savings alongside container/node/PVC columns and include
them in `by_plugin.gpu` once a cost-model change trigger exists.
