# Configurability Reference

Complete environment variable reference for ROS-OCP Backend recommendation engines,
classification thresholds, retention, and platform settings.

For algorithm behavior (decay weighting, adaptive margin, trend detection), see
[Recommendation Math](recommendation-math.md). For how thresholds affect each plugin's
output, see [Recommendation Engines](recommendation-engines.md).

---

## Configuration Precedence

ROS-OCP uses a **three-tier precedence model** for every configurable parameter:

| Tier | Source | Scope | Behavior |
|------|--------|-------|----------|
| **1 — Admin env var** | `ROS_*` environment variable | Platform-wide | **Locks** the field for all tenants. Tenant Settings API writes return `422 Unprocessable Entity` for locked fields. |
| **2 — Tenant Settings API** | Per-`org_id` database record | Single tenant | Applied when no admin env var is set. Stored in PostgreSQL (`org_recommendation_terms`, snapshot settings, business-hours schedules, etc.). |
| **3 — Compiled default** | Hardcoded in plugin or engine | Fallback | Used when neither tier 1 nor tier 2 provides a value. Defined in `DefaultTerms()`, `DefaultCPUConfig()`, plugin constants, etc. |

**Resolution order:** Tier 1 → Tier 2 → Tier 3. Admin env vars always win.

Term-specific env vars (`ROS_TERMS_<PLUGIN>_<TERM>_<FIELD>`) lock individual term
fields. Threshold env vars (`ROS_CONTAINER_*`, `ROS_GPU_*`, etc.) lock the
corresponding sizing or classification parameter platform-wide.

---

## Settings API Routes

Base path: `/api/cost-management/v1/recommendations/openshift/settings/`

| Route | Methods | Status | Purpose |
|-------|---------|--------|---------|
| `/settings/terms?recommendation_type=<plugin>` | GET, PUT, DELETE | **Existing** | Per-tenant term windows (short / medium / long). Valid plugins: `container`, `namespace`, `node`, `gpu`, `pvc`. |
| `/settings/snapshot` | GET, PUT | **Existing** | Snapshot staleness thresholds (orphan age, never-restored days, stale days, redundant count, cost per GiB/month). |
| `/settings/business-hours` | GET, PUT, DELETE | **Existing** | Org-default business-hours schedule. |
| `/settings/business-hours/clusters/:cluster_id` | GET, PUT, DELETE | **Existing** | Cluster-level schedule override. |
| `/settings/business-hours/clusters/:cluster_id/namespaces/:namespace` | GET, PUT, DELETE | **Existing** | Namespace-level schedule override. |
| `/settings/thresholds?recommendation_type=<plugin>` | GET, PUT | **Proposed** | Per-tenant sizing and classification thresholds (percentiles, margins, idle limits, GPU SM thresholds, PVC oversized ratio, etc.). |

When a parameter is locked by an admin env var, the Settings API marks it `"locked": true`
in GET responses and rejects PUT attempts for that field.

Implementation references: [`handlers_terms.go`](../../internal/api/handlers_terms.go),
[`handlers_snapshot_settings.go`](../../internal/api/handlers_snapshot_settings.go),
[`handlers_business_hours_settings.go`](../../internal/api/handlers_business_hours_settings.go),
[`term_config.go`](../../internal/engine/term_config.go).

---

## Global / Platform

Platform-wide settings. Most are **admin-only** (not tenant-configurable via API).

| Env var | Default | Type | Description | Tenant-configurable | Status |
|---------|---------|------|-------------|---------------------|--------|
| `ROS_STALENESS_THRESHOLD_HOURS` | 72 | int | Hours without cluster report before recs marked stale | Yes | Already exists |
| `ROS_MAX_LOOKBACK_DAYS` | 90 | int | Max digest lookback for queries | No | Already exists |
| `ROS_OOM_BASE_BUMP` | 0.15 | float64 | Log-scaling factor in OOM memory bump | Yes | Already exists |
| `ROS_OOM_MAX_BUMP` | 1.60 | float64 | Maximum memory bump multiplier after OOM | Yes | Already exists |
| `ROS_HISTORY_RETENTION_DAYS` | 90 | int | Recommendation history retention | No | Already exists |
| `ROS_STALE_ARCHIVE_DAYS` | 30 | int | Delete stale recs older than N days | No | Already exists |
| `ROS_RETENTION_MONTHS` | 6 | int | Partition retention for digest tables | No | Already exists |
| `ROS_ENABLED_PLUGINS` | (empty=all) | string | Plugin allowlist (CSV) | No | Already exists |
| `ROS_DISABLED_PLUGINS` | (empty) | string | Plugin blocklist (CSV) | No | Already exists |

---

## Container

Sizing, classification, and notification thresholds for per-container CPU/memory
recommendations. Cost and performance engines share these parameters with different
percentile values.

| Env var | Default | Type | Description | Tenant-configurable | Status |
|---------|---------|------|-------------|---------------------|--------|
| `ROS_CONTAINER_CPU_COST_PERCENTILE` | 0.60 | float64 | CPU percentile for cost engine (P60) | Yes | New |
| `ROS_CONTAINER_CPU_PERF_PERCENTILE` | 0.98 | float64 | CPU percentile for performance engine (P98) | Yes | New |
| `ROS_CONTAINER_MEM_COST_PERCENTILE` | 0.95 | float64 | Memory percentile for cost engine (P95) | Yes | New |
| `ROS_CONTAINER_MEM_PERF_PERCENTILE` | 1.0 | float64 | Memory percentile for performance engine (max) | Yes | New |
| `ROS_CONTAINER_MIN_MARGIN` | 1.15 | float64 | Adaptive margin floor. <br><em>Expanded: The recommendation engine adds a safety buffer above the observed usage to prevent throttling or OOM kills. This margin adapts based on how variable the workload is (high variance = larger margin). This setting is the minimum margin that will be applied even for perfectly stable workloads. A value of 1.15 means at least 15% headroom above observed usage.</em> | Yes | New |
| `ROS_CONTAINER_MAX_MARGIN` | 1.50 | float64 | Adaptive margin ceiling. <br><em>Expanded: The maximum safety buffer applied to highly variable workloads. A value of 1.50 means at most 50% headroom. Workloads with erratic usage patterns get closer to this cap.</em> | Yes | New |
| `ROS_CONTAINER_LIMIT_MULTIPLIER` | 1.05 | float64 | Limit = request × multiplier | Yes | New |
| `ROS_CONTAINER_CPU_FLOOR_MC` | 25 | int64 | Minimum CPU request (millicores). <br><em>Expanded: No recommendation will ever suggest less than this value. This prevents impractically small CPU requests that could cause scheduling issues or extreme throttling. 25 millicores (0.025 cores) is the practical minimum for most containers on OpenShift.</em> | Yes | New |
| `ROS_CONTAINER_IDLE_CPU_THRESHOLD_MC` | 10 | int64 | Max CPU for idle classification | Yes | New |
| `ROS_CONTAINER_IDLE_MEM_THRESHOLD_KIB` | 10240 | int64 | Max memory for idle classification (10 MiB) | Yes | New |
| `ROS_CONTAINER_MEM_TREND_SLOPE_THRESHOLD` | 100.0 | float64 | Memory trend slope (KiB/day) for notification. <br><em>Expanded: If the container's memory consumption is growing faster than this rate (measured by linear regression over recent days), a notification is emitted warning about potential memory leaks or growing datasets. 100 KiB/day ≈ 3 MiB/month.</em> | Yes | New |
| `ROS_CONTAINER_LOW_CONFIDENCE_THRESHOLD` | 0.5 | float32 | Confidence below which low-confidence notification fires. <br><em>Expanded: Confidence is calculated as `min(days_of_data / window_days, 1.0)` — i.e., how much of the requested observation window actually has data. A threshold of 0.5 means: if less than 50% of the window has data (e.g., 3 days of data in a 7-day window), the recommendation is flagged as low-confidence.</em> | Yes | New |
| `ROS_TERMS_CONTAINER_SHORT_WINDOW_DAYS` | 1 | int | Short-term window | Yes* | Already exists |
| `ROS_TERMS_CONTAINER_SHORT_MIN_DATA_DAYS` | 1 | int | Short min data days | Yes* | Already exists |
| `ROS_TERMS_CONTAINER_SHORT_DECAY_HALFLIFE_HOURS` | 0 | float64 | Short decay (0=none) | Yes* | Already exists |
| `ROS_TERMS_CONTAINER_MEDIUM_WINDOW_DAYS` | 7 | int | Medium window | Yes* | Already exists |
| `ROS_TERMS_CONTAINER_MEDIUM_MIN_DATA_DAYS` | 3 | int | Medium min data | Yes* | Already exists |
| `ROS_TERMS_CONTAINER_MEDIUM_DECAY_HALFLIFE_HOURS` | 168 | float64 | Medium decay (7d) | Yes* | Already exists |
| `ROS_TERMS_CONTAINER_LONG_WINDOW_DAYS` | 15 | int | Long window | Yes* | Already exists |
| `ROS_TERMS_CONTAINER_LONG_MIN_DATA_DAYS` | 7 | int | Long min data | Yes* | Already exists |
| `ROS_TERMS_CONTAINER_LONG_DECAY_HALFLIFE_HOURS` | 360 | float64 | Long decay (15d) | Yes* | Already exists |

\* Configurable via `PUT /settings/terms?recommendation_type=container`. Admin `ROS_TERMS_*` env vars lock individual term fields.

---

## Namespace

Same sizing parameters as container. Namespace recommendations aggregate
container-level digests; thresholds apply to the aggregated series.

| Env var | Default | Type | Description | Tenant-configurable | Status |
|---------|---------|------|-------------|---------------------|--------|
| `ROS_NAMESPACE_CPU_COST_PERCENTILE` | 0.60 | float64 | CPU cost percentile | Yes | New |
| `ROS_NAMESPACE_CPU_PERF_PERCENTILE` | 0.98 | float64 | CPU perf percentile | Yes | New |
| `ROS_NAMESPACE_MEM_COST_PERCENTILE` | 0.95 | float64 | Memory cost percentile | Yes | New |
| `ROS_NAMESPACE_MEM_PERF_PERCENTILE` | 1.0 | float64 | Memory perf percentile | Yes | New |
| `ROS_NAMESPACE_MIN_MARGIN` | 1.15 | float64 | Adaptive margin floor. <br><em>Expanded: Adaptive margin floor for namespace-level recommendations. Minimum headroom percentage applied even for stable namespace-wide usage. 1.15 = at least 15% above observed.</em> | Yes | New |
| `ROS_NAMESPACE_MAX_MARGIN` | 1.50 | float64 | Adaptive margin ceiling. <br><em>Expanded: Adaptive margin ceiling for namespace-level recommendations. Maximum headroom for highly variable namespace usage. 1.50 = at most 50% above observed.</em> | Yes | New |
| `ROS_NAMESPACE_LIMIT_MULTIPLIER` | 1.05 | float64 | Limit multiplier | Yes | New |
| `ROS_NAMESPACE_CPU_FLOOR_MC` | 25 | int64 | CPU floor (m). <br><em>Expanded: Minimum CPU request in namespace recommendations (millicores). Prevents recommending impractically tiny CPU requests for the entire namespace aggregate.</em> | Yes | New |
| `ROS_NAMESPACE_IDLE_CPU_THRESHOLD_MC` | 10 | int64 | Idle CPU (m). <br><em>Expanded: Maximum CPU usage (millicores) for a namespace to be classified as idle. If no container in the namespace ever exceeds this usage over the observation window, the entire namespace is considered idle. Idle namespaces get a special notification suggesting they may be candidates for decommissioning.</em> | Yes | New |
| `ROS_NAMESPACE_IDLE_MEM_THRESHOLD_KIB` | 10240 | int64 | Idle memory (KiB). <br><em>Expanded: Maximum memory usage (KiB) for idle namespace classification. 10240 KiB = 10 MiB. If peak memory across all containers never exceeds this, the namespace is idle.</em> | Yes | New |
| `ROS_NAMESPACE_MEM_TREND_SLOPE_THRESHOLD` | 100.0 | float64 | Trend slope (KiB/day). <br><em>Expanded: Memory growth rate (KiB/day) above which a 'trending up' notification fires for the namespace. Helps detect runaway growth across the namespace before it becomes critical.</em> | Yes | New |
| `ROS_NAMESPACE_LOW_CONFIDENCE_THRESHOLD` | 0.5 | float32 | Low confidence threshold. <br><em>Expanded: Confidence threshold for namespace recommendations. Same calculation as container: days_of_data / window_days. Below this → low-confidence notification.</em> | Yes | New |
| `ROS_TERMS_NAMESPACE_SHORT_WINDOW_DAYS` | 1 | int | Short-term window | Yes* | Already exists |
| `ROS_TERMS_NAMESPACE_SHORT_MIN_DATA_DAYS` | 1 | int | Short min data days | Yes* | Already exists |
| `ROS_TERMS_NAMESPACE_SHORT_DECAY_HALFLIFE_HOURS` | 0 | float64 | Short decay (0=none) | Yes* | Already exists |
| `ROS_TERMS_NAMESPACE_MEDIUM_WINDOW_DAYS` | 7 | int | Medium window | Yes* | Already exists |
| `ROS_TERMS_NAMESPACE_MEDIUM_MIN_DATA_DAYS` | 3 | int | Medium min data | Yes* | Already exists |
| `ROS_TERMS_NAMESPACE_MEDIUM_DECAY_HALFLIFE_HOURS` | 168 | float64 | Medium decay (7d) | Yes* | Already exists |
| `ROS_TERMS_NAMESPACE_LONG_WINDOW_DAYS` | 15 | int | Long window | Yes* | Already exists |
| `ROS_TERMS_NAMESPACE_LONG_MIN_DATA_DAYS` | 7 | int | Long min data | Yes* | Already exists |
| `ROS_TERMS_NAMESPACE_LONG_DECAY_HALFLIFE_HOURS` | 360 | float64 | Long decay (15d) | Yes* | Already exists |

\* Configurable via `PUT /settings/terms?recommendation_type=namespace`.

---

## Node

Node-level CPU/memory utilization, classification, and dual-engine sizing
(cost target 80%, performance target 55%).

| Env var | Default | Type | Description | Tenant-configurable | Status |
|---------|---------|------|-------------|---------------------|--------|
| `ROS_NODE_UNDERUTIL_THRESHOLD` | 0.30 | float64 | P95 util below → underutilized. <br><em>Expanded: Node underutilization threshold (fraction of allocatable capacity). A node is classified as 'underutilized' when BOTH its CPU P95 AND memory P95 usage are below this fraction of allocatable capacity. 0.30 means: if a node never uses more than 30% of its CPU and 30% of its memory, it's underutilized and a candidate for consolidation.</em> | Yes | Already exists |
| `ROS_NODE_OVERCOMMIT_THRESHOLD` | 1.50 | float64 | Requests/allocatable above → overcommitted. <br><em>Expanded: Node overcommit threshold (ratio of total pod requests to allocatable). A node is 'overcommitted' when the sum of all pod CPU requests exceeds this multiple of the node's allocatable CPU. 1.50 means: if pods request 150% of what the node can actually provide, the node is dangerously overcommitted and likely to experience evictions.</em> | Yes | Already exists |
| `ROS_NODE_ALLOCATABLE_FACTOR` | 0.93 | float64 | Fallback when allocatable unknown. <br><em>Expanded: Fallback multiplier when node allocatable capacity is unknown. Some nodes don't report allocatable resources. In that case, allocatable is estimated as `max_observed_requests × this_factor`. 0.93 accounts for system-reserved resources (~7% for kubelet, OS, etc.).</em> | Yes | Already exists |
| `ROS_NODE_STRANDED_IMBALANCE_THRESHOLD` | 0.60 | float64 | CPU/mem imbalance → stranded. <br><em>Expanded: CPU/memory imbalance ratio above which a node is classified as having 'stranded resources'. Stranded means one resource (e.g., CPU) is heavily used while the other (memory) has large amounts wasted. Calculated as `|cpu_util − mem_util| / max(cpu_util, mem_util)`. 0.60 means: if one resource is 60%+ more utilized than the other, the imbalance is flagged.</em> | Yes | Already exists |
| `ROS_NODE_EMA_ALPHA` | 0.30 | float64 | EMA smoothing factor. <br><em>Expanded: Exponential Moving Average (EMA) smoothing factor for node trend and imbalance calculations. Higher values (closer to 1.0) react faster to recent changes but are noisier. Lower values (closer to 0.0) are smoother but lag behind. 0.30 gives moderate smoothing, weighting recent data ~30% vs ~70% history.</em> | Yes | Already exists |
| `ROS_NODE_COST_TARGET_UTILIZATION` | 0.80 | float64 | Cost engine target (80%) | Yes | New |
| `ROS_NODE_PERF_TARGET_UTILIZATION` | 0.55 | float64 | Performance engine target (55%) | Yes | New |
| `ROS_NODE_PERF_CONSOLIDATION_HEADROOM_MULTIPLIER` | 2.0 | float64 | Perf consolidates only when current ≥ N× recommended. <br><em>Expanded: Performance engine consolidation guard. The performance engine will only recommend consolidating nodes (reducing node count) when the current capacity is at least this multiple of the recommended capacity on BOTH CPU and memory. 2.0 means: the cluster must have at least twice the recommended resources before consolidation is suggested. This prevents the performance engine from being too aggressive with capacity reduction.</em> | Yes | New |
| `ROS_NODE_TREND_MIN_DAYS` | 3 | int | Min days for CPU trend slope | Yes | New |
| `ROS_TERMS_NODE_SHORT_WINDOW_DAYS` | 1 | int | Short-term window | Yes* | Already exists |
| `ROS_TERMS_NODE_SHORT_MIN_DATA_DAYS` | 1 | int | Short min data days | Yes* | Already exists |
| `ROS_TERMS_NODE_SHORT_DECAY_HALFLIFE_HOURS` | 0 | float64 | Short decay (0=none) | Yes* | Already exists |
| `ROS_TERMS_NODE_MEDIUM_WINDOW_DAYS` | 7 | int | Medium window | Yes* | Already exists |
| `ROS_TERMS_NODE_MEDIUM_MIN_DATA_DAYS` | 3 | int | Medium min data | Yes* | Already exists |
| `ROS_TERMS_NODE_MEDIUM_DECAY_HALFLIFE_HOURS` | 168 | float64 | Medium decay (7d) | Yes* | Already exists |
| `ROS_TERMS_NODE_LONG_WINDOW_DAYS` | 15 | int | Long window | Yes* | Already exists |
| `ROS_TERMS_NODE_LONG_MIN_DATA_DAYS` | 7 | int | Long min data | Yes* | Already exists |
| `ROS_TERMS_NODE_LONG_DECAY_HALFLIFE_HOURS` | 360 | float64 | Long decay (15d) | Yes* | Already exists |

\* Configurable via `PUT /settings/terms?recommendation_type=node`.

---

## GPU

!!! warning "Expert configuration only"
    GPU thresholds interact with NVIDIA DCGM profiling semantics and MIG hardware sizing.
    Change only with GPU workload expertise. Incorrect values produce misleading
    recommendations.

Classification, MIG sizing, confidence scoring, and time-slicing parameters.

| Env var | Default | Type | Description | Tenant-configurable | Status |
|---------|---------|------|-------------|---------------------|--------|
| `ROS_GPU_IDLE_THRESHOLD` | 0.02 | float64 | Avg SM below → idle | Yes | Already exists ⚠️ |
| `ROS_GPU_UNDERUTILIZED_SM_THRESHOLD` | 0.25 | float64 | SM below → underutilized | Yes | Already exists ⚠️ |
| `ROS_GPU_UNDERUTILIZED_TENSOR_THRESHOLD` | 0.15 | float64 | Tensor below → underutilized | Yes | Already exists ⚠️ |
| `ROS_GPU_MEMBOUND_DRAM_THRESHOLD` | 0.60 | float64 | DRAM above → memory-bound | Yes | Already exists ⚠️ |
| `ROS_GPU_MEMBOUND_TENSOR_THRESHOLD` | 0.15 | float64 | Tensor below + high DRAM → memory-bound | Yes | Already exists ⚠️ |
| `ROS_GPU_FB_HEADROOM_FACTOR` | 1.20 | float64 | MIG sizing: P98 × factor | Yes | Already exists ⚠️ |
| `ROS_GPU_COMPUTE_BOUND_DRAM_THRESHOLD` | 0.30 | float64 | DRAM below → compute_bound_underutil | Yes | New ⚠️ |
| `ROS_GPU_MIG_FB_PERCENTILE` | 0.98 | float64 | Percentile for MIG FB selection | Yes | New ⚠️ |
| `ROS_GPU_CONFIDENCE_DAYS_TIER1` | 3 | int | Days below → confidence 0.3. <br><em>Expanded: First confidence tier boundary (days of GPU data). With fewer than this many days of profiling data, the recommendation confidence starts at 0.3 (very low). This protects against making bold GPU recommendations from insufficient data.</em> | Yes | New ⚠️ |
| `ROS_GPU_CONFIDENCE_DAYS_TIER2` | 7 | int | Days below → confidence 0.6. <br><em>Expanded: Second tier boundary. Below this → confidence base 0.6. Between tier1 and tier2, the GPU classification is considered moderately confident.</em> | Yes | New ⚠️ |
| `ROS_GPU_CONFIDENCE_DAYS_TIER3` | 14 | int | Days below → confidence 0.8. <br><em>Expanded: Third tier boundary. Below this → confidence base 0.8. Above → confidence 1.0 (full confidence). The tiered approach ensures that longer observation periods produce more trustworthy GPU recommendations.</em> | Yes | New ⚠️ |
| `ROS_GPU_SPIKE_RATIO_THRESHOLD` | 5.0 | float64 | max SM / avg SM → bursty. <br><em>Expanded: Burst detection ratio for GPU workloads. Calculated as `max_SM_utilization / avg_SM_utilization`. When this ratio exceeds the threshold, the workload is considered 'bursty' (short intense GPU use interspersed with idle periods). Bursty workloads get a confidence penalty because peak vs average divergence makes sizing uncertain. 5.0 means: if peak GPU use is 5× the average, it's bursty.</em> | Yes | New ⚠️ |
| `ROS_GPU_SPIKE_CONFIDENCE_PENALTY` | 0.70 | float64 | Penalty on spike | Yes | New ⚠️ |
| `ROS_GPU_NO_PROFILING_CONFIDENCE_FACTOR` | 0.50 | float64 | Confidence when no profiling. <br><em>Expanded: Confidence multiplier when NVIDIA DCGM profiling metrics are absent. Some GPUs or drivers don't report detailed SM/tensor/DRAM metrics. In that case, classification relies only on basic utilization data and confidence is scaled by this factor. 0.50 means halved confidence without profiling.</em> | Yes | New ⚠️ |
| `ROS_GPU_TIMESLICING_MAJORITY_THRESHOLD` | 0.50 | float64 | Min fraction of eligible containers. <br><em>Expanded: Minimum fraction of GPU-using containers on a node that must be time-slicing candidates for a time-slicing recommendation to be emitted. Prevents recommending time-slicing when only a small minority of GPU workloads would benefit. 0.50 means at least half the GPU containers must be underutilizing their full GPU.</em> | Yes | New ⚠️ |
| `ROS_GPU_TIMESLICING_MIN_REPLICAS` | 2 | int | Min replicas | Yes | New ⚠️ |
| `ROS_GPU_TIMESLICING_MAX_REPLICAS` | 8 | int | Max replicas | Yes | New ⚠️ |
| `ROS_GPU_TIMESLICING_BASE_PENALTY` | 0.70 | float64 | Base confidence penalty | Yes | New ⚠️ |
| `ROS_GPU_TIMESLICING_IMPACTED_WEIGHT` | 0.30 | float64 | Impacted-container weight. <br><em>Expanded: Weight of the 'impacted container ratio' in time-slicing confidence calculation. Time-slicing confidence = `base_penalty + impacted_weight × (candidates/total_gpu_containers)`. Higher weight means confidence increases more when a larger proportion of containers would benefit from sharing.</em> | Yes | New ⚠️ |
| `ROS_GPU_NODE_FRESHNESS_DAYS` | 7 | int | Max age of node GPU telemetry. <br><em>Expanded: Maximum age (days) of node-level GPU telemetry data for time-slicing analysis. Nodes whose last GPU report is older than this are excluded from time-slicing recommendations. Prevents stale data from producing outdated sharing suggestions.</em> | Yes | New ⚠️ |
| `ROS_TERMS_GPU_SHORT_WINDOW_DAYS` | 1 | int | Short-term window | Yes* | Already exists |
| `ROS_TERMS_GPU_SHORT_MIN_DATA_DAYS` | 1 | int | Short min data days | Yes* | Already exists |
| `ROS_TERMS_GPU_SHORT_DECAY_HALFLIFE_HOURS` | 0 | float64 | Short decay (0=none) | Yes* | Already exists |
| `ROS_TERMS_GPU_MEDIUM_WINDOW_DAYS` | 7 | int | Medium window | Yes* | Already exists |
| `ROS_TERMS_GPU_MEDIUM_MIN_DATA_DAYS` | 3 | int | Medium min data | Yes* | Already exists |
| `ROS_TERMS_GPU_MEDIUM_DECAY_HALFLIFE_HOURS` | 168 | float64 | Medium decay (7d) | Yes* | Already exists |
| `ROS_TERMS_GPU_LONG_WINDOW_DAYS` | 15 | int | Long window | Yes* | Already exists |
| `ROS_TERMS_GPU_LONG_MIN_DATA_DAYS` | 7 | int | Long min data | Yes* | Already exists |
| `ROS_TERMS_GPU_LONG_DECAY_HALFLIFE_HOURS` | 360 | float64 | Long decay (15d) | Yes* | Already exists |

\* Configurable via `PUT /settings/terms?recommendation_type=gpu`.

See also [GPU Classification](gpu-classification.md) for the decision tree and
[GPU Time-Slicing](../features/gpu-time-slicing.md) for replica selection logic.

---

## PVC

Storage right-sizing thresholds. PVC uses longer default term windows (7d / 30d / 90d)
because storage growth is slow.

| Env var | Default | Type | Description | Tenant-configurable | Status |
|---------|---------|------|-------------|---------------------|--------|
| `ROS_PVC_OVERSIZED_THRESHOLD` | 0.20 | float64 | Usage/capacity below → oversized. <br><em>Expanded: PVC oversized classification threshold (fraction). A PVC is 'oversized' when actual peak usage divided by provisioned capacity is below this value. 0.20 means: if you provisioned 100 GiB but never use more than 20 GiB, the PVC is flagged as oversized and a downsizing recommendation is produced.</em> | Yes | New |
| `ROS_PVC_NEAR_FULL_THRESHOLD` | 0.85 | float64 | Usage/capacity above → near-full. <br><em>Expanded: PVC near-full classification threshold (fraction). A PVC is 'near-full' when usage/capacity exceeds this value. 0.85 means: using more than 85% of provisioned storage triggers an expansion warning.</em> | Yes | New |
| `ROS_PVC_MIN_TREND_DAYS` | 7 | int | Min days for growth slope. <br><em>Expanded: Minimum days of usage data required before computing a storage growth trend (linear regression slope). Prevents noisy slope estimates from too-short time series. 7 means: at least a week of PVC usage data before projecting future growth.</em> | Yes | New |
| `ROS_PVC_RECOMMENDED_SIZE_MULTIPLIER` | 2 | int | Recommended = max usage × N. <br><em>Expanded: Multiplier for recommended PVC size. When a PVC is oversized, the recommendation is `max_observed_usage × multiplier`. 2 means: recommend provisioning 2× the peak usage, giving 50% headroom for growth.</em> | Yes | New |
| `ROS_PVC_MIN_RECOMMENDED_GIB` | 1 | int | Floor (1 GiB). <br><em>Expanded: Minimum recommended PVC size (GiB). No downsizing recommendation will ever suggest less than this. Prevents recommending impractically small volumes. 1 GiB is the minimum.</em> | Yes | New |
| `ROS_PVC_DAYS_TO_FULL_ALERT` | 30 | int | Days-to-full below → alert. <br><em>Expanded: Days-to-full alert window. If the current growth trend projects the PVC filling up within fewer than this many days, a near-full alert is triggered even if current usage hasn't crossed the near-full threshold yet. 30 means: a warning fires if the PVC will fill up within a month at current growth rate.</em> | Yes | New |
| `ROS_TERMS_PVC_SHORT_WINDOW_DAYS` | 7 | int | Short-term window | Yes* | Already exists |
| `ROS_TERMS_PVC_SHORT_MIN_DATA_DAYS` | 3 | int | Short min data days | Yes* | Already exists |
| `ROS_TERMS_PVC_SHORT_DECAY_HALFLIFE_HOURS` | 0 | float64 | Short decay (0=none) | Yes* | Already exists |
| `ROS_TERMS_PVC_MEDIUM_WINDOW_DAYS` | 30 | int | Medium window | Yes* | Already exists |
| `ROS_TERMS_PVC_MEDIUM_MIN_DATA_DAYS` | 14 | int | Medium min data | Yes* | Already exists |
| `ROS_TERMS_PVC_MEDIUM_DECAY_HALFLIFE_HOURS` | 0 | float64 | Medium decay (0=none) | Yes* | Already exists |
| `ROS_TERMS_PVC_LONG_WINDOW_DAYS` | 90 | int | Long window | Yes* | Already exists |
| `ROS_TERMS_PVC_LONG_MIN_DATA_DAYS` | 30 | int | Long min data | Yes* | Already exists |
| `ROS_TERMS_PVC_LONG_DECAY_HALFLIFE_HOURS` | 0 | float64 | Long decay (0=none) | Yes* | Already exists |

\* Configurable via `PUT /settings/terms?recommendation_type=pvc`.

---

## Snapshot

VolumeSnapshot staleness classification. Snapshot thresholds are tenant-configurable
via `GET/PUT /settings/snapshot` (tier 2) or admin env vars (tier 1).

| Env var | Default | Type | Description | Tenant-configurable | Status |
|---------|---------|------|-------------|---------------------|--------|
| `ROS_SNAPSHOT_ORPHAN_AGE_DAYS` | 7 | int | Source PVC gone + age > N → orphaned | Yes* | Already exists |
| `ROS_SNAPSHOT_NEVER_RESTORED_DAYS` | 30 | int | Age > N, 0 restores → never restored. <br><em>Expanded: Days since creation without any restore event before flagging as 'never restored'. Snapshot restores are tracked by the koku-metrics-operator which monitors VolumeSnapshot and VolumeSnapshotContent resources on the cluster. If a snapshot exists for longer than this and has never been used to create a new PVC (restore count = 0), it may be unnecessary.</em> | Yes* | Already exists |
| `ROS_SNAPSHOT_STALE_DAYS` | 90 | int | Age > N → stale | Yes* | Already exists |
| `ROS_SNAPSHOT_REDUNDANT_THRESHOLD` | 3 | int | > N per PVC → redundant | Yes* | Already exists |
| `ROS_SNAPSHOT_COST_PER_GIB_MONTH` | 0.05 | float64 | Fallback $/GiB/month. <br><em>Expanded: Fallback storage cost rate (USD per GiB per month) used when no cost model rate is available from Koku. This is used to estimate the monthly cost of keeping a snapshot. The resolution chain is: Koku effective-rates `storage_gb_usage_per_month` (dynamic) → tenant DB override → this env var → $0.05 default. Set this to match your actual block storage provider's snapshot pricing.</em> | Yes* | Already exists |
| `ROS_SNAPSHOT_INVENTORY_FRESH_HOURS` | 6 | int | Recent-ingest window | No | New |

\* Configurable via `PUT /settings/snapshot`.

---

## Business Hours

Platform feature gate and reship behavior. Not tenant-configurable via thresholds API;
schedules are managed through the business-hours Settings API routes.

| Env var | Default | Type | Description | Tenant-configurable | Status |
|---------|---------|------|-------------|---------------------|--------|
| `ROS_BUSINESS_HOURS_ENABLED` | true | bool | Feature gate | No | Already exists |
| `ROS_BUSINESS_HOURS_RESHIP_FORWARD_ONLY_FALLBACK` | false | bool | Forward-only fallback | No | Already exists |

Admin guide: [Business Hours](../features/business-hours.md).

---

## Reship

Internal reship poller for business-hours historical data backfill. Admin-only.

| Env var | Default | Type | Description | Tenant-configurable | Status |
|---------|---------|------|-------------|---------------------|--------|
| `ROS_RESHIP_POLLER_INTERVAL_SECS` | 60 | int | Retry interval | No | Already exists |
| `ROS_RESHIP_MAX_RETRIES` | 10 | int | Max failures | No | Already exists |

---

## Savings / Cost

Dollar estimate integration with Koku Masu `effective_rates`. See
[Cost Integration](cost-integration.md) for formulas and plugin matrix.

| Env var | Default | Type | Description | Tenant-configurable | Status |
|---------|---------|------|-------------|---------------------|--------|
| `ROS_SAVINGS_ESTIMATES_ENABLED` | true | bool | Kill-switch | No | Already exists |
| `KOKU_MASU_URL` | (empty) | string | Koku masu base URL | No | Already exists |

---

## Recommended Values by Use Case

These are starting points for tuning. Validate against your workload profiles before
applying in production.

### Aggressive cost optimization

Maximize rightsizing and deallocation recommendations. Accept higher risk of
occasional CPU throttling or OOM under burst load.

| Parameter | Suggested value | Rationale |
|-----------|-----------------|-----------|
| `ROS_CONTAINER_CPU_COST_PERCENTILE` | 0.50 | Lower percentile → smaller CPU requests |
| `ROS_CONTAINER_MEM_COST_PERCENTILE` | 0.90 | Slightly below default P95 |
| `ROS_CONTAINER_IDLE_CPU_THRESHOLD_MC` | 15 | Classify more containers as idle |
| `ROS_CONTAINER_IDLE_MEM_THRESHOLD_KIB` | 20480 | 20 MiB idle memory ceiling |
| `ROS_NODE_COST_TARGET_UTILIZATION` | 0.85 | Target higher node utilization |
| `ROS_NODE_UNDERUTIL_THRESHOLD` | 0.25 | Flag underutilized nodes sooner |

### Conservative / stability-first

Prioritize headroom and performance engine recommendations. Fewer aggressive
downsizes; higher percentiles and tighter margins.

| Parameter | Suggested value | Rationale |
|-----------|-----------------|-----------|
| `ROS_CONTAINER_CPU_PERF_PERCENTILE` | 0.99 | Near-peak CPU coverage |
| `ROS_CONTAINER_MEM_PERF_PERCENTILE` | 1.0 | Max observed memory (default) |
| `ROS_CONTAINER_MIN_MARGIN` | 1.25 | Wider safety margin floor |
| `ROS_CONTAINER_MAX_MARGIN` | 1.60 | Allow larger adaptive margins |
| `ROS_NODE_PERF_TARGET_UTILIZATION` | 0.50 | More headroom on performance engine |
| `ROS_NODE_PERF_CONSOLIDATION_HEADROOM_MULTIPLIER` | 2.5 | Require more waste before perf consolidation |

### GPU training workloads

Training jobs have long warmup phases and bursty SM utilization. Default idle
thresholds may misclassify warming-up GPUs as idle.

| Parameter | Suggested value | Rationale |
|-----------|-----------------|-----------|
| `ROS_GPU_IDLE_THRESHOLD` | 0.05 | Tolerate low SM during warmup |
| `ROS_GPU_SPIKE_RATIO_THRESHOLD` | 8.0 | Reduce false bursty classification |
| `ROS_GPU_CONFIDENCE_DAYS_TIER3` | 21 | Require more data before high confidence |
| `ROS_TERMS_GPU_MEDIUM_WINDOW_DAYS` | 14 | Longer window captures full training cycles |

### Batch / HPC storage

Pre-provisioned PVC capacity is normal; default oversized threshold (20%) flags
too many volumes as oversized.

| Parameter | Suggested value | Rationale |
|-----------|-----------------|-----------|
| `ROS_PVC_OVERSIZED_THRESHOLD` | 0.40 | Allow 40% utilization before oversized |
| `ROS_PVC_RECOMMENDED_SIZE_MULTIPLIER` | 3 | Larger headroom for burst writes |
| `ROS_TERMS_PVC_LONG_WINDOW_DAYS` | 120 | Slow growth needs longer observation |

---

## Related Documentation

| Document | Scope |
|----------|-------|
| [Recommendation Engines](recommendation-engines.md) | Plugin-by-plugin threshold behavior and formulas |
| [Recommendation Math](recommendation-math.md) | Adaptive margin, decay weighting, trend detection |
| [Plugin Architecture](plugin-architecture.md) | Term resolution, plugin traits, enable/disable |
| [GPU Classification](gpu-classification.md) | GPU decision tree and MIG profile selection |
| [Cost Integration](cost-integration.md) | Savings formulas and fleet summary |
| [Upgrade Runbook](../operations/upgrade-runbook.md) | Migration procedures and deploy notes |

## Source File Index

| Area | Primary files |
|------|---------------|
| Config loading | [`config.go`](../../internal/config/config.go) |
| Term resolution | [`term_config.go`](../../internal/engine/term_config.go) |
| Container sizing | [`types.go`](../../internal/engine/types.go), [`recommend_all.go`](../../internal/engine/recommend_all.go) |
| Node sizing | [`recommend_nodes.go`](../../internal/engine/recommend_nodes.go) |
| GPU classification | [`gpu_recommender.go`](../../internal/engine/gpu_recommender.go), [`gpu_timeslicing.go`](../../internal/engine/gpu_timeslicing.go) |
| PVC sizing | [`pvc_recommend.go`](../../internal/engine/pvc_recommend.go) |
| Snapshot classification | [`snapshot_classify.go`](../../internal/engine/snapshot_classify.go), [`snapshot_settings.go`](../../internal/engine/snapshot_settings.go) |
