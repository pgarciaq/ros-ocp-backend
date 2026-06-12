# Recommendation Engine Reference

Complete reference for recommendation thresholds, percentiles, term windows, and
configuration parameters across all native-engine plugins.

For the full environment variable catalog, Settings API routes, precedence model,
and tuning guidance, see [Configurability Reference](configurability.md).

For algorithm details (adaptive margin formula, trend detection), see
[Recommendation Math](recommendation-math.md). For dollar estimates and savings
formulas, see [Cost Integration](cost-integration.md).

For decay weighting — formula, visual charts, edge-weight tables, and configuration —
see the dedicated [Decay Weights](decay-weights.md) page. Decay weights use
precomputed lookup tables keyed by integer half-life hours (lazy-built on first use)
instead of per-row `math.Exp`. When a tenant customizes `window_days` without
setting `decay_halflife_hours`, half-life auto-derives as `window_days × 12`.

---

## Summary Matrix

| Plugin | Cost / performance engines | Terms (short / medium / long) | Savings estimates | Primary source |
|--------|---------------------------|-------------------------------|-------------------|----------------|
| **container** | Yes (`cost`, `performance`) | 1d / 7d / 15d | Yes (ingestion) | [`recommend_all.go`](../../internal/engine/recommend_all.go), [`types.go`](../../internal/engine/types.go) |
| **namespace** | Yes (same percentiles as container) | 1d / 7d / 15d | No | [`recommend_namespace.go`](../../internal/engine/recommend_namespace.go) |
| **node** | Yes (`cost` 80%, `performance` 55%) | 1d / 7d / 15d | Yes (ingestion) | [`recommend_nodes.go`](../../internal/engine/recommend_nodes.go) |
| **gpu** | No (single classification per term) | 1d / 7d / 15d | Yes (API read) | [`gpu_recommender.go`](../../internal/engine/gpu_recommender.go) |
| **pvc** | No | 7d / 30d / 90d | Yes (ingestion) | [`pvc_recommend.go`](../../internal/engine/pvc_recommend.go) |
| **quota** | No (threshold-based) | None | Yes (ingestion, `tighten` only) | [`recommend_quota.go`](../../internal/engine/recommend_quota.go) |
| **cluster-quota** | No (threshold-based) | None | Yes (ingestion, `tighten` only) | [`recommend_cluster_quota.go`](../../internal/engine/recommend_cluster_quota.go) |
| **snapshot** | No | None | Yes (recoverable cost) | [`snapshot_classify.go`](../../internal/engine/snapshot_classify.go) |
| **vm** | Yes (`cost`, `performance`) | 7d / 15d / 30d (min 3 / 7 / 15 days) | Yes (ingestion); API field `savings` | [`vm_recommender.go`](../../internal/engine/vm_recommender.go) |

**Business hours** (container + namespace): optional second digest stream using the
same cost/performance percentiles as container. See [Business Hours](../features-business-hours.md).

---

## Global Configuration

| Parameter | Default | Env variable | Source |
|-----------|---------|--------------|--------|
| Staleness threshold | 48 hours | `ROS_STALENESS_THRESHOLD_HOURS` | [`recommend_all.go`](../../internal/engine/recommend_all.go) |
| Max lookback (container, namespace, node, GPU) | 90 days | `ROS_MAX_LOOKBACK_DAYS` | [`config.go`](../../internal/config/config.go), plugin `MaxWindowDays()` |
| Max lookback (PVC) | 365 days | (plugin cap via term `WINDOW_DAYS`) | [`internal/plugins/pvc/plugin.go`](../../internal/plugins/pvc/plugin.go) |
| Max lookback (VM) | 90 days | `ROS_VM_REC_HISTORY_RETENTION_DAYS` (history table) | [`internal/plugins/vm/plugin.go`](../../internal/plugins/vm/plugin.go) |
| Quota headroom | 10% | `ROS_QUOTA_HEADROOM_PERCENT` | [`quota_settings.go`](../../internal/engine/quota_settings.go) |
| Quota high-risk utilization | 90% | `ROS_QUOTA_HIGH_RISK_THRESHOLD_PERCENT` | [`quota_settings.go`](../../internal/engine/quota_settings.go) |
| Quota medium-risk utilization | 70% | `ROS_QUOTA_MEDIUM_RISK_THRESHOLD_PERCENT` | [`quota_settings.go`](../../internal/engine/quota_settings.go) |
| Cluster-quota headroom | 10% | `ROS_CLUSTER_QUOTA_HEADROOM_PERCENT` | [`cluster_quota_settings.go`](../../internal/engine/cluster_quota_settings.go) |
| Cluster-quota high-risk utilization | 90% | `ROS_CLUSTER_QUOTA_HIGH_RISK_THRESHOLD_PERCENT` | [`cluster_quota_settings.go`](../../internal/engine/cluster_quota_settings.go) |
| Cluster-quota medium-risk utilization | 70% | `ROS_CLUSTER_QUOTA_MEDIUM_RISK_THRESHOLD_PERCENT` | [`cluster_quota_settings.go`](../../internal/engine/cluster_quota_settings.go) |
| OOM base bump factor | 0.15 | `ROS_OOM_BASE_BUMP` | [`types.go`](../../internal/engine/types.go) |
| OOM max bump multiplier | 1.60 | `ROS_OOM_MAX_BUMP` | [`types.go`](../../internal/engine/types.go) |
| Savings kill-switch | enabled | `ROS_SAVINGS_ESTIMATES_ENABLED` | [`config.go`](../../internal/config/config.go) |

---

## Container & Namespace Recommendations

Container and namespace plugins share the same sizing parameters. Namespace
aggregates container-level digests; each engine row uses the profile-specific
fields from `RecommendCPU` / `RecommendMemory`.

### Engine percentiles and sizing

| Parameter | Cost engine | Performance engine | Source |
|-----------|-------------|-------------------|--------|
| CPU usage percentile | P60 | P98 | [`DefaultCPUConfig()`](../../internal/engine/types.go), [`cpuConfigForProfile()`](../../internal/engine/recommend_all.go) |
| Memory usage percentile | P95 | Max (P100) | [`DefaultMemoryConfig()`](../../internal/engine/types.go), [`memConfigForProfile()`](../../internal/engine/recommend_all.go) |
| Adaptive margin | 1.15–1.50 (both) | 1.15–1.50 (both) | [`margin.go`](../../internal/engine/margin.go) |
| Limit multiplier | 1.05× request | 1.05× request | [`types.go`](../../internal/engine/types.go) |
| CPU floor | 25 millicores | 25 millicores | [`types.go`](../../internal/engine/types.go) |
| OOM bump | `min(1.60, 1.0 + 0.15 × log₂(1 + OOMCount))` | same | [`recommend_memory.go`](../../internal/engine/recommend_memory.go) |

**Formulas:**

```
cost_request    = max(floor, round(WeightedPercentile(usage, cost_pctile) × adaptive_margin))
perf_request    = max(floor, round(WeightedPercentile(usage, perf_pctile) × adaptive_margin))
limit           = round(request × 1.05)
```

Adaptive margin: `clamp(1.0 + (p95 − p50) / mean, 1.15, 1.50)` — see
[Recommendation Math](recommendation-math.md#adaptive-margin).

### Default terms (container, namespace, node, GPU)

| Term | Window | Min data days | Decay half-life |
|------|--------|---------------|-----------------|
| short | 1 day | 1 | 0 (no decay) |
| medium | 7 days | 3 | 168 h (7 d) |
| long | 15 days | 7 | 360 h (15 d) |

Plugin defaults: [`internal/plugins/container/plugin.go`](../../internal/plugins/container/plugin.go),
[`internal/plugins/namespace/plugin.go`](../../internal/plugins/namespace/plugin.go),
[`internal/plugins/node/plugin.go`](../../internal/plugins/node/plugin.go),
[`internal/plugins/gpu/plugin.go`](../../internal/plugins/gpu/plugin.go).

### Idle, abandoned, and staleness

| Signal | Condition | Configurable |
|--------|-----------|--------------|
| **Idle** | Max CPU ≤ 10 m **and** max memory ≤ 10 MiB across all digest rows in the term window | Constants in [`detect_idle.go`](../../internal/engine/detect_idle.go) |
| **Abandoned** | All digest rows have CPU max = 0 **and** memory max = 0 | [`DetectAbandoned()`](../../internal/engine/detect_idle.go) |
| **Stale** | No cluster report within staleness threshold (default 48 h) | `ROS_STALENESS_THRESHOLD_HOURS` |

Idle containers receive 100% savings estimation (deallocation). Abandoned
supersedes idle in notification codes.

---

## Node (CPU + Memory) Recommendations

Each node produces **two engine rows** per term. Shared classification is computed
once per (node, term); sizing and consolidation differ by engine.

### Engine-specific parameters

| Parameter | Cost engine | Performance engine | Source |
|-----------|-------------|-------------------|--------|
| Target utilization | 80% | 55% | [`nodeEngines`](../../internal/engine/recommend_nodes.go) |
| Node consolidation | When underutilized | Only with extreme waste (current ≥ 2× recommended on **both** CPU and memory) | [`sizeNodeForEngine()`](../../internal/engine/recommend_nodes.go) |

**Sizing:** recommended capacity = `max(usage_p95, requests) / target_utilization`.

**Consolidation:**

- **Cost:** `NodeCountReduction = 1` whenever the node is classified underutilized.
- **Performance:** consolidation only when underutilized **and**
  `current_cpu ≥ 2 × rec_cpu` **and** `current_mem ≥ 2 × rec_mem`.

### Shared classification thresholds

| Signal | Threshold | Env override | Source |
|--------|-----------|--------------|--------|
| Underutilized | CPU P95 **and** mem P95 < 30% of allocatable | `ROS_NODE_UNDERUTIL_THRESHOLD` (default `0.30`) | [`classifyNode()`](../../internal/engine/recommend_nodes.go) |
| Overcommitted | CPU requests / allocatable > 150% | `ROS_NODE_OVERCOMMIT_THRESHOLD` (default `1.50`) | [`classifyNode()`](../../internal/engine/recommend_nodes.go) |
| Stranded resources | EMA-smoothed imbalance > 0.6 | `ROS_NODE_STRANDED_IMBALANCE_THRESHOLD` (default `0.6`) | [`classifyNode()`](../../internal/engine/recommend_nodes.go) |

**Supporting parameters:**

| Parameter | Default | Env variable |
|-----------|---------|--------------|
| Allocatable fallback factor | 0.93 | `ROS_NODE_ALLOCATABLE_FACTOR` |
| EMA smoothing alpha | 0.3 | `ROS_NODE_EMA_ALPHA` |

Stranded imbalance per day: `|cpu_util_p95 − mem_util_p95| / max(cpu_util_p95, mem_util_p95)`.
The stranded resource label is `cpu` or `memory` depending on which dimension is higher.

Default terms: same as container (1d / 7d / 15d). See [Cost Integration — Node Savings](cost-integration.md#node-savings-cpumemory-utilization).

---

## GPU Recommendations

No cost/performance engine split. One classification per (container, GPU model, term).

`filter[engine]=cost|performance` applies to container, namespace, node, and VM list
routes only. GPU plugin APIs (`GET .../gpu/mig`, `GET .../gpu/timeslicing`, container
detail `gpu.{term}`) use **recommendation terms** (`short` / `medium` / `long`) instead.
Public docs: [GPU MIG](../../docs-site/features/gpu-mig.md#recommendation-terms-vs-dual-engine).

### Classification thresholds

Evaluated in order on daily-average DCGM metrics across the term window:

| Classification | Condition | Env override |
|----------------|-----------|--------------|
| `no_profiling` | No SM / tensor / DRAM profiling data | — |
| `idle` | avg SM < 2% | `ROS_GPU_IDLE_THRESHOLD` (default `0.02`) |
| `memory_bound` | avg DRAM > 60% **and** avg tensor < 15% | `ROS_GPU_MEMBOUND_DRAM_THRESHOLD`, `ROS_GPU_MEMBOUND_TENSOR_THRESHOLD` |
| `underutilized` | avg tensor < 15% **and** avg SM < 25% | `ROS_GPU_UNDERUTILIZED_TENSOR_THRESHOLD`, `ROS_GPU_UNDERUTILIZED_SM_THRESHOLD` |
| `compute_bound_underutil` | avg tensor < 25% **and** avg DRAM < 30% | (hardcoded `0.30` DRAM in code) |
| `well_utilized` | everything else | — |

Source: [`GPUThresholds.Classify()`](../../internal/engine/gpu_recommender.go).
Extended discussion: [GPU Classification](gpu-classification.md).

### MIG profile selection

| Parameter | Value | Env override |
|-----------|-------|--------------|
| Frame-buffer percentile | P98 of daily max FB usage | — |
| Headroom factor | × 1.20 | `ROS_GPU_FB_HEADROOM_FACTOR` (default `1.20`) |

Select smallest MIG profile where `profile_FB_MiB ≥ P98_FB × headroom`. If none
fits → `"full_gpu"`.

### GPU time-slicing (node-level)

| Parameter | Value | Source |
|-----------|-------|--------|
| Candidate majority | ≥ 50% of eligible containers (candidates + impacted) | [`ComputeNodeTimeslicingRec()`](../../internal/engine/gpu_timeslicing.go) |
| Replica formula | `ceil(1 / peak_util)` where peak = max(avg SM, avg DRAM, avg FB fraction) | [`computeReplicas()`](../../internal/engine/gpu_timeslicing.go) |
| Replica clamp | [2, 8] | [`gpu_timeslicing.go`](../../internal/engine/gpu_timeslicing.go) |
| Node freshness | Latest telemetry ≤ 7 days | `NodeGPUFreshnessDays` |

**Excluded from time-slicing candidates:** idle, memory-bound, MIG-recommended workloads.

Default terms: same as container (1d / 7d / 15d). Max window: 90 days.

**Savings:** `total_node_savings` and `savings_per_gpu` on the time-slicing list, plus
`estimated_monthly_timeslicing_savings` on container `gpu.{term}`, are computed at
**API read time** from Masu `effective_rates` — not stored on ingest. GPU savings are
excluded from `GET .../savings-summary`. See [GPU plugin — Savings](../../docs-site/plugin-reference/gpu.md#savings).

---

## PVC Recommendations

No cost/performance engine split. One classification per (PVC, term).

### Classification thresholds

| Classification | Condition | Env override |
|----------------|-----------|--------------|
| **Oversized** | `usage_max / capacity < 20%` (requires ≥ min_data_days) | Hardcoded `pvcOversizedThreshold = 0.20` |
| **Near-full** | `usage_max / capacity > 85%` | Hardcoded `pvcNearFullThreshold = 0.85` |
| **Orphaned** | All usage metrics zero for ≥ min_data_days | — |
| **Healthy** | default | — |

Source: [`pvc_recommend.go`](../../internal/engine/pvc_recommend.go).

### Sizing and alerts

| Rule | Value |
|------|-------|
| Recommended capacity (oversized / near-full) | `max(usage_max × 2, 1 GiB)` |
| Growth alert | Projected days-to-full < 30 (positive growth slope) |
| Growth trend minimum | ≥ 2 days, or term `min_data_days` if larger |

### Default terms (PVC only — longer windows)

| Term | Window | Min data days | Decay half-life |
|------|--------|---------------|-----------------|
| short | 7 days | 3 | 0 (no decay) |
| medium | 30 days | 14 | 0 |
| long | 90 days | 30 | 0 |

Plugin defaults: [`internal/plugins/pvc/plugin.go`](../../internal/plugins/pvc/plugin.go).
Max window: **365 days**.

See also [PVC Right-Sizing](../features-f27-pvc-rightsizing.md).

---

## ResourceQuota Recommendations

No cost/performance engine split. One recommendation per namespace `ResourceQuota`
hard limit set. The engine runs after container recommendations are written during
container CSV processing (not as an `IngestHook` — namespace CSV hooks fire before
`recommendation_sets` exist).

### Input signals

| Signal | Source | Notes |
|--------|--------|-------|
| Quota hard / used | Latest namespace quota snapshot from namespace CSV digests | CPU, memory, storage, pods, object count |
| Recommended workload totals | Sum of container `cost` engine **`medium_term`** recommendations per namespace | Fixed term/engine in [`recommend_quota.go`](../../internal/engine/recommend_quota.go) |

### Classification thresholds

Recommended values apply configurable headroom (default **10%**) to container sums
and quota-used metrics. Utilization compares the greater of quota **used** and
recommended totals against hard limits (basis points).

| Recommendation type | Condition | Source |
|---------------------|-----------|--------|
| **raise** | Max utilization ≥ high-risk threshold (default **90%**) | [`classifyQuotaRecommendation()`](../../internal/engine/recommend_quota.go) |
| **tighten** | Recommended hard limits below current hard on CPU, memory, storage, or pods | same |
| **optimal** | Hard limits present, no raise/tighten signal | same |
| **none** | No actionable quota hard limits | same |

| Risk level | Condition | Default threshold |
|------------|-----------|-------------------|
| **high** | Max utilization ≥ high-risk | 90% (`ROS_QUOTA_HIGH_RISK_THRESHOLD_PERCENT`) |
| **medium** | Max utilization ≥ medium-risk | 70% (`ROS_QUOTA_MEDIUM_RISK_THRESHOLD_PERCENT`) |
| **low** | Utilization > 0 below medium-risk | — |
| **none** | No utilization signal | — |

### Settings and API

Settings precedence: env (locked) → per-org DB (`recommendation_thresholds`) → compiled default.
Settings API: `GET/PUT/DELETE /recommendations/openshift/settings/quota`.

List API: `GET /recommendations/openshift/quota`. Detail:
`GET /recommendations/openshift/quota/detail`.

### Savings

`estimated_savings_cents` is computed at ingestion for **`tighten`** recommendations
only, from freed CPU/memory/storage capacity and Koku `effective_rates`. Fleet rollup:
`GET .../savings-summary` → `by_plugin.quota`.

Plugin: [`internal/plugins/quota/plugin.go`](../../internal/plugins/quota/plugin.go).

---

## ClusterResourceQuota Recommendations

No cost/performance engine split. One recommendation per OpenShift
`ClusterResourceQuota`. Ingests `cluster-quota` CSV metrics into
`daily_cluster_quota_digests`; recommendations run on cluster-quota CSV processing.

### Input signals

| Signal | Source | Notes |
|--------|--------|-------|
| CRQ hard / used | Latest snapshot from cluster-quota digests | Scoped namespace list on the CRQ |
| Namespace quota aggregates | Sum of namespace `quota` plugin recommendations for CRQ namespaces | CPU/memory request/limit totals |

### Classification thresholds

Same headroom and risk model as namespace **quota** (default headroom **10%**,
high-risk **90%**, medium-risk **70%**). Recommended values apply headroom to the
greater of CRQ **used** and aggregated namespace recommendations.

| Recommendation type | Condition |
|---------------------|-----------|
| **raise** | Max utilization ≥ high-risk threshold |
| **tighten** | Recommended limits below current hard on CPU, memory, storage, or pods |
| **optimal** | Hard limits present, no raise/tighten signal |
| **none** | No actionable hard limits |

Env overrides: `ROS_CLUSTER_QUOTA_HEADROOM_PERCENT`,
`ROS_CLUSTER_QUOTA_HIGH_RISK_THRESHOLD_PERCENT`,
`ROS_CLUSTER_QUOTA_MEDIUM_RISK_THRESHOLD_PERCENT`.

### Settings and API

Settings API: `GET/PUT/DELETE /recommendations/openshift/settings/cluster-quota`.

List API: `GET /recommendations/openshift/cluster-quota`. Detail:
`GET /recommendations/openshift/cluster-quota/detail`.

### Savings

`estimated_savings_cents` at ingestion for **`tighten`** only (same rate model as
namespace quota). Fleet rollup: `by_plugin.cluster-quota`.

Source: [`recommend_cluster_quota.go`](../../internal/engine/recommend_cluster_quota.go).
Plugin: [`internal/plugins/cluster-quota/plugin.go`](../../internal/plugins/cluster-quota/plugin.go).

---

## Snapshot Recommendations

No engines, no terms. Single classification per snapshot at ingestion time.

### Classification thresholds

Evaluated in priority order:

| Classification | Condition | Default | Env override |
|----------------|-----------|---------|--------------|
| **Orphaned** | Source PVC deleted **and** age > N days | 7 days | `ROS_SNAPSHOT_ORPHAN_AGE_DAYS` |
| **Managed** | Backup-tool label detected (Velero, Kasten, etc.) | — | — |
| **Redundant** | > N snapshots per PVC (not among N newest, age > stale days) | 3 | `ROS_SNAPSHOT_REDUNDANT_THRESHOLD` |
| **Stale** | Age > N **and** restore count = 0 | 90 days | `ROS_SNAPSHOT_STALE_DAYS` |
| **Never restored** | Age > N **and** restore count = 0 | 30 days | `ROS_SNAPSHOT_NEVER_RESTORED_DAYS` |
| **Active** | default | — | — |

Source: [`classifySnapshot()`](../../internal/engine/snapshot_classify.go),
[`snapshot_settings.go`](../../internal/engine/snapshot_settings.go).

Settings precedence: env (locked) → per-org DB (`snapshot_settings`) → compiled default.
Settings API: `GET/PUT /recommendations/openshift/settings/snapshot`.

List API: `GET /recommendations/openshift/snapshots`. Namespace/cluster rollup for
prioritization: `GET /recommendations/openshift/snapshots/summary` (aggregates
reclaimable holding cost and restore size; see
[`handlers_snapshot_summary.go`](../../internal/api/handlers_snapshot_summary.go)).

### Cost rate

| Priority | Source |
|----------|--------|
| 1 | Per-org Settings API (`cost_per_gib_month_usd`) |
| 2 | `ROS_SNAPSHOT_COST_PER_GIB_MONTH` env var |
| 3 | Koku `effective_rates` → `storage_gb_usage_per_month` (infra + supplementary) |
| 4 | Compiled default **$0.05**/GiB/month |

Formula: `estimated_cost_cents = USDToCents(restore_size_gib × cost_per_gib_month)`; API exposes `estimated_monthly_cost` as `MoneyAmount`.

v1 rates are **placeholders** (Settings/env/default, optional PVC storage proxy).
Provider-accurate costing is upstream **[COST-7523](https://redhat.atlassian.net/browse/COST-7523)**
(effective cost internal endpoint). v1 is **detection-only** — no automated restore,
safe-delete, or Velero/OADP workflows.

See [Cost Integration — Snapshot cost](cost-integration.md#snapshot-cost-dynamic-default-from-effective-rates).

---

## VM (OpenShift Virtualization) Recommendations

KubeVirt VMs use **whole vCPU / whole GiB** sizing with separate cost and performance
engine rows per term. Source: [`recommendVM()`](../../internal/engine/vm_recommender.go).

| Engine | CPU percentile | Memory percentile | Notes |
|--------|----------------|-------------------|-------|
| **cost** | P95 (default) | P95 + 20% margin | Adaptive CPU margin 15–50% when enabled; downsize hysteresis |
| **performance** | P99 (default) | P99 + margin | Higher headroom for burst workloads; downsize stability days |

Defaults: [`DefaultVMRecConfig()`](../../internal/engine/vm_config.go) (`ROS_VM_CPU_PERCENTILE_COST`,
`ROS_VM_CPU_PERCENTILE_PERF`). Digest fields: `CPUUsageP95MC` / `CPUUsageP99MC`,
`MemUsageP95KiB` / `MemUsageP99KiB`.

### Default terms (VM only — longer windows)

| Term | Window | Min data days | Decay half-life |
|------|--------|---------------|-----------------|
| short | 7 days | 3 | 0 (no decay) |
| medium | 15 days | 7 | 0 |
| long | 30 days | 15 | 0 |

Plugin defaults: [`internal/plugins/vm/plugin.go`](../../internal/plugins/vm/plugin.go).
Tenant term overrides: `GET/PUT/DELETE .../settings/terms?recommendation_type=vm`.
Threshold overrides: `GET/PUT/DELETE .../settings/vm` (percentiles, margins, idle, disk, GPU, network).
Max window: **90 days** (`MaxWindowDays()`). History retention:
`ROS_VM_REC_HISTORY_RETENTION_DAYS`.

### Savings

Monthly `savings` (`value` + `units`) is computed at ingestion and returned on list/detail.
When `ROS_SAVINGS_ESTIMATES_ENABLED=false` or rates are missing, `savings` is JSON **`null`**
(no notification code **25**). Fleet rollup: `GET .../savings-summary` → `by_plugin.vm`.

Public docs: [Virtual Machine recommendations](../../docs-site/features/virtual-machines.md),
[Plugin reference — vm](../../docs-site/plugin-reference/vm.md).

---

## Term Configurability

All term-based plugins support three configurable terms (`short`, `medium`, `long`).

### Precedence (per term, per plugin)

1. **Admin env var** — always wins; locks the term from tenant override
2. **Tenant DB** — `org_recommendation_terms` table
3. **Plugin default** — `DefaultTerms()` on each plugin

### Environment variables

Format: `ROS_TERMS_<PLUGIN>_<TERM>_<FIELD>`

| Field suffix | Description |
|--------------|-------------|
| `WINDOW_DAYS` | Lookback window (1 … plugin `MaxWindowDays()`) |
| `MIN_DATA_DAYS` | Minimum digest days required to emit a recommendation |
| `DECAY_HALFLIFE_HOURS` | Exponential decay half-life (0 = no decay) |

**Examples:**

```
ROS_TERMS_CONTAINER_LONG_WINDOW_DAYS=30
ROS_TERMS_PVC_MEDIUM_MIN_DATA_DAYS=21
ROS_TERMS_GPU_SHORT_DECAY_HALFLIFE_HOURS=0
```

**Plugin names** (uppercase in env): `CONTAINER`, `NAMESPACE`, `NODE`, `GPU`, `PVC`, `VM`.

When `WINDOW_DAYS` is set without `MIN_DATA_DAYS`, min data days auto-derives as
`max(1, window_days / 2)` via [`ComputeMinDataDays()`](../../internal/engine/term_config.go).

### Settings API

```
GET    /recommendations/openshift/settings/terms?recommendation_type=<plugin>
PUT    /recommendations/openshift/settings/terms?recommendation_type=<plugin>
DELETE /recommendations/openshift/settings/terms?recommendation_type=<plugin>
```

Valid `recommendation_type` values: plugins implementing `TermProvider`
(container, namespace, node, gpu, pvc, vm). **quota**, **cluster-quota**, and
**snapshot** use threshold-based settings endpoints instead of terms.

Implementation: [`handlers_terms.go`](../../internal/api/handlers_terms.go),
[`term_config.go`](../../internal/engine/term_config.go).

---

## Business Hours

When `ROS_BUSINESS_HOURS_ENABLED=true` (default), a second digest stream
(`schedule_type = business_hours`) is computed at ingestion. Recommendations
use **identical percentiles and sizing parameters** as container:

| Engine | CPU percentile | Memory percentile |
|--------|----------------|-------------------|
| cost | P60 | P95 |
| performance | P98 | Max (P100) |

OOM bump, adaptive margin, limit multiplier, and floor match container defaults.
Terms and decay follow the container plugin configuration.

Source: [`recommend_business_hours.go`](../../internal/engine/recommend_business_hours.go).
Admin guide: [Business Hours](../business-hours-admin-guide.md).

---

## Related Documentation

| Document | Scope |
|----------|-------|
| [Decay Weights](decay-weights.md) | Exponential decay formula, charts, edge weights, configuration |
| [Recommendation Math](recommendation-math.md) | Adaptive margin, trend detection algorithms |
| [GPU Classification](gpu-classification.md) | GPU decision tree, MIG, confidence scoring, two-tier support |
| [Cost Integration](cost-integration.md) | Savings formulas, kill-switch, fleet summary, plugin savings matrix |
| [Plugin Architecture](plugin-architecture.md) | Plugin traits, term provider, registry |
| [Configurability Reference](configurability.md) | All `ROS_*` env vars, Settings API, precedence, tuning |

## Source File Index

| Area | Primary files |
|------|---------------|
| Container sizing | [`types.go`](../../internal/engine/types.go), [`recommend_cpu.go`](../../internal/engine/recommend_cpu.go), [`recommend_memory.go`](../../internal/engine/recommend_memory.go), [`recommend_all.go`](../../internal/engine/recommend_all.go) |
| Namespace | [`recommend_namespace.go`](../../internal/engine/recommend_namespace.go) |
| Node | [`recommend_nodes.go`](../../internal/engine/recommend_nodes.go), [`node_savings.go`](../../internal/engine/node_savings.go) |
| GPU | [`gpu_recommender.go`](../../internal/engine/gpu_recommender.go), [`gpu_timeslicing.go`](../../internal/engine/gpu_timeslicing.go) |
| PVC | [`pvc_recommend.go`](../../internal/engine/pvc_recommend.go), [`pvc_savings.go`](../../internal/engine/pvc_savings.go) |
| Quota | [`recommend_quota.go`](../../internal/engine/recommend_quota.go), [`quota_settings.go`](../../internal/engine/quota_settings.go), [`quota_run.go`](../../internal/engine/quota_run.go) |
| Cluster-quota | [`recommend_cluster_quota.go`](../../internal/engine/recommend_cluster_quota.go), [`cluster_quota_settings.go`](../../internal/engine/cluster_quota_settings.go), [`cluster_quota_run.go`](../../internal/engine/cluster_quota_run.go) |
| Snapshot | [`snapshot_classify.go`](../../internal/engine/snapshot_classify.go), [`snapshot_settings.go`](../../internal/engine/snapshot_settings.go) |
| VM | [`vm_recommender.go`](../../internal/engine/vm_recommender.go), [`vm_config.go`](../../internal/engine/vm_config.go), [`vm_savings.go`](../../internal/engine/vm_savings.go), [`vm_settings.go`](../../internal/engine/vm_settings.go) |
| Terms | [`term_config.go`](../../internal/engine/term_config.go), [`handlers_terms.go`](../../internal/api/handlers_terms.go) |
| Global config | [`internal/config/config.go`](../../internal/config/config.go) |
| Plugin defaults | [`internal/plugins/*/plugin.go`](../../internal/plugins/) |
